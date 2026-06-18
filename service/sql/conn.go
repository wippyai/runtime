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
	db     *sql.DB
	status chan any
	config atomic.Pointer[any]
	kind   registry.Kind
	wg     sync.WaitGroup
	closed atomic.Bool
}

// Start implements supervisor.Service
func (p *ConnPool) Start(ctx context.Context) (<-chan any, error) {
	if p.closed.Load() {
		return nil, ErrPoolClosed
	}

	// Test connection
	if err := p.db.PingContext(ctx); err != nil {
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
		return p.db.Close()
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

	if err := eng.ValidateConfigType(ec); err != nil {
		return err
	}

	if err := ec.Validate(); err != nil {
		return NewInvalidConfigError(err)
	}

	eng.Tune(p.db, ec)

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

	return newDBConn(p, p.db, p.kind), nil
}

// DBConn represents a database connection resource
type DBConn struct {
	pool     *ConnPool
	db       *sql.DB
	dbType   registry.Kind
	released atomic.Bool
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
		return nil, resource.ErrReleased
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
