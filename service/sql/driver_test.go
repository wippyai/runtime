// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	config "github.com/wippyai/runtime/api/service/sql"
	"go.uber.org/zap"
)

// fixedConfigEngine returns a preset SQLite config regardless of the entry, so driver
// tests can drive createPool to sql.Open without a transcoder. Prepare can be forced
// to fail to exercise the create lifecycle's close-on-error branch.
type fixedConfigEngine struct {
	prepareErr error
	kind       registry.Kind
	driver     string
}

func (e fixedConfigEngine) Kind() registry.Kind { return e.kind }

func (e fixedConfigEngine) DriverName() string { return e.driver }

func (fixedConfigEngine) DecodeConfig(context.Context, payload.Transcoder, registry.Entry) (config.EngineConfig, error) {
	return &config.SQLiteConfig{File: ":memory:", Pool: config.PoolConfig{MaxLifetime: time.Hour}}, nil
}

func (fixedConfigEngine) ResolveEnv(context.Context, EngineDeps, config.EngineConfig) error {
	return nil
}

func (fixedConfigEngine) BuildDSN(config.EngineConfig) (string, error) {
	return ":memory:", nil
}

func (e fixedConfigEngine) Prepare(context.Context, *sql.DB, config.EngineConfig) error {
	return e.prepareErr
}

func (fixedConfigEngine) Tune(*sql.DB, config.EngineConfig) {}

func (fixedConfigEngine) ValidateConfigType(config.EngineConfig) error {
	return nil
}

func TestDriverSelectionIsExplicit(t *testing.T) {
	const kind = registry.Kind("db.sql.drivernametest")
	driver := fixedConfigEngine{kind: kind, driver: "sqlite3"}
	deps := EngineDeps{Log: zap.NewNop()}
	entry := registry.Entry{ID: registry.NewID("test", "ov"), Kind: kind, Data: payload.New("x")}

	_, _, err := NewDefaultPoolFactory().CreatePool(context.Background(), deps, entry)
	require.Error(t, err, "an unconfigured factory must not discover a driver globally")

	factory := NewDefaultPoolFactory(driver)
	pool, _, err := factory.CreatePool(context.Background(), deps, entry)
	require.NoError(t, err)
	require.NotNil(t, pool)
	require.NoError(t, pool.Stop(context.Background()))
}

func TestCreatePoolClosesOnPrepareError(t *testing.T) {
	const kind = registry.Kind("db.sql.preparefailtest")
	prepErr := errors.New("prepare boom")
	driver := fixedConfigEngine{kind: kind, driver: "sqlite3", prepareErr: prepErr}

	entry := registry.Entry{ID: registry.NewID("test", "pf"), Kind: kind, Data: payload.New("x")}
	pool, _, err := NewDefaultPoolFactory(driver).CreatePool(context.Background(), EngineDeps{Log: zap.NewNop()}, entry)

	require.Error(t, err)
	assert.Nil(t, pool)
	assert.ErrorIs(t, err, prepErr)
}
