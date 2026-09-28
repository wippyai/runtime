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
	mem, err = e.BuildDSN(&config.SQLiteConfig{File: ":memory:", ForeignKeys: true})
	require.NoError(t, err)
	assert.Equal(t, "file::memory:?mode=memory&_foreign_keys=1", mem)

	filePath := filepath.Join(t.TempDir(), "app.db")
	file, err := e.BuildDSN(&config.SQLiteConfig{File: filePath})
	require.NoError(t, err)
	assert.Equal(t, "file:"+filePath+"?mode=rwc", file)
	file, err = e.BuildDSN(&config.SQLiteConfig{File: filePath, ForeignKeys: true})
	require.NoError(t, err)
	assert.Equal(t, "file:"+filePath+"?mode=rwc&_foreign_keys=1", file)

	_, err = e.BuildDSN(&config.DBConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid config type")
}

func TestForeignKeys(t *testing.T) {
	for _, tt := range []struct {
		name        string
		memory      bool
		foreignKeys bool
	}{
		{name: "file enabled", foreignKeys: true},
		{name: "memory enabled", memory: true, foreignKeys: true},
		{name: "file omitted"},
		{name: "memory omitted", memory: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			file := filepath.Join(t.TempDir(), "foreign-keys.db")
			if tt.memory {
				file = ":memory:"
			}
			cfg := &config.SQLiteConfig{File: file, ForeignKeys: tt.foreignKeys, Pool: config.PoolConfig{MaxLifetime: time.Hour}}
			opened, err := (engine{}).Open(ctx, cfg)
			require.NoError(t, err)
			defer func() { require.NoError(t, opened.DB.Close()) }()
			if opened.Observer != nil {
				defer func() { require.NoError(t, opened.Observer.Close()) }()
			}
			(engine{}).Tune(opened.DB, cfg)

			var enabled int
			require.NoError(t, opened.DB.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&enabled))
			if tt.foreignKeys {
				assert.Equal(t, 1, enabled)
			} else {
				assert.Equal(t, 0, enabled)
			}

			_, err = opened.DB.ExecContext(ctx, `CREATE TABLE parent (id INTEGER PRIMARY KEY);
				CREATE TABLE child (id INTEGER PRIMARY KEY, parent_id INTEGER REFERENCES parent(id) ON DELETE CASCADE);`)
			require.NoError(t, err)
			_, err = opened.DB.ExecContext(ctx, "INSERT INTO child (id, parent_id) VALUES (1, 999)")
			if tt.foreignKeys {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			_, err = opened.DB.ExecContext(ctx, "INSERT INTO parent (id) VALUES (1)")
			require.NoError(t, err)
			_, err = opened.DB.ExecContext(ctx, "INSERT INTO child (id, parent_id) VALUES (2, 1)")
			require.NoError(t, err)
			_, err = opened.DB.ExecContext(ctx, "DELETE FROM parent WHERE id = 1")
			require.NoError(t, err)
			var count int
			require.NoError(t, opened.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM child WHERE id = 2").Scan(&count))
			if tt.foreignKeys {
				assert.Equal(t, 0, count)
			} else {
				assert.Equal(t, 1, count)
			}
		})
	}
}

func TestForeignKeysSurviveConnectionRecycling(t *testing.T) {
	ctx := context.Background()
	cfg := &config.SQLiteConfig{
		File:        filepath.Join(t.TempDir(), "recycled.db"),
		ForeignKeys: true,
		Pool:        config.PoolConfig{MaxLifetime: 5 * time.Millisecond},
	}
	opened, err := (engine{}).Open(ctx, cfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, opened.DB.Close()) }()
	if opened.Observer != nil {
		defer func() { require.NoError(t, opened.Observer.Close()) }()
	}
	(engine{}).Tune(opened.DB, cfg)

	_, err = opened.DB.ExecContext(ctx, `CREATE TABLE parent (id INTEGER PRIMARY KEY);
		CREATE TABLE child (parent_id INTEGER REFERENCES parent(id));`)
	require.NoError(t, err)
	time.Sleep(20 * time.Millisecond)
	_, err = opened.DB.ExecContext(ctx, "INSERT INTO child (parent_id) VALUES (999)")
	require.Error(t, err)
	assert.Greater(t, opened.DB.Stats().MaxLifetimeClosed, int64(0))
	var enabled int
	require.NoError(t, opened.DB.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&enabled))
	assert.Equal(t, 1, enabled)
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

func TestTunePreservesSingleApplicationConnection(t *testing.T) {
	db, err := sql.Open("sqlite3", "file:test-tune-pool?mode=memory&cache=shared")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	engine{}.Tune(db, &config.SQLiteConfig{
		File: filepath.Join(t.TempDir(), "tune.db"),
		Pool: config.PoolConfig{MaxOpen: 4, MaxIdle: 3, MaxLifetime: time.Hour},
	})
	assert.Equal(t, 1, db.Stats().MaxOpenConnections)
}

func TestValidateConfigType(t *testing.T) {
	require.NoError(t, engine{}.ValidateConfigType(&config.SQLiteConfig{File: ":memory:"}))
	err := engine{}.ValidateConfigType(&config.DBConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid config type")
}
