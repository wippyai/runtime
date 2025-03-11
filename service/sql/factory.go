package sql

import (
	"database/sql"
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
	config "github.com/ponyruntime/pony/api/service/sql"
)

// PoolFactoryAPI defines the interface for creating database connection pools
type PoolFactoryAPI interface {
	// CreateStandardPool creates a connection pool for standard SQL databases (Postgres, MySQL)
	CreateStandardPool(kind registry.Kind, cfg *config.DBConfig) (*ConnPool, error)

	// CreateSQLitePool creates a connection pool for SQLite databases
	CreateSQLitePool(cfg *config.SQLiteConfig) (*ConnPool, error)
}

// DefaultPoolFactory is the default implementation of PoolFactoryAPI
type DefaultPoolFactory struct{}

// NewDefaultPoolFactory creates a new default pool factory
func NewDefaultPoolFactory() PoolFactoryAPI {
	return &DefaultPoolFactory{}
}

// CreateStandardPool implements PoolFactoryAPI.CreateStandardPool
func (f *DefaultPoolFactory) CreateStandardPool(kind registry.Kind, cfg *config.DBConfig) (*ConnPool, error) {
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

// CreateSQLitePool implements PoolFactoryAPI.CreateSQLitePool
func (f *DefaultPoolFactory) CreateSQLitePool(cfg *config.SQLiteConfig) (*ConnPool, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	var dsn string

	// Handle in-memory database
	if cfg.File == ":memory:" {
		dsn = ":memory:"
	} else {
		// Use the file path directly
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
