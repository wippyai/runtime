package sql

import (
	"context"
	"database/sql"

	"github.com/wippyai/runtime/api/registry"
	config "github.com/wippyai/runtime/api/service/sql"
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
		return nil, NewInvalidConfigError(err)
	}

	dsn, err := buildDSN(kind, cfg)
	if err != nil {
		return nil, NewInvalidDSNError(err)
	}

	db, err := sql.Open(getDriver(kind), dsn)
	if err != nil {
		return nil, NewConnectionPoolCreationError(err)
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
		return nil, NewInvalidConfigError(err)
	}

	var dsn string

	// Handle in-memory database
	if cfg.File == ":memory:" {
		dsn = ":memory:"
	} else {
		// Use the file path directly
		dsn = "file:" + cfg.File + "?mode=rwc"
	}

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, NewSQLiteConnectionCreationError(err)
	}

	// Enable WAL mode for better concurrency
	if _, err := db.ExecContext(context.Background(), "PRAGMA journal_mode=WAL;"); err != nil {
		_ = db.Close()
		return nil, NewWALModeError(err)
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
