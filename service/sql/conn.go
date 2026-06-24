// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
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
		gen.closeWhenIdle()
		return gen.waitClosed(ctx)
	}
}

// UpdateConfig updates the pool configuration
func (p *ConnPool) UpdateConfig(cfg any) error {
	if p.closed.Load() {
		return ErrPoolClosed
	}

	switch c := cfg.(type) {
	case *config.DBConfig:
		if p.kind == config.SQLite {
			return NewInvalidConfigTypeError("DBConfig", config.SQLite)
		}

		if err := c.Validate(); err != nil {
			return NewInvalidConfigError(err)
		}

		newDB, err := openStandardDB(p.kind, c)
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

		var cfg any = c
		p.config.Store(&cfg)

	case *config.SQLiteConfig:
		if p.kind != config.SQLite {
			return NewInvalidConfigTypeError("SQLiteConfig", p.kind)
		}

		if err := c.Validate(); err != nil {
			return NewInvalidConfigError(err)
		}

		gen := p.currentGeneration()
		if gen == nil {
			return ErrPoolClosed
		}
		gen.db.SetConnMaxLifetime(c.Pool.MaxLifetime)

		var cfg any = c
		p.config.Store(&cfg)

	default:
		return NewUnsupportedConfigTypeError(p.kind)
	}

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

func openStandardDB(kind registry.Kind, cfg *config.DBConfig) (*sql.DB, error) {
	dsn, err := buildDSN(kind, cfg)
	if err != nil {
		return nil, NewInvalidDSNError(err)
	}

	db, err := sql.Open(getDriver(kind), dsn)
	if err != nil {
		return nil, NewConnectionPoolCreationError(err)
	}

	db.SetMaxOpenConns(cfg.Pool.MaxOpen)
	db.SetMaxIdleConns(cfg.Pool.MaxIdle)
	db.SetConnMaxLifetime(cfg.Pool.MaxLifetime)
	return db, nil
}

// Helper to build DSN string for different database types
func buildDSN(kind registry.Kind, cfg *config.DBConfig) (string, error) {
	switch kind {
	case config.Postgres:
		if err := validateDSNFields(cfg); err != nil {
			return "", err
		}
		opts := buildPostgresOptionsString(cfg.Options)
		var b strings.Builder
		b.Grow(128)
		b.WriteString("host=")
		b.WriteString(quotePostgresValue(cfg.Host))
		b.WriteString(" port=")
		b.WriteString(strconv.Itoa(cfg.Port))
		b.WriteString(" user=")
		b.WriteString(quotePostgresValue(cfg.Username))
		b.WriteString(" password=")
		b.WriteString(quotePostgresValue(cfg.Password))
		b.WriteString(" dbname=")
		b.WriteString(quotePostgresValue(cfg.Database))
		if opts != "" {
			b.WriteString(" ")
			b.WriteString(opts)
		}
		return b.String(), nil

	case config.MySQL:
		if err := validateDSNFields(cfg); err != nil {
			return "", err
		}
		opts := buildMySQLOptionsString(cfg.Options)
		var b strings.Builder
		b.Grow(128)
		b.WriteString(cfg.Username)
		b.WriteString(":")
		b.WriteString(cfg.Password)
		b.WriteString("@tcp(")
		b.WriteString(cfg.Host)
		b.WriteString(":")
		b.WriteString(strconv.Itoa(cfg.Port))
		b.WriteString(")/")
		b.WriteString(cfg.Database)
		if opts != "" {
			b.WriteString("?")
			b.WriteString(opts)
		}
		return b.String(), nil

	default:
		return "", NewUnsupportedDatabaseTypeError(kind)
	}
}

func validateDSNFields(cfg *config.DBConfig) error {
	switch {
	case cfg.Host == "":
		return NewInvalidDSNError(errors.New("host is empty"))
	case cfg.Port <= 0:
		return NewInvalidDSNError(fmt.Errorf("port is invalid: %d", cfg.Port))
	case cfg.Username == "":
		return NewInvalidDSNError(errors.New("username is empty"))
	case cfg.Database == "":
		return NewInvalidDSNError(errors.New("database is empty"))
	}
	return nil
}

func quotePostgresValue(value string) string {
	var b strings.Builder
	b.Grow(len(value) + 2)
	b.WriteByte('\'')
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c == '\\' || c == '\'' {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	b.WriteByte('\'')
	return b.String()
}

func getDriver(kind registry.Kind) string {
	switch kind {
	case config.Postgres:
		return "postgres"
	case config.MySQL:
		return "mysql"
	default:
		return kind
	}
}

// buildPostgresOptionsString renders lib/pq keyword/value options.
func buildPostgresOptionsString(options map[string]string) string {
	if len(options) == 0 {
		return ""
	}

	keys := make([]string, 0, len(options))
	for k := range options {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.Grow(len(options) * 20)
	for i, k := range keys {
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(quotePostgresValue(options[k]))
	}

	return b.String()
}

func buildMySQLOptionsString(options map[string]string) string {
	if len(options) == 0 {
		return ""
	}

	values := url.Values{}
	for k, v := range options {
		values.Set(k, v)
	}
	return values.Encode()
}

// Helper kept for older internal tests and benchmarks. PostgreSQL was the only
// historical caller that used this space-separated keyword/value form.
func buildOptionsString(options map[string]string) string {
	return buildPostgresOptionsString(options)
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
