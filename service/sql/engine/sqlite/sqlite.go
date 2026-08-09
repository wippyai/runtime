// SPDX-License-Identifier: MPL-2.0

// Package sqlite implements the file-backed SQLite SQL driver. It is explicitly
// constructed by the boot graph and owns the connector/connection lifecycle for
// each pool generation.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	config "github.com/wippyai/runtime/api/service/sql"
	sqlservice "github.com/wippyai/runtime/service/sql"
	entryutil "github.com/wippyai/runtime/system/entry"
)

// defaultDriver is retained for diagnostics and config validation. Physical
// opens use the connector-owned driver in observer.go when the preupdate build
// tag is enabled, so no process-global driver name is replaced.
const defaultDriver = "sqlite3"

type engine struct{}

// NewDriver returns a SQLite SQL driver. The concrete return keeps the
// connector-owned Open capability available to composition/integration code;
// it remains assignable to service/sql.Driver wherever only the base engine
// contract is needed.
func NewDriver() engine { return engine{} }

func (engine) Kind() registry.Kind {
	return config.SQLite
}

func (engine) DriverName() string {
	return defaultDriver
}

func (engine) DecodeConfig(ctx context.Context, dtt payload.Transcoder, entry registry.Entry) (config.EngineConfig, error) {
	cfg, err := entryutil.DecodeEntryConfig[config.SQLiteConfig](ctx, dtt, entry)
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

func (engine) ResolveEnv(context.Context, sqlservice.EngineDeps, config.EngineConfig) error {
	return nil
}

func (engine) Open(ctx context.Context, ec config.EngineConfig) (sqlservice.OpenedDB, error) {
	dsn, err := engine{}.BuildDSN(ec)
	if err != nil {
		return sqlservice.OpenedDB{}, err
	}

	cfg, ok := ec.(*config.SQLiteConfig)
	if !ok {
		return sqlservice.OpenedDB{}, sqlservice.NewInvalidConfigTypeError(fmt.Sprintf("%T", ec), config.SQLite)
	}
	db, observer, err := openSQLite(ctx, dsn, cfg.MaxMutationChanges, cfg.MaxMutationBytes)
	if err != nil {
		return sqlservice.OpenedDB{}, sqlservice.NewConnectionPoolCreationError(err)
	}

	return sqlservice.OpenedDB{DB: db, Observer: observer}, nil
}

func (engine) BuildDSN(ec config.EngineConfig) (string, error) {
	cfg, ok := ec.(*config.SQLiteConfig)
	if !ok {
		return "", sqlservice.NewInvalidConfigTypeError(fmt.Sprintf("%T", ec), config.SQLite)
	}

	if cfg.File == ":memory:" {
		return ":memory:", nil
	}

	return "file:" + cfg.File + "?mode=rwc", nil
}

func (engine) Prepare(ctx context.Context, db *sql.DB, _ config.EngineConfig) error {
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL;"); err != nil {
		return sqlservice.NewWALModeError(err)
	}

	return nil
}

func (engine) Tune(db *sql.DB, ec config.EngineConfig) {
	cfg, ok := ec.(*config.SQLiteConfig)
	if !ok {
		return
	}

	// A private in-memory database is scoped to one physical connection, so
	// sharing it across a pool would create multiple unrelated databases. File
	// databases, however, need the configured pool width so a snapshot read
	// transaction does not consume the only writer connection.
	if cfg.File == ":memory:" {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	} else {
		db.SetMaxOpenConns(cfg.Pool.MaxOpen)
		db.SetMaxIdleConns(cfg.Pool.MaxIdle)
	}
	db.SetConnMaxLifetime(cfg.Pool.MaxLifetime)
}

func (engine) ValidateConfigType(ec config.EngineConfig) error {
	if _, ok := ec.(*config.SQLiteConfig); !ok {
		return sqlservice.NewInvalidConfigTypeError(fmt.Sprintf("%T", ec), config.SQLite)
	}

	return nil
}
