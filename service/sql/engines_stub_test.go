// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	config "github.com/wippyai/runtime/api/service/sql"
	entryutil "github.com/wippyai/runtime/internal/entry"
)

// stubEngine is a faithful-but-minimal engine registered only for the core dispatch
// tests. The real engines live in service/sql/engine/* and cannot be imported here
// (that would be an import cycle), so these stubs exercise the registry, factory,
// manager, and ConnPool plumbing. Real DSN/env/WAL behavior is tested in the engine
// sub-packages.
type stubEngine struct {
	kind     registry.Kind
	driver   string
	isSQLite bool
}

func init() {
	RegisterEngine(stubEngine{kind: config.Postgres, driver: "postgres"})
	RegisterEngine(stubEngine{kind: config.MySQL, driver: "mysql"})
	RegisterEngine(stubEngine{kind: config.SQLite, driver: "sqlite3", isSQLite: true})
}

func (e stubEngine) Kind() registry.Kind {
	return e.kind
}

func (e stubEngine) DriverName() string {
	return e.driver
}

func (e stubEngine) DecodeConfig(ctx context.Context, dtt payload.Transcoder, entry registry.Entry) (config.EngineConfig, error) {
	if e.isSQLite {
		cfg, err := entryutil.DecodeEntryConfig[config.SQLiteConfig](ctx, dtt, entry)
		if err != nil {
			return nil, err
		}
		return cfg, nil
	}

	cfg, err := entryutil.DecodeEntryConfig[config.DBConfig](ctx, dtt, entry)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func (stubEngine) ResolveEnv(context.Context, EngineDeps, config.EngineConfig) error {
	return nil
}

func (e stubEngine) BuildDSN(config.EngineConfig) (string, error) {
	if e.isSQLite {
		return ":memory:", nil
	}
	return "host=stub", nil
}

func (stubEngine) Prepare(context.Context, *sql.DB, config.EngineConfig) error {
	return nil
}

func (e stubEngine) Tune(db *sql.DB, ec config.EngineConfig) {
	if e.isSQLite {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		if cfg, ok := ec.(*config.SQLiteConfig); ok {
			db.SetConnMaxLifetime(cfg.Pool.MaxLifetime)
		}
		return
	}

	if cfg, ok := ec.(*config.DBConfig); ok {
		db.SetMaxOpenConns(cfg.Pool.MaxOpen)
		db.SetMaxIdleConns(cfg.Pool.MaxIdle)
		db.SetConnMaxLifetime(cfg.Pool.MaxLifetime)
	}
}

func (e stubEngine) ValidateConfigType(ec config.EngineConfig) error {
	if e.isSQLite {
		if _, ok := ec.(*config.SQLiteConfig); !ok {
			return NewInvalidConfigTypeError(fmt.Sprintf("%T", ec), e.kind)
		}
		return nil
	}

	if _, ok := ec.(*config.DBConfig); !ok {
		return NewInvalidConfigTypeError(fmt.Sprintf("%T", ec), e.kind)
	}
	return nil
}
