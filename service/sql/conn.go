// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"database/sql"
	"sync"
	"sync/atomic"

	config "github.com/wippyai/runtime/api/service/sql"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
)

// ConnPool represents a database connection pool that acts both as a service
// and a resource provider
type ConnPool struct {
	db      *sql.DB
	current *dbGeneration
	status  chan any
	config  atomic.Pointer[any]
	kind    registry.Kind
	mu      sync.RWMutex
	wg      sync.WaitGroup
	closed  atomic.Bool
}

type dbGeneration struct {
	db       *sql.DB
	closed   chan struct{}
	closeErr error
	closeMu  sync.Mutex
	once     sync.Once
	refs     atomic.Int32
	closing  atomic.Bool
}

func newDBGeneration(db *sql.DB) *dbGeneration {
	return &dbGeneration{
		db:     db,
		closed: make(chan struct{}),
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
		g.closeMu.Lock()
		g.closeErr = g.db.Close()
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
	// Try to set closed state - if already closed, return immediately
	if !p.closed.CompareAndSwap(false, true) {
		return nil
	}

	// Wait for all resources to be released
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		p.mu.Lock()
		if p.current == nil && p.db != nil {
			p.current = newDBGeneration(p.db)
		}
		gen := p.current
		p.current = nil
		p.db = nil
		p.mu.Unlock()
		if gen == nil {
			return nil
		}
		gen.closeWhenIdle()
		return gen.waitClosed(ctx)
	}
}

// UpdateConfig updates the pool configuration. It delegates engine-specific
// validation and tuning to the engine registered for the pool's kind.
func (p *ConnPool) UpdateConfig(cfg any) error {
	if p.closed.Load() {
		return ErrPoolClosed
	}

	ec, ok := cfg.(config.EngineConfig)
	if !ok {
		return NewUnsupportedConfigTypeError(p.kind)
	}

	eng, ok := engineFor(p.kind)
	if !ok {
		return NewUnsupportedConfigTypeError(p.kind)
	}

	return p.updateConfig(context.Background(), eng, ec)
}

func (p *ConnPool) updateConfig(ctx context.Context, eng Engine, ec config.EngineConfig) error {
	if p.closed.Load() {
		return ErrPoolClosed
	}

	if err := eng.ValidateConfigType(ec); err != nil {
		return err
	}

	if err := ec.Validate(); err != nil {
		return NewInvalidConfigError(err)
	}

	if p.kind == config.SQLite {
		gen := p.currentGeneration()
		if gen == nil {
			return ErrPoolClosed
		}
		eng.Tune(gen.db, ec)
		var stored any = ec
		p.config.Store(&stored)
		return nil
	}

	newDB, err := openEngineDB(ctx, eng, ec)
	if err != nil {
		return err
	}
	newGen := newDBGeneration(newDB)

	p.mu.Lock()
	if p.closed.Load() {
		p.mu.Unlock()
		_ = newDB.Close()
		return ErrPoolClosed
	}
	oldGen := p.current
	if oldGen == nil && p.db != nil {
		oldGen = newDBGeneration(p.db)
	}
	p.current = newGen
	p.db = newDB
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

	// Track resource usage before checking closed state to avoid race with Stop()
	p.wg.Add(1)

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
	DB   *sql.DB       // The database connection
	Type registry.Kind // The database type (postgres, mysql, sqlite, etc.)
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
		DB:   r.gen.db,
		Type: r.dbType,
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
