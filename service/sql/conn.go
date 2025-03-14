package sql

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"

	config "github.com/ponyruntime/pony/api/service/sql"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
)

// ConnPool represents a database connection pool that acts both as a service
// and a resource provider
type ConnPool struct {
	kind   registry.Kind
	db     *sql.DB
	status chan any

	mu     sync.RWMutex
	wg     sync.WaitGroup // tracks active resource users
	closed atomic.Bool
	config atomic.Pointer[any] // either *config.DBConfig or *config.SQLiteConfig
}

// Start implements supervisor.Service
func (p *ConnPool) Start(ctx context.Context) (<-chan any, error) {
	if p.closed.Load() {
		return nil, fmt.Errorf("connection pool is closed")
	}

	// Test connection
	if err := p.db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
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
		return p.db.Close()
	}
}

// UpdateConfig updates the pool configuration
func (p *ConnPool) UpdateConfig(cfg interface{}) error {
	if p.closed.Load() {
		return fmt.Errorf("connection pool is closed")
	}

	switch c := cfg.(type) {
	case *config.DBConfig:
		if p.kind == config.KindSQLite {
			return fmt.Errorf("invalid config type for SQLite")
		}

		if err := c.Validate(); err != nil {
			return fmt.Errorf("invalid configuration: %w", err)
		}

		p.db.SetMaxOpenConns(c.Pool.MaxOpen)
		p.db.SetMaxIdleConns(c.Pool.MaxIdle)
		p.db.SetConnMaxLifetime(c.Pool.MaxLifetime)

		var cfg any = c
		p.config.Store(&cfg)

	case *config.SQLiteConfig:
		if p.kind != config.KindSQLite {
			return fmt.Errorf("invalid config type for non-SQLite database")
		}

		if err := c.Validate(); err != nil {
			return fmt.Errorf("invalid configuration: %w", err)
		}

		p.db.SetConnMaxLifetime(c.Pool.MaxLifetime)

		var cfg any = c
		p.config.Store(&cfg)

	default:
		return fmt.Errorf("unsupported config type: %T", cfg)
	}

	return nil
}

// Acquire implements resource.Provider
func (p *ConnPool) Acquire(
	_ context.Context,
	_ registry.ID,
	mode resource.AccessMode,
) (resource.Resource[any], error) {
	if p.closed.Load() {
		return nil, fmt.Errorf("connection pool is closed")
	}

	// Only support normal mode for now
	if mode != resource.ModeNormal {
		return nil, fmt.Errorf("unsupported access mode: %v", mode)
	}

	// Track resource usage
	p.wg.Add(1)

	return newDBConn(p, p.db, p.kind), nil
}

// Helper to build DSN string for different database types
func buildDSN(kind registry.Kind, cfg *config.DBConfig) (string, error) {
	switch kind {
	case config.KindPostgres:
		return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s %s",
			cfg.Host, cfg.Port, cfg.Username, cfg.Password, cfg.Database,
			buildOptionsString(cfg.Options)), nil

	case config.KindMySQL:
		return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?%s",
			cfg.Username, cfg.Password, cfg.Host, cfg.Port, cfg.Database,
			buildOptionsString(cfg.Options)), nil

	default:
		return "", fmt.Errorf("unsupported database type: %s", kind)
	}
}

// Helper to build options string from map
func buildOptionsString(options map[string]string) string {
	if len(options) == 0 {
		return ""
	}

	var opts string
	for k, v := range options {
		if opts != "" {
			opts += " "
		}
		opts += fmt.Sprintf("%s=%s", k, v)
	}

	return opts
}

// DBConn represents a database connection resource
type DBConn struct {
	pool     *ConnPool
	released atomic.Bool
	db       *sql.DB
	dbType   registry.Kind
}

// DBResource contains both the database connection and its type
type DBResource struct {
	DB   *sql.DB       // The database connection
	Type registry.Kind // The database type (postgres, mysql, sqlite, etc.)
}

// newDBConn creates a new database resource
func newDBConn(pool *ConnPool, db *sql.DB, dbType registry.Kind) *DBConn {
	return &DBConn{
		pool:   pool,
		db:     db,
		dbType: dbType,
	}
}

// Get implements resource.Resource
func (r *DBConn) Get() (any, error) {
	if r.released.Load() {
		return nil, resource.ErrResourceReleased
	}

	// Return both the DB and its type
	return DBResource{
		DB:   r.db,
		Type: r.dbType,
	}, nil
}

// Release implements resource.Resource
func (r *DBConn) Release() {
	// Only release once - if we were already released, return immediately
	if !r.released.CompareAndSwap(false, true) {
		return
	}

	r.pool.wg.Done()
}
