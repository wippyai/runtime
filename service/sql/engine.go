// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"database/sql"

	envapi "github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	config "github.com/wippyai/runtime/api/service/sql"
	"go.uber.org/zap"
)

// EngineDeps carries the shared collaborators an engine needs to turn a registry
// entry into a configured pool.
type EngineDeps struct {
	Transcoder payload.Transcoder
	Env        envapi.Registry
	Log        *zap.Logger
}

// Engine is a self-contained SQL dialect. Each engine knows how to decode its
// configuration, resolve environment overrides, open and tune a pool, and run any
// post-open preparation. Engines register themselves with RegisterEngine, so adding
// a database never touches the factory or manager dispatch surface.
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

var engines = make(map[registry.Kind]Engine)

// RegisterEngine adds an engine to the registry under its kind. Intended to be
// called from engine package init functions.
func RegisterEngine(e Engine) {
	engines[e.Kind()] = e
}

// engineFor looks up the engine registered for a kind.
func engineFor(kind registry.Kind) (Engine, bool) {
	e, ok := engines[kind]
	return e, ok
}

// createPool runs the generic create lifecycle for a known engine.
func createPool(ctx context.Context, deps EngineDeps, eng Engine, entry registry.Entry) (*ConnPool, config.EngineConfig, error) {
	cfg, err := eng.DecodeConfig(ctx, deps.Transcoder, entry)
	if err != nil {
		return nil, nil, NewInvalidConfigError(err)
	}

	if err := eng.ResolveEnv(ctx, deps, cfg); err != nil {
		return nil, nil, err
	}

	if err := cfg.Validate(); err != nil {
		return nil, nil, NewInvalidConfigError(err)
	}

	dsn, err := eng.BuildDSN(cfg)
	if err != nil {
		return nil, nil, NewInvalidDSNError(err)
	}

	db, err := sql.Open(driverName(eng.Kind(), eng.DriverName()), dsn)
	if err != nil {
		return nil, nil, NewConnectionPoolCreationError(err)
	}

	if err := eng.Prepare(ctx, db, cfg); err != nil {
		_ = db.Close()
		return nil, nil, err
	}

	eng.Tune(db, cfg)

	pool := &ConnPool{
		kind:   eng.Kind(),
		db:     db,
		status: make(chan any, 1),
	}

	var cfgAny any = cfg
	pool.config.Store(&cfgAny)

	return pool, cfg, nil
}

// updatePool runs the generic update lifecycle for a known engine.
func updatePool(ctx context.Context, deps EngineDeps, eng Engine, pool *ConnPool, entry registry.Entry) (config.EngineConfig, error) {
	cfg, err := eng.DecodeConfig(ctx, deps.Transcoder, entry)
	if err != nil {
		return nil, NewInvalidConfigError(err)
	}

	if err := eng.ResolveEnv(ctx, deps, cfg); err != nil {
		return nil, err
	}

	if err := pool.UpdateConfig(cfg); err != nil {
		return nil, NewPoolUpdateError(err)
	}

	return cfg, nil
}
