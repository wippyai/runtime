package sql

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	config "github.com/ponyruntime/pony/api/resource/sql"
)

// ConnPool represents a database connection pool that acts both as a service
// and a resource provider
type ConnPool struct {
	kind   registry.Kind
	db     *sql.DB
	status chan any

	mu     sync.RWMutex
	wg     sync.WaitGroup // tracks active resource users
	closed bool
	config interface{} // either *config.DBConfig or *config.SQLiteConfig
}

// dbResource represents a database connection resource
type dbResource struct {
	pool     *ConnPool
	released bool
	mu       sync.RWMutex
}

// NewStandardConnPool creates a new connection pool for standard SQL databases
func NewStandardConnPool(kind registry.Kind, cfg *config.DBConfig) (*ConnPool, error) {
	dsn, err := buildDSN(kind, cfg)
	if err != nil {
		return nil, fmt.Errorf("invalid connection config: %w", err)
	}

	db, err := sql.Open(string(kind), dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Configure pool settings
	db.SetMaxOpenConns(cfg.Pool.MaxOpen)
	db.SetMaxIdleConns(cfg.Pool.MaxIdle)

	lifetime, err := time.ParseDuration(cfg.Pool.MaxLifetime)
	if err != nil {
		return nil, fmt.Errorf("invalid max lifetime duration: %w", err)
	}
	db.SetConnMaxLifetime(lifetime)

	return &ConnPool{
		kind:   kind,
		db:     db,
		config: cfg,
		status: make(chan any, 1),
	}, nil
}

// NewSQLiteConnPool creates a new connection pool for SQLite
func NewSQLiteConnPool(cfg *config.SQLiteConfig) (*ConnPool, error) {
	db, err := sql.Open("sqlite3", cfg.File)
	if err != nil {
		return nil, fmt.Errorf("failed to create SQLite connection: %w", err)
	}

	// SQLite specific settings
	db.SetMaxOpenConns(1) // SQLite supports only one writer
	db.SetMaxIdleConns(1)

	lifetime, err := time.ParseDuration(cfg.Pool.MaxLifetime)
	if err != nil {
		return nil, fmt.Errorf("invalid max lifetime duration: %w", err)
	}
	db.SetConnMaxLifetime(lifetime)

	return &ConnPool{
		kind:   config.KindSQLite,
		db:     db,
		config: cfg,
		status: make(chan any, 1),
	}, nil
}

// Start implements supervisor.Service
func (p *ConnPool) Start(ctx context.Context) (<-chan any, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
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
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()

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
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return fmt.Errorf("connection pool is closed")
	}

	switch c := cfg.(type) {
	case *config.DBConfig:
		if p.kind == config.KindSQLite {
			return fmt.Errorf("invalid config type for SQLite")
		}
		p.db.SetMaxOpenConns(c.Pool.MaxOpen)
		p.db.SetMaxIdleConns(c.Pool.MaxIdle)
		lifetime, err := time.ParseDuration(c.Pool.MaxLifetime)
		if err != nil {
			return fmt.Errorf("invalid max lifetime duration: %w", err)
		}
		p.db.SetConnMaxLifetime(lifetime)
		p.config = c

	case *config.SQLiteConfig:
		if p.kind != config.KindSQLite {
			return fmt.Errorf("invalid config type for non-SQLite database")
		}
		lifetime, err := time.ParseDuration(c.Pool.MaxLifetime)
		if err != nil {
			return fmt.Errorf("invalid max lifetime duration: %w", err)
		}
		p.db.SetConnMaxLifetime(lifetime)
		p.config = c

	default:
		return fmt.Errorf("unsupported config type: %T", cfg)
	}

	return nil
}

// Close closes the connection pool
func (p *ConnPool) Close() error {
	return p.Stop(context.Background())
}

// Acquire implements resource.Provider
func (p *ConnPool) Acquire(ctx context.Context, id registry.ID, mode resource.AccessMode) (resource.Resource[any], error) {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return nil, fmt.Errorf("connection pool is closed")
	}
	p.mu.RUnlock()

	// Only support normal mode for now
	if mode != resource.ModeNormal {
		return nil, fmt.Errorf("unsupported access mode: %v", mode)
	}

	// Track resource usage
	p.wg.Add(1)

	return &dbResource{
		pool: p,
	}, nil
}

// Get implements resource.Resource
func (r *dbResource) Get() (any, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.released {
		return nil, resource.ErrResourceReleased
	}

	return r.pool.db, nil
}

// Release implements resource.Resource
func (r *dbResource) Release() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.released {
		return nil
	}

	r.released = true
	r.pool.wg.Done()

	return nil
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
