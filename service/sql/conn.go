// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"database/sql"
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

		p.db.SetMaxOpenConns(c.Pool.MaxOpen)
		p.db.SetMaxIdleConns(c.Pool.MaxIdle)
		p.db.SetConnMaxLifetime(c.Pool.MaxLifetime)

		var cfg any = c
		p.config.Store(&cfg)

	case *config.SQLiteConfig:
		if p.kind != config.SQLite {
			return NewInvalidConfigTypeError("SQLiteConfig", p.kind)
		}

		if err := c.Validate(); err != nil {
			return NewInvalidConfigError(err)
		}

		p.db.SetConnMaxLifetime(c.Pool.MaxLifetime)

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

	return newDBConn(p, p.db, p.kind), nil
}

// Helper to build DSN string for different database types
func buildDSN(kind registry.Kind, cfg *config.DBConfig) (string, error) {
	switch kind {
	case config.Postgres:
		opts := buildPostgresOptionsString(cfg.Options)
		var b strings.Builder
		b.Grow(128)
		b.WriteString("host=")
		b.WriteString(cfg.Host)
		b.WriteString(" port=")
		b.WriteString(strconv.Itoa(cfg.Port))
		b.WriteString(" user=")
		b.WriteString(cfg.Username)
		b.WriteString(" password=")
		b.WriteString(cfg.Password)
		b.WriteString(" dbname=")
		b.WriteString(cfg.Database)
		if opts != "" {
			b.WriteString(" ")
			b.WriteString(opts)
		}
		return b.String(), nil

	case config.MySQL:
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
		b.WriteString(options[k])
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
