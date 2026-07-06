// SPDX-License-Identifier: MPL-2.0

// Package sqlite implements the file-backed SQLite engine. It registers itself with
// service/sql through the public engine seam; a CDC-enabled build overrides the
// underlying driver via service/sql.RegisterDriver, which core applies transparently.
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

// defaultDriver is the stock SQLite driver. A build with the preupdate hook overrides
// it via service/sql.RegisterDriver; the engine itself stays override-agnostic.
const defaultDriver = "sqlite3"

type engine struct{}

func init() {
	sqlservice.RegisterEngine(engine{})
}

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

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(cfg.Pool.MaxLifetime)
}

func (engine) ValidateConfigType(ec config.EngineConfig) error {
	if _, ok := ec.(*config.SQLiteConfig); !ok {
		return sqlservice.NewInvalidConfigTypeError(fmt.Sprintf("%T", ec), config.SQLite)
	}

	return nil
}
