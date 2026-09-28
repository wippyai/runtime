// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"database/sql"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

// ConnPool represents a database connection pool that acts both as a service
// and a resource provider
type ConnPool struct {
	driver      Driver
	stopErr     error
	stopDone    chan struct{}
	current     *dbGeneration
	status      chan any
	config      atomic.Pointer[any]
	db          *sql.DB
	kind        registry.Kind
	wg          sync.WaitGroup
	mu          sync.RWMutex
	stopMu      sync.Mutex
	closed      atomic.Bool
	stopStarted bool
}

type dbGeneration struct {
	closeErr error
	observer sqlapi.CommittedMutationSource
	db       *sql.DB
	closed   chan struct{}
	once     sync.Once
	closeMu  sync.Mutex
	refs     atomic.Int32
	closing  atomic.Bool
}

func newDBGeneration(db *sql.DB, observers ...sqlapi.CommittedMutationSource) *dbGeneration {
	var observer sqlapi.CommittedMutationSource
	if len(observers) > 0 {
		observer = observers[0]
	}
	return &dbGeneration{
		db:       db,
		closed:   make(chan struct{}),
		observer: observer,
	}
}

func (g *dbGeneration) acquire() bool {
	if g == nil || g.closing.Load() {
		return false
	}
	g.refs.Add(1)
	if g.closing.Load() {
		g.release()
		return false
	}
	return true
}

func (g *dbGeneration) release() {
	if g == nil {
		return
	}
	if g.refs.Add(-1) == 0 && g.closing.Load() {
		g.closeNow()
	}
}

func (g *dbGeneration) closeWhenIdle() {
	if g == nil {
		return
	}
	g.closing.Store(true)
	if g.refs.Load() == 0 {
		g.closeNow()
	}
}

func (g *dbGeneration) closeNow() {
	g.once.Do(func() {
		if g.observer != nil {
			_ = g.observer.Close()
		}
		g.closeMu.Lock()
		if g.db != nil {
			g.closeErr = g.db.Close()
		}
		g.closeMu.Unlock()
		close(g.closed)
	})
}

func (g *dbGeneration) waitClosed(ctx context.Context) error {
	if g == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-g.closed:
		g.closeMu.Lock()
		err := g.closeErr
		g.closeMu.Unlock()
		return err
	}
}

func (p *ConnPool) currentGeneration() *dbGeneration {
	p.mu.RLock()
	gen := p.current
	p.mu.RUnlock()
	if gen != nil {
		return gen
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.current == nil && p.db != nil {
		p.current = newDBGeneration(p.db)
	}
	return p.current
}

// Start implements supervisor.Service
func (p *ConnPool) Start(ctx context.Context) (<-chan any, error) {
	if p.closed.Load() {
		return nil, ErrPoolClosed
	}

	gen := p.currentGeneration()
	if gen == nil {
		return nil, ErrPoolClosed
	}

	// Test connection
	if err := gen.db.PingContext(ctx); err != nil {
		return nil, NewPingError(err)
	}

	// Signal ready status
	select {
	case p.status <- "database connection established":
	default:
	}

	return p.status, nil
}

// Stop implements supervisor.Service
func (p *ConnPool) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	p.stopMu.Lock()
	// Serialize the closed transition with Acquire's WaitGroup admission. A
	// positive Add must not race with cleanupStop's Wait when the pool has no
	// outstanding resources; holding this mutex makes the handoff explicit.
	p.closed.Store(true)
	if !p.stopStarted {
		p.stopStarted = true
		p.stopDone = make(chan struct{})
		go p.cleanupStop(p.stopDone)
	}
	done := p.stopDone
	p.stopMu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		p.stopMu.Lock()
		err := p.stopErr
		p.stopMu.Unlock()
		return err
	}
}

func (p *ConnPool) cleanupStop(done chan struct{}) {
	p.wg.Wait()
	p.mu.Lock()
	if p.current == nil && p.db != nil {
		p.current = newDBGeneration(p.db)
	}
	gen := p.current
	p.current = nil
	p.db = nil
	p.mu.Unlock()
	var err error
	if gen != nil {
		gen.closeWhenIdle()
		err = gen.waitClosed(context.Background())
	}
	p.stopMu.Lock()
	p.stopErr = err
	p.stopMu.Unlock()
	close(done)
}

// UpdateConfig updates the pool configuration. It delegates engine-specific
// validation and tuning to the engine registered for the pool's kind.
func (p *ConnPool) UpdateConfig(cfg any) error {
	if p.closed.Load() {
		return ErrPoolClosed
	}

	ec, ok := cfg.(sqlapi.EngineConfig)
	if !ok {
		return NewUnsupportedConfigTypeError(p.kind)
	}

	if p.driver == nil {
		return NewUnsupportedConfigTypeError(p.kind)
	}

	return p.updateConfig(context.Background(), p.driver, ec)
}

func (p *ConnPool) updateConfig(ctx context.Context, driver Driver, ec sqlapi.EngineConfig) error {
	if p.closed.Load() {
		return ErrPoolClosed
	}

	if err := driver.ValidateConfigType(ec); err != nil {
		return err
	}

	if err := ec.Validate(); err != nil {
		return NewInvalidConfigError(err)
	}

	opened, err := openDriverDB(ctx, driver, ec)
	if err != nil {
		return err
	}
	newGen := newDBGeneration(opened.DB, opened.Observer)

	p.mu.Lock()
	if p.closed.Load() {
		p.mu.Unlock()
		if opened.Observer != nil {
			_ = opened.Observer.Close()
		}
		_ = opened.DB.Close()
		return ErrPoolClosed
	}
	oldGen := p.current
	if oldGen == nil && p.db != nil {
		oldGen = newDBGeneration(p.db)
	}
	p.current = newGen
	p.db = opened.DB
	p.mu.Unlock()

	if oldGen != nil {
		oldGen.closeWhenIdle()
	}

	var stored any = ec
	p.config.Store(&stored)

	return nil
}

// Acquire implements resource.Provider
func (p *ConnPool) Acquire(
	_ context.Context,
	_ registry.ID,
	mode resource.AccessMode,
) (resource.Resource[any], error) {
	// Only support normal mode for now
	if mode != resource.ModeNormal {
		return nil, NewUnsupportedAccessModeError(string(mode))
	}

	// Admission is serialized with Stop. This prevents a positive WaitGroup Add
	// from racing with cleanupStop's Wait after the counter reaches zero.
	p.stopMu.Lock()
	if p.closed.Load() {
		p.stopMu.Unlock()
		return nil, ErrPoolClosed
	}
	p.wg.Add(1)
	p.stopMu.Unlock()

	if p.closed.Load() {
		p.wg.Done()
		return nil, ErrPoolClosed
	}

	for {
		gen := p.currentGeneration()
		if gen == nil {
			p.wg.Done()
			return nil, ErrPoolClosed
		}
		if gen.acquire() {
			return newDBConn(p, gen, p.kind), nil
		}
		if p.closed.Load() {
			p.wg.Done()
			return nil, ErrPoolClosed
		}
	}
}

// DBConn represents a database connection resource
type DBConn struct {
	pool     *ConnPool
	gen      *dbGeneration
	dbType   registry.Kind
	released atomic.Bool
}

// DBResource contains both the database connection and its type
type DBResource struct {
	Observer sqlapi.CommittedMutationSource
	DB       *sql.DB       // The database connection
	Type     registry.Kind // The database type (postgres, mysql, sqlite, etc.)
}

// newDBConn creates a new database resource
func newDBConn(pool *ConnPool, gen *dbGeneration, dbType registry.Kind) *DBConn {
	return &DBConn{
		pool:   pool,
		gen:    gen,
		dbType: dbType,
	}
}

// Get implements resource.Resource
func (r *DBConn) Get() (any, error) {
	if r.released.Load() {
		return nil, resource.ErrReleased
	}

	// Return both the DB and its type
	return DBResource{
		DB:       r.gen.db,
		Type:     r.dbType,
		Observer: r.gen.observer,
	}, nil
}

// Release implements resource.Resource
func (r *DBConn) Release() {
	// Only release once - if we were already released, return immediately
	if !r.released.CompareAndSwap(false, true) {
		return
	}

	r.gen.release()
	r.pool.wg.Done()
}
