package sql

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/ponyruntime/pony/api/fs"
	"os"
	"sync"
	"sync/atomic"

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
	closed atomic.Bool
	config atomic.Pointer[any] // either *config.DBConfig or *config.SQLiteConfig
}

// NewStandardConnPool creates a new connection pool for standard SQL databases
func NewStandardConnPool(kind registry.Kind, cfg *config.DBConfig) (*ConnPool, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

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
	db.SetConnMaxLifetime(cfg.Pool.MaxLifetime)

	pool := &ConnPool{
		kind:   kind,
		db:     db,
		status: make(chan any, 1),
	}

	var cfgAny any = cfg
	pool.config.Store(&cfgAny)

	return pool, nil
}

// NewSQLiteConnPool creates a new connection pool for SQLite
func NewSQLiteConnPool(ctx context.Context, cfg *config.SQLiteConfig) (*ConnPool, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	var dsn string

	// Handle in-memory database
	if cfg.File == ":memory:" {
		dsn = ":memory:"
	} else {
		// Get FS registry from context
		fsReg := fs.FromContext(ctx)
		if fsReg == nil {
			return nil, fmt.Errorf("fs registry not found in context")
		}

		filesystem, exists := fsReg.GetFS(cfg.FS.String())
		if !exists {
			return nil, fmt.Errorf("filesystem %s not found", cfg.FS)
		}

		// Open file through FS to verify path
		f, err := filesystem.OpenFile(cfg.File, os.O_CREATE|os.O_RDWR, 0644)
		if err != nil {
			return nil, fmt.Errorf("failed to create/open database file: %w", err)
		}
		_ = f.Close()

		// SQLite needs absolute path
		dsn = fmt.Sprintf("file:%s?mode=rwc", cfg.File)
	}

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to create SQLite connection: %w", err)
	}

	// Enable WAL mode for better concurrency
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	// SQLite specific settings
	db.SetMaxOpenConns(1) // SQLite supports only one writer
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(cfg.Pool.MaxLifetime)

	pool := &ConnPool{
		kind:   config.KindSQLite,
		db:     db,
		status: make(chan any, 1),
	}

	var cfgAny any = cfg
	pool.config.Store(&cfgAny)

	return pool, nil
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
	ctx context.Context,
	id registry.ID,
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

	return newDBConn(p, p.db), nil
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
