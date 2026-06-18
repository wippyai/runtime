// SPDX-License-Identifier: MPL-2.0

package standard

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	envapi "github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/registry"
	config "github.com/wippyai/runtime/api/service/sql"
	sqlservice "github.com/wippyai/runtime/service/sql"
	"go.uber.org/zap"
)

func sqlOpenMemory(*testing.T) (*sql.DB, error) {
	return sql.Open("sqlite3", ":memory:")
}

type mockEnv struct{ vars map[string]string }

func newMockEnv() *mockEnv { return &mockEnv{vars: make(map[string]string)} }

func (m *mockEnv) Get(_ context.Context, name string) (string, error) {
	if v, ok := m.vars[name]; ok {
		return v, nil
	}
	return "", envapi.ErrVariableNotFound
}

func (m *mockEnv) Lookup(_ context.Context, name string) (string, bool, error) {
	v, ok := m.vars[name]
	return v, ok, nil
}

func (m *mockEnv) Set(_ context.Context, name, value string) error {
	m.vars[name] = value
	return nil
}

func (m *mockEnv) All(_ context.Context) (map[string]string, error) { return m.vars, nil }

func (m *mockEnv) GetStorage(_ context.Context, _ registry.ID) (envapi.Storage, error) {
	return nil, envapi.ErrVariableNotFound
}

func (m *mockEnv) RegisterStorage(_ registry.ID, _ envapi.Storage) {}

func TestRegistered(t *testing.T) {
	for _, k := range []registry.Kind{config.Postgres, config.MySQL} {
		_, _, err := (&sqlservice.DefaultPoolFactory{}).CreatePool(
			context.Background(),
			sqlservice.EngineDeps{Log: zap.NewNop()},
			registry.Entry{ID: registry.NewID("t", "x"), Kind: k, Data: nil},
		)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "unsupported entry kind", "engine %s must be registered", k)
	}
}

func TestBuildDSN(t *testing.T) {
	tests := []struct {
		name     string
		kind     registry.Kind
		cfg      *config.DBConfig
		expected string
	}{
		{
			name:     "postgres",
			kind:     config.Postgres,
			cfg:      &config.DBConfig{Host: "localhost", Port: 5432, Database: "testdb", Username: "user", Password: "pass", Options: map[string]string{"sslmode": "disable"}},
			expected: "host=localhost port=5432 user=user password=pass dbname=testdb sslmode=disable",
		},
		{
			name:     "postgres with connect timeout",
			kind:     config.Postgres,
			cfg:      &config.DBConfig{Host: "localhost", Port: 5432, Database: "testdb", Username: "user", Password: "pass", Options: map[string]string{"connect_timeout": "2", "sslmode": "disable"}},
			expected: "host=localhost port=5432 user=user password=pass dbname=testdb connect_timeout=2 sslmode=disable",
		},
		{
			name:     "mysql",
			kind:     config.MySQL,
			cfg:      &config.DBConfig{Host: "localhost", Port: 3306, Database: "testdb", Username: "user", Password: "pass", Options: map[string]string{"charset": "utf8mb4"}},
			expected: "user:pass@tcp(localhost:3306)/testdb?charset=utf8mb4",
		},
		{
			name:     "mysql with query options",
			kind:     config.MySQL,
			cfg:      &config.DBConfig{Host: "localhost", Port: 3306, Database: "testdb", Username: "user", Password: "pass", Options: map[string]string{"charset": "utf8mb4", "parseTime": "true", "timeout": "2s"}},
			expected: "user:pass@tcp(localhost:3306)/testdb?charset=utf8mb4&parseTime=true&timeout=2s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := engine{kind: tt.kind}
			if tt.kind == config.Postgres {
				e.dsn = buildPostgresDSN
			} else {
				e.dsn = buildMySQLDSN
			}
			dsn, err := e.BuildDSN(tt.cfg)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, dsn)
		})
	}
}

func TestBuildDSN_WrongType(t *testing.T) {
	e := engine{kind: config.Postgres, dsn: buildPostgresDSN}
	_, err := e.BuildDSN(&config.SQLiteConfig{File: ":memory:"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid config type")
}

func TestDriverNameAndKind(t *testing.T) {
	e := engine{kind: config.Postgres, driver: "postgres"}
	assert.Equal(t, config.Postgres, e.Kind())
	assert.Equal(t, "postgres", e.DriverName())
}

func TestOptionsStrings(t *testing.T) {
	assert.Empty(t, buildPostgresOptionsString(nil))
	assert.Equal(t, "application_name=test connect_timeout=10 sslmode=disable",
		buildPostgresOptionsString(map[string]string{"sslmode": "disable", "connect_timeout": "10", "application_name": "test"}))
	assert.Empty(t, buildMySQLOptionsString(nil))
	assert.Equal(t, "charset=utf8mb4&parseTime=true&timeout=2s",
		buildMySQLOptionsString(map[string]string{"charset": "utf8mb4", "parseTime": "true", "timeout": "2s"}))
}

func TestResolveEnv(t *testing.T) {
	ctx := context.Background()
	e := engine{kind: config.Postgres}

	t.Run("resolves all fields", func(t *testing.T) {
		env := newMockEnv()
		require.NoError(t, env.Set(ctx, "DB_HOST", "env-host"))
		require.NoError(t, env.Set(ctx, "DB_PORT", "9999"))
		require.NoError(t, env.Set(ctx, "DB_NAME", "env-db"))
		require.NoError(t, env.Set(ctx, "DB_USER", "env-user"))
		require.NoError(t, env.Set(ctx, "DB_PASS", "env-pass"))
		deps := sqlservice.EngineDeps{Env: env, Log: zap.NewNop()}

		cfg := &config.DBConfig{HostEnv: "DB_HOST", PortEnv: "DB_PORT", DatabaseEnv: "DB_NAME", UsernameEnv: "DB_USER", PasswordEnv: "DB_PASS"}
		require.NoError(t, e.ResolveEnv(ctx, deps, cfg))
		assert.Equal(t, "env-host", cfg.Host)
		assert.Equal(t, 9999, cfg.Port)
		assert.Equal(t, "env-db", cfg.Database)
		assert.Equal(t, "env-user", cfg.Username)
		assert.Equal(t, "env-pass", cfg.Password)
	})

	t.Run("rejects non-numeric port", func(t *testing.T) {
		env := newMockEnv()
		require.NoError(t, env.Set(ctx, "DB_PORT", "not-a-number"))
		deps := sqlservice.EngineDeps{Env: env, Log: zap.NewNop()}
		err := e.ResolveEnv(ctx, deps, &config.DBConfig{PortEnv: "DB_PORT"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid port")
	})

	t.Run("no-op when env vars empty", func(t *testing.T) {
		deps := sqlservice.EngineDeps{Env: newMockEnv(), Log: zap.NewNop()}
		cfg := &config.DBConfig{Host: "static-host"}
		require.NoError(t, e.ResolveEnv(ctx, deps, cfg))
		assert.Equal(t, "static-host", cfg.Host)
	})

	t.Run("rejects wrong config type", func(t *testing.T) {
		deps := sqlservice.EngineDeps{Env: newMockEnv(), Log: zap.NewNop()}
		err := e.ResolveEnv(ctx, deps, &config.SQLiteConfig{File: ":memory:"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid config type")
	})
}

func TestTuneAndValidateConfigType(t *testing.T) {
	e := engine{kind: config.Postgres}
	require.NoError(t, e.ValidateConfigType(&config.DBConfig{}))
	require.Error(t, e.ValidateConfigType(&config.SQLiteConfig{File: ":memory:"}))

	db, err := sqlOpenMemory(t)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	e.Tune(db, &config.DBConfig{Pool: config.PoolConfig{MaxOpen: 7, MaxIdle: 3, MaxLifetime: time.Hour}})
	assert.Equal(t, 7, db.Stats().MaxOpenConnections)
}
