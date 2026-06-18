// SPDX-License-Identifier: MPL-2.0

// Package standard implements the network SQL engines (PostgreSQL and MySQL) that
// share DBConfig. It registers itself with service/sql through the public engine
// seam, so the core package carries no knowledge of these dialects.
package standard

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	envapi "github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	config "github.com/wippyai/runtime/api/service/sql"
	entryutil "github.com/wippyai/runtime/internal/entry"
	sqlservice "github.com/wippyai/runtime/service/sql"
	"go.uber.org/zap"
)

// engine serves a network SQL dialect. Postgres and MySQL share the same DBConfig,
// env resolution, and pool tuning, differing only in driver name and DSN format, so
// each is a separate instance carrying its own DSN builder.
type engine struct {
	dsn    func(*config.DBConfig) (string, error)
	kind   registry.Kind
	driver string
}

func init() {
	sqlservice.RegisterEngine(engine{kind: config.Postgres, driver: "postgres", dsn: buildPostgresDSN})
	sqlservice.RegisterEngine(engine{kind: config.MySQL, driver: "mysql", dsn: buildMySQLDSN})
}

func (e engine) Kind() registry.Kind {
	return e.kind
}

func (e engine) DriverName() string {
	return e.driver
}

func (engine) DecodeConfig(ctx context.Context, dtt payload.Transcoder, entry registry.Entry) (config.EngineConfig, error) {
	cfg, err := entryutil.DecodeEntryConfig[config.DBConfig](ctx, dtt, entry)
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

func (e engine) ResolveEnv(ctx context.Context, deps sqlservice.EngineDeps, ec config.EngineConfig) error {
	cfg, ok := ec.(*config.DBConfig)
	if !ok {
		return sqlservice.NewInvalidConfigTypeError(fmt.Sprintf("%T", ec), e.kind)
	}

	if v := resolveEnvVar(ctx, deps.Env, deps.Log, cfg.HostEnv, "host"); v != "" {
		cfg.Host = v
	}
	if v := resolveEnvVar(ctx, deps.Env, deps.Log, cfg.PortEnv, "port"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil {
			return sqlservice.NewInvalidPortError(cfg.PortEnv, err)
		}
		cfg.Port = port
	}
	if v := resolveEnvVar(ctx, deps.Env, deps.Log, cfg.DatabaseEnv, "database"); v != "" {
		cfg.Database = v
	}
	if v := resolveEnvVar(ctx, deps.Env, deps.Log, cfg.UsernameEnv, "username"); v != "" {
		cfg.Username = v
	}
	if v := resolveEnvVar(ctx, deps.Env, deps.Log, cfg.PasswordEnv, "password"); v != "" {
		cfg.Password = v
	}

	return nil
}

func (e engine) BuildDSN(ec config.EngineConfig) (string, error) {
	cfg, ok := ec.(*config.DBConfig)
	if !ok {
		return "", sqlservice.NewInvalidConfigTypeError(fmt.Sprintf("%T", ec), e.kind)
	}

	return e.dsn(cfg)
}

func (engine) Prepare(context.Context, *sql.DB, config.EngineConfig) error {
	return nil
}

func (engine) Tune(db *sql.DB, ec config.EngineConfig) {
	cfg, ok := ec.(*config.DBConfig)
	if !ok {
		return
	}

	db.SetMaxOpenConns(cfg.Pool.MaxOpen)
	db.SetMaxIdleConns(cfg.Pool.MaxIdle)
	db.SetConnMaxLifetime(cfg.Pool.MaxLifetime)
}

func (e engine) ValidateConfigType(ec config.EngineConfig) error {
	if _, ok := ec.(*config.DBConfig); !ok {
		return sqlservice.NewInvalidConfigTypeError(fmt.Sprintf("%T", ec), e.kind)
	}

	return nil
}

func buildPostgresDSN(cfg *config.DBConfig) (string, error) {
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
}

func buildMySQLDSN(cfg *config.DBConfig) (string, error) {
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

// resolveEnvVar looks up an environment variable and returns its value, logging and
// returning empty on any miss.
func resolveEnvVar(ctx context.Context, env envapi.Registry, log *zap.Logger, envVar, field string) string {
	if envVar == "" || env == nil {
		return ""
	}

	val, found, err := env.Lookup(ctx, envVar)
	if err != nil {
		if log != nil {
			log.Warn("failed to lookup env var", zap.String("field", field), zap.String("var", envVar), zap.Error(err))
		}
		return ""
	}
	if !found {
		if log != nil {
			log.Warn("env var not found", zap.String("field", field), zap.String("var", envVar))
		}
		return ""
	}

	return val
}
