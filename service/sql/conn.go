package sql

import (
	"database/sql"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"sync/atomic"
)

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
func (r *DBConn) Release() error {
	// Only release once - if we were already released, return immediately
	if !r.released.CompareAndSwap(false, true) {
		return nil
	}

	r.pool.wg.Done()
	return nil
}
