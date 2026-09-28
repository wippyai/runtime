// SPDX-License-Identifier: MPL-2.0

// Package standard implements the network SQL drivers (PostgreSQL and MySQL)
// that share DBConfig. Drivers are constructed explicitly by the boot graph;
// importing this package does not mutate process-global SQL state.
package standard

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	config "github.com/wippyai/runtime/api/service/sql"
	sqlservice "github.com/wippyai/runtime/service/sql"
	entryutil "github.com/wippyai/runtime/system/entry"
)

// engine serves a network SQL dialect. Postgres and MySQL share the same DBConfig,
// env resolution, and pool tuning, differing only in driver name and DSN format, so
// each is a separate instance carrying its own DSN builder.
type engine struct {
	dsn    func(*config.DBConfig) (string, error)
	kind   registry.Kind
	driver string
}

// NewPostgresDriver returns the PostgreSQL SQL driver.
func NewPostgresDriver() sqlservice.Driver {
	return engine{kind: config.Postgres, driver: "postgres", dsn: buildPostgresDSN}
}

// NewMySQLDriver returns the MySQL SQL driver.
func NewMySQLDriver() sqlservice.Driver {
	return engine{kind: config.MySQL, driver: "mysql", dsn: buildMySQLDSN}
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

func (engine) ResolveEnv(context.Context, sqlservice.EngineDeps, config.EngineConfig) error {
	return nil
}

func (e engine) Open(_ context.Context, ec config.EngineConfig) (sqlservice.OpenedDB, error) {
	dsn, err := e.BuildDSN(ec)
	if err != nil {
		return sqlservice.OpenedDB{}, err
	}

	db, err := sql.Open(e.driver, dsn)
	if err != nil {
		return sqlservice.OpenedDB{}, sqlservice.NewConnectionPoolCreationError(err)
	}

	return sqlservice.OpenedDB{DB: db}, nil
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
	if err := validateDSNFields(cfg); err != nil {
		return "", err
	}
	opts := buildPostgresOptionsString(cfg.Options)
	var b strings.Builder
	b.Grow(128)
	b.WriteString("host=")
	b.WriteString(quotePostgresValue(cfg.Host))
	b.WriteString(" port=")
	b.WriteString(strconv.Itoa(cfg.Port))
	b.WriteString(" user=")
	b.WriteString(quotePostgresValue(cfg.Username))
	b.WriteString(" password=")
	b.WriteString(quotePostgresValue(cfg.Password))
	b.WriteString(" dbname=")
	b.WriteString(quotePostgresValue(cfg.Database))
	if opts != "" {
		b.WriteString(" ")
		b.WriteString(opts)
	}

	return b.String(), nil
}

func buildMySQLDSN(cfg *config.DBConfig) (string, error) {
	if err := validateDSNFields(cfg); err != nil {
		return "", err
	}
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
		b.WriteString(quotePostgresValue(options[k]))
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

func validateDSNFields(cfg *config.DBConfig) error {
	switch {
	case cfg.Host == "":
		return sqlservice.NewInvalidDSNError(errors.New("host is empty"))
	case cfg.Port <= 0:
		return sqlservice.NewInvalidDSNError(fmt.Errorf("port is invalid: %d", cfg.Port))
	case cfg.Username == "":
		return sqlservice.NewInvalidDSNError(errors.New("username is empty"))
	case cfg.Database == "":
		return sqlservice.NewInvalidDSNError(errors.New("database is empty"))
	}
	return nil
}

func quotePostgresValue(value string) string {
	var b strings.Builder
	b.Grow(len(value) + 2)
	b.WriteByte('\'')
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c == '\\' || c == '\'' {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	b.WriteByte('\'')
	return b.String()
}
