// SPDX-License-Identifier: MPL-2.0

package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	config "github.com/wippyai/runtime/api/service/sql"
	sqlservice "github.com/wippyai/runtime/service/sql"
	"go.uber.org/zap"
)

func TestKindDriverRegistered(t *testing.T) {
	e := engine{}
	assert.Equal(t, config.SQLite, e.Kind())
	assert.Equal(t, "sqlite3", e.DriverName())

	_, _, err := sqlservice.NewDefaultPoolFactory(NewDriver()).CreatePool(
		context.Background(),
		sqlservice.EngineDeps{Log: zap.NewNop()},
		registry.Entry{ID: registry.NewID("t", "x"), Kind: config.SQLite, Data: nil},
	)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "unsupported entry kind")
}

func TestBuildDSN(t *testing.T) {
	e := engine{}

	mem, err := e.BuildDSN(&config.SQLiteConfig{File: ":memory:"})
	require.NoError(t, err)
	assert.Equal(t, ":memory:", mem)

	file, err := e.BuildDSN(&config.SQLiteConfig{File: "/tmp/app.db"})
	require.NoError(t, err)
	assert.Equal(t, "file:/tmp/app.db?mode=rwc", file)

	_, err = e.BuildDSN(&config.DBConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid config type")
}

func TestPrepareEnablesWAL(t *testing.T) {
	file := filepath.Join(t.TempDir(), "app.db")
	db, err := sql.Open("sqlite3", "file:"+file+"?mode=rwc")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, engine{}.Prepare(context.Background(), db, &config.SQLiteConfig{File: file}))

	var mode string
	require.NoError(t, db.QueryRowContext(context.Background(), "PRAGMA journal_mode;").Scan(&mode))
	assert.Equal(t, "wal", mode)
}

func TestTuneSingleWriter(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	engine{}.Tune(db, &config.SQLiteConfig{File: ":memory:", Pool: config.PoolConfig{MaxLifetime: time.Hour, MaxOpen: 4, MaxIdle: 4}})
	assert.Equal(t, 1, db.Stats().MaxOpenConnections)
}

func TestTuneHonorsFilePoolWidth(t *testing.T) {
	db, err := sql.Open("sqlite3", "file:test-tune-pool?mode=memory&cache=shared")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	engine{}.Tune(db, &config.SQLiteConfig{
		File: filepath.Join(t.TempDir(), "tune.db"),
		Pool: config.PoolConfig{MaxOpen: 4, MaxIdle: 3, MaxLifetime: time.Hour},
	})
	assert.Equal(t, 4, db.Stats().MaxOpenConnections)
}

func TestValidateConfigType(t *testing.T) {
	require.NoError(t, engine{}.ValidateConfigType(&config.SQLiteConfig{File: ":memory:"}))
	err := engine{}.ValidateConfigType(&config.DBConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid config type")
}
