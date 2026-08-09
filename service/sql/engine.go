// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"database/sql"
	"fmt"

	envapi "github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	config "github.com/wippyai/runtime/api/service/sql"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
	"go.uber.org/zap"
)

// EngineDeps carries the shared collaborators an engine needs to turn a registry
// entry into a configured pool.
type EngineDeps struct {
	Transcoder payload.Transcoder
	Env        envapi.Registry
	Log        *zap.Logger
}

// Engine is a self-contained SQL dialect. Engines are supplied to a Manager (or
// Factory) explicitly, so the SQL service has no process-global engine registry.
// The original contract intentionally remains small: custom engines may keep
// using BuildDSN plus database/sql, while engines that own a physical connector
// can additionally implement DBOpener.
type Engine interface {
	Kind() registry.Kind
	DriverName() string
	DecodeConfig(ctx context.Context, dtt payload.Transcoder, entry registry.Entry) (config.EngineConfig, error)
	ResolveEnv(ctx context.Context, deps EngineDeps, cfg config.EngineConfig) error
	BuildDSN(cfg config.EngineConfig) (string, error)
	Prepare(ctx context.Context, db *sql.DB, cfg config.EngineConfig) error
	Tune(db *sql.DB, cfg config.EngineConfig)
	ValidateConfigType(cfg config.EngineConfig) error
}

// Driver is the explicit-injection name for an Engine. It is an alias so
// existing extensions implementing the original Engine contract remain valid.
type Driver = Engine

// DBOpener is the optional physical-handle seam. A driver that implements it
// owns the database connector and any capabilities attached to that physical
// handle (for example SQLite mutation observation). Engines that do not need
// that ownership use the Engine.BuildDSN fallback in openDriverDB.
type DBOpener interface {
	Open(ctx context.Context, cfg config.EngineConfig) (OpenedDB, error)
}

// OpenedDB is the physical database handle created by a Driver. Observer is an
// optional engine capability and is deliberately kept beside the handle so it
// cannot be accidentally shared between unrelated pool generations.
type OpenedDB struct {
	DB       *sql.DB
	Observer sqlapi.CommittedMutationSource
}

// createPool runs the generic create lifecycle for a known engine.
func createPool(ctx context.Context, deps EngineDeps, driver Driver, entry registry.Entry) (*ConnPool, config.EngineConfig, error) {
	cfg, err := driver.DecodeConfig(ctx, deps.Transcoder, entry)
	if err != nil {
		return nil, nil, NewInvalidConfigError(err)
	}

	if err := driver.ResolveEnv(ctx, deps, cfg); err != nil {
		return nil, nil, err
	}

	if err := cfg.Validate(); err != nil {
		return nil, nil, NewInvalidConfigError(err)
	}

	opened, err := openDriverDB(ctx, driver, cfg)
	if err != nil {
		return nil, nil, err
	}

	pool := &ConnPool{
		kind:    driver.Kind(),
		driver:  driver,
		db:      opened.DB,
		current: newDBGeneration(opened.DB, opened.Observer),
		status:  make(chan any, 1),
	}

	var cfgAny any = cfg
	pool.config.Store(&cfgAny)

	return pool, cfg, nil
}

// updatePool runs the generic update lifecycle for a known engine.
func updatePool(ctx context.Context, deps EngineDeps, driver Driver, pool *ConnPool, entry registry.Entry) (config.EngineConfig, error) {
	cfg, err := driver.DecodeConfig(ctx, deps.Transcoder, entry)
	if err != nil {
		return nil, NewInvalidConfigError(err)
	}

	if err := driver.ResolveEnv(ctx, deps, cfg); err != nil {
		return nil, err
	}

	if err := pool.updateConfig(ctx, driver, cfg); err != nil {
		return nil, NewPoolUpdateError(err)
	}

	return cfg, nil
}

func openDriverDB(ctx context.Context, driver Driver, cfg config.EngineConfig) (OpenedDB, error) {
	var (
		opened OpenedDB
		err    error
	)
	if opener, ok := driver.(DBOpener); ok {
		opened, err = opener.Open(ctx, cfg)
	} else {
		var dsn string
		dsn, err = driver.BuildDSN(cfg)
		if err != nil {
			return OpenedDB{}, NewInvalidDSNError(err)
		}
		opened.DB, err = sql.Open(driver.DriverName(), dsn)
		if err != nil {
			return OpenedDB{}, NewConnectionPoolCreationError(err)
		}
	}
	if err != nil {
		return OpenedDB{}, err
	}
	if opened.DB == nil {
		if opened.Observer != nil {
			_ = opened.Observer.Close()
		}
		return OpenedDB{}, NewConnectionPoolCreationError(
			fmt.Errorf("driver %q returned a nil database", driver.Kind()),
		)
	}

	if err := driver.Prepare(ctx, opened.DB, cfg); err != nil {
		_ = opened.DB.Close()
		if opened.Observer != nil {
			_ = opened.Observer.Close()
		}
		return OpenedDB{}, err
	}

	driver.Tune(opened.DB, cfg)
	return opened, nil
}
