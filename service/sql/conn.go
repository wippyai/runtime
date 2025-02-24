package sql

import (
	"database/sql"
	"github.com/ponyruntime/pony/api/resource"
	"sync/atomic"
)

// dbConn represents a database connection resource
type dbConn struct {
	pool     *ConnPool
	released atomic.Bool
	db       *sql.DB
}

// newDBConn creates a new database resource
func newDBConn(pool *ConnPool, db *sql.DB) *dbConn {
	return &dbConn{
		pool: pool,
		db:   db,
	}
}

// Get implements resource.Resource
func (r *dbConn) Get() (any, error) {
	if r.released.Load() {
		return nil, resource.ErrResourceReleased
	}
	return r.db, nil
}

// Release implements resource.Resource
func (r *dbConn) Release() error {
	// Only release once - if we were already released, return immediately
	if !r.released.CompareAndSwap(false, true) {
		return nil
	}

	r.pool.wg.Done()
	return nil
}
