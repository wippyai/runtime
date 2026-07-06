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
	"github.com/wippyai/runtime/api/registry"
	config "github.com/wippyai/runtime/api/service/sql"
	sqlservice "github.com/wippyai/runtime/service/sql"
	"go.uber.org/zap"
)

func sqlOpenMemory(*testing.T) (*sql.DB, error) {
	return sql.Open("sqlite3", ":memory:")
}

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
			expected: "host='localhost' port=5432 user='user' password='pass' dbname='testdb' sslmode='disable'",
		},
		{
			name:     "postgres with connect timeout",
			kind:     config.Postgres,
			cfg:      &config.DBConfig{Host: "localhost", Port: 5432, Database: "testdb", Username: "user", Password: "pass", Options: map[string]string{"connect_timeout": "2", "sslmode": "disable"}},
			expected: "host='localhost' port=5432 user='user' password='pass' dbname='testdb' connect_timeout='2' sslmode='disable'",
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
	assert.Equal(t, "application_name='test' connect_timeout='10' sslmode='disable'",
		buildPostgresOptionsString(map[string]string{"sslmode": "disable", "connect_timeout": "10", "application_name": "test"}))
	assert.Empty(t, buildMySQLOptionsString(nil))
	assert.Equal(t, "charset=utf8mb4&parseTime=true&timeout=2s",
		buildMySQLOptionsString(map[string]string{"charset": "utf8mb4", "parseTime": "true", "timeout": "2s"}))
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

func TestQuotePostgresValue(t *testing.T) {
	assert.Equal(t, "'alice'", quotePostgresValue("alice"))
	assert.Equal(t, "''", quotePostgresValue(""))
	assert.Equal(t, "'se cret'", quotePostgresValue("se cret"))
	assert.Equal(t, `'O\'Brien'`, quotePostgresValue("O'Brien"))
	assert.Equal(t, `'a\\b'`, quotePostgresValue(`a\b`))
}

func TestValidateDSNFields(t *testing.T) {
	base := func() *config.DBConfig {
		return &config.DBConfig{Host: "h", Port: 5432, Username: "u", Database: "d"}
	}

	require.NoError(t, validateDSNFields(base()))

	c := base()
	c.Host = ""
	assert.ErrorContains(t, validateDSNFields(c), "host is empty")

	c = base()
	c.Port = 0
	assert.ErrorContains(t, validateDSNFields(c), "port is invalid")

	c = base()
	c.Username = ""
	assert.ErrorContains(t, validateDSNFields(c), "username is empty")

	c = base()
	c.Database = ""
	assert.ErrorContains(t, validateDSNFields(c), "database is empty")
}
