// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	config "github.com/wippyai/runtime/api/service/sql"
)

func createTestDBConfig() *config.DBConfig {
	return &config.DBConfig{
		Host:     "localhost",
		Port:     5432,
		Database: "testdb",
		Username: "user",
		Password: "pass",
		Pool: config.PoolConfig{
			MaxOpen:     10,
			MaxIdle:     5,
			MaxLifetime: time.Hour,
		},
		Options: map[string]string{
			"sslmode": "disable",
		},
	}
}

// TestDefaultPoolFactory_BuildDSN tests DSN string building without connecting to actual databases
func TestDefaultPoolFactory_BuildDSN(t *testing.T) {
	tests := []struct {
		name     string
		kind     registry.Kind
		cfg      *config.DBConfig
		expected string
		isError  bool
	}{
		{
			name: "PostgreSQL DSN",
			kind: config.Postgres,
			cfg: &config.DBConfig{
				Host:     "localhost",
				Port:     5432,
				Database: "testdb",
				Username: "user",
				Password: "pass",
				Options: map[string]string{
					"sslmode": "disable",
				},
			},
			expected: "host=localhost port=5432 user=user password=pass dbname=testdb sslmode=disable",
			isError:  false,
		},
		{
			name: "PostgreSQL DSN with connect timeout",
			kind: config.Postgres,
			cfg: &config.DBConfig{
				Host:     "localhost",
				Port:     5432,
				Database: "testdb",
				Username: "user",
				Password: "pass",
				Options: map[string]string{
					"connect_timeout": "2",
					"sslmode":         "disable",
				},
			},
			expected: "host=localhost port=5432 user=user password=pass dbname=testdb connect_timeout=2 sslmode=disable",
			isError:  false,
		},
		{
			name: "MySQL DSN",
			kind: config.MySQL,
			cfg: &config.DBConfig{
				Host:     "localhost",
				Port:     3306,
				Database: "testdb",
				Username: "user",
				Password: "pass",
				Options: map[string]string{
					"charset": "utf8mb4",
				},
			},
			expected: "user:pass@tcp(localhost:3306)/testdb?charset=utf8mb4",
			isError:  false,
		},
		{
			name: "MySQL DSN with query options",
			kind: config.MySQL,
			cfg: &config.DBConfig{
				Host:     "localhost",
				Port:     3306,
				Database: "testdb",
				Username: "user",
				Password: "pass",
				Options: map[string]string{
					"charset":   "utf8mb4",
					"parseTime": "true",
					"timeout":   "2s",
				},
			},
			expected: "user:pass@tcp(localhost:3306)/testdb?charset=utf8mb4&parseTime=true&timeout=2s",
			isError:  false,
		},
		{
			name:     "Unsupported database type",
			kind:     "db.unsupported",
			cfg:      createTestDBConfig(),
			expected: "",
			isError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn, err := buildDSN(tt.kind, tt.cfg)

			if tt.isError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, dsn)
			}
		})
	}
}

// TestDefaultPoolFactory_CreateStandardPool tests standard pool factory methods validation
func TestDefaultPoolFactory_CreateStandardPool(t *testing.T) {
	// We'll test the validation logic without actually connecting
	factory := &DefaultPoolFactory{}

	tests := []struct {
		cfg     *config.DBConfig
		name    string
		kind    registry.Kind
		errMsg  string
		isError bool
	}{
		{
			name:    "Invalid configuration - empty host",
			kind:    config.Postgres,
			cfg:     &config.DBConfig{Host: "", Port: 5432, Database: "db", Username: "user", Password: "pass"},
			isError: true,
			errMsg:  "invalid configuration",
		},
		{
			name:    "Invalid configuration - zero port",
			kind:    config.Postgres,
			cfg:     &config.DBConfig{Host: "localhost", Port: 0, Database: "db", Username: "user", Password: "pass"},
			isError: true,
			errMsg:  "invalid configuration",
		},
		{
			name:    "Invalid configuration - empty database",
			kind:    config.Postgres,
			cfg:     &config.DBConfig{Host: "localhost", Port: 5432, Database: "", Username: "user", Password: "pass"},
			isError: true,
			errMsg:  "invalid configuration",
		},
		{
			name:    "Invalid configuration - empty username",
			kind:    config.Postgres,
			cfg:     &config.DBConfig{Host: "localhost", Port: 5432, Database: "db", Username: "", Password: "pass"},
			isError: true,
			errMsg:  "invalid configuration",
		},
		{
			name:    "Invalid configuration - empty password",
			kind:    config.Postgres,
			cfg:     &config.DBConfig{Host: "localhost", Port: 5432, Database: "db", Username: "user", Password: ""},
			isError: true,
			errMsg:  "invalid configuration",
		},
		{
			name:    "Unsupported database type",
			kind:    "db.unsupported",
			cfg:     createTestDBConfig(),
			isError: true,
			errMsg:  "invalid connection config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool, err := factory.CreateStandardPool(context.Background(), tt.kind, tt.cfg)

			if tt.isError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, pool)
			}
		})
	}
}

// TestDefaultPoolFactory_CreateSQLitePoolValidation tests SQLite pool validation
func TestDefaultPoolFactory_CreateSQLitePoolValidation(t *testing.T) {
	factory := &DefaultPoolFactory{}

	tests := []struct {
		cfg     *config.SQLiteConfig
		name    string
		errMsg  string
		isError bool
	}{
		{
			name:    "Invalid configuration - empty file",
			cfg:     &config.SQLiteConfig{File: "", Pool: config.PoolConfig{MaxLifetime: time.Hour}},
			isError: true,
			errMsg:  "invalid configuration",
		},
		{
			name:    "Invalid configuration - zero max lifetime",
			cfg:     &config.SQLiteConfig{File: ":memory:", Pool: config.PoolConfig{MaxLifetime: 0}},
			isError: true,
			errMsg:  "invalid configuration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool, err := factory.CreateSQLitePool(context.Background(), tt.cfg)

			if tt.isError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, pool)
			}
		})
	}
}
