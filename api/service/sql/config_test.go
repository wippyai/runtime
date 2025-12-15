// Package sql provides SQL database service configuration.
package sql

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
)

func TestKindConstants(t *testing.T) {
	tests := []struct {
		name     string
		kind     registry.Kind
		expected string
	}{
		{"postgres", Postgres, "db.sql.postgres"},
		{"mysql", MySQL, "db.sql.mysql"},
		{"sqlite", SQLite, "db.sql.sqlite"},
		{"mssql", MSSQL, "db.sql.mssql"},
		{"oracle", Oracle, "db.sql.oracle"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.kind)
		})
	}
}

func TestDefaultConstants(t *testing.T) {
	assert.Equal(t, 0, DefaultMaxOpen)
	assert.Equal(t, 0, DefaultMaxIdle)
	assert.Equal(t, 1*time.Hour, DefaultMaxLifetime)
}

func TestPoolConfig_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		config  PoolConfig
		wantErr bool
	}{
		{
			name: "complete pool config",
			config: PoolConfig{
				MaxOpen:     100,
				MaxIdle:     10,
				MaxLifetime: 1 * time.Hour,
			},
			wantErr: false,
		},
		{
			name:    "zero values",
			config:  PoolConfig{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded PoolConfig
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.config.MaxOpen, decoded.MaxOpen)
			assert.Equal(t, tt.config.MaxIdle, decoded.MaxIdle)
			assert.Equal(t, tt.config.MaxLifetime, decoded.MaxLifetime)
		})
	}
}

func TestPoolConfig_InitDefaults(t *testing.T) {
	tests := []struct {
		name     string
		config   PoolConfig
		expected PoolConfig
	}{
		{
			name:   "zero values get defaults",
			config: PoolConfig{},
			expected: PoolConfig{
				MaxOpen:     DefaultMaxOpen,
				MaxIdle:     DefaultMaxIdle,
				MaxLifetime: DefaultMaxLifetime,
			},
		},
		{
			name: "existing values preserved",
			config: PoolConfig{
				MaxOpen:     50,
				MaxIdle:     5,
				MaxLifetime: 2 * time.Hour,
			},
			expected: PoolConfig{
				MaxOpen:     50,
				MaxIdle:     5,
				MaxLifetime: 2 * time.Hour,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.config.InitDefaults()
			assert.Equal(t, tt.expected, tt.config)
		})
	}
}

func TestDBConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  DBConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: DBConfig{
				Host:     "localhost",
				Port:     5432,
				Database: "testdb",
				Username: "user",
				Password: "pass",
				Pool:     PoolConfig{MaxLifetime: 1 * time.Hour},
			},
			wantErr: false,
		},
		{
			name: "missing host but has env",
			config: DBConfig{
				HostEnv:  "DB_HOST",
				Port:     5432,
				Database: "testdb",
				Username: "user",
				Password: "pass",
				Pool:     PoolConfig{MaxLifetime: 1 * time.Hour},
			},
			wantErr: false,
		},
		{
			name: "missing host and env",
			config: DBConfig{
				Port:     5432,
				Database: "testdb",
				Username: "user",
				Password: "pass",
			},
			wantErr: true,
			errMsg:  "host is required",
		},
		{
			name: "invalid port",
			config: DBConfig{
				Host:     "localhost",
				Database: "testdb",
				Username: "user",
				Password: "pass",
			},
			wantErr: true,
			errMsg:  "port must be greater than 0",
		},
		{
			name: "missing database",
			config: DBConfig{
				Host:     "localhost",
				Port:     5432,
				Username: "user",
				Password: "pass",
			},
			wantErr: true,
			errMsg:  "database is required",
		},
		{
			name: "negative max open",
			config: DBConfig{
				Host:     "localhost",
				Port:     5432,
				Database: "testdb",
				Username: "user",
				Password: "pass",
				Pool: PoolConfig{
					MaxOpen:     -1,
					MaxLifetime: 1 * time.Hour,
				},
			},
			wantErr: true,
			errMsg:  "max open connections must be non-negative",
		},
		{
			name: "zero max lifetime",
			config: DBConfig{
				Host:     "localhost",
				Port:     5432,
				Database: "testdb",
				Username: "user",
				Password: "pass",
				Pool: PoolConfig{
					MaxLifetime: 0,
				},
			},
			wantErr: true,
			errMsg:  "max lifetime must be greater than 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSQLiteConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  SQLiteConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: SQLiteConfig{
				File: "/var/db/test.db",
				Pool: PoolConfig{MaxLifetime: 1 * time.Hour},
			},
			wantErr: false,
		},
		{
			name: "in-memory database",
			config: SQLiteConfig{
				File: ":memory:",
				Pool: PoolConfig{MaxLifetime: 1 * time.Hour},
			},
			wantErr: false,
		},
		{
			name:    "missing file",
			config:  SQLiteConfig{},
			wantErr: true,
			errMsg:  "file path is required",
		},
		{
			name: "zero max lifetime",
			config: SQLiteConfig{
				File: "/tmp/test.db",
				Pool: PoolConfig{MaxLifetime: 0},
			},
			wantErr: true,
			errMsg:  "max lifetime must be greater than 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDBConfig_InitDefaults(t *testing.T) {
	config := DBConfig{}
	config.InitDefaults()

	assert.Equal(t, DefaultMaxOpen, config.Pool.MaxOpen)
	assert.Equal(t, DefaultMaxIdle, config.Pool.MaxIdle)
	assert.Equal(t, DefaultMaxLifetime, config.Pool.MaxLifetime)
	assert.NotNil(t, config.Options)
}

func TestSQLiteConfig_InitDefaults(t *testing.T) {
	config := SQLiteConfig{}
	config.InitDefaults()

	assert.Equal(t, DefaultMaxOpen, config.Pool.MaxOpen)
	assert.Equal(t, DefaultMaxIdle, config.Pool.MaxIdle)
	assert.Equal(t, DefaultMaxLifetime, config.Pool.MaxLifetime)
	assert.NotNil(t, config.Options)
}

func TestPoolConfig_UnmarshalJSON_InvalidDuration(t *testing.T) {
	jsonData := `{"max_lifetime":"invalid"}`
	var config PoolConfig
	err := json.Unmarshal([]byte(jsonData), &config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid duration format")
}

func TestDBConfig_Validate_MissingUsername(t *testing.T) {
	config := DBConfig{
		Host:     "localhost",
		Port:     5432,
		Database: "testdb",
		Password: "pass",
		Pool:     PoolConfig{MaxLifetime: 1 * time.Hour},
	}
	err := config.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "username is required")
}

func TestDBConfig_Validate_MissingPassword(t *testing.T) {
	config := DBConfig{
		Host:     "localhost",
		Port:     5432,
		Database: "testdb",
		Username: "user",
		Pool:     PoolConfig{MaxLifetime: 1 * time.Hour},
	}
	err := config.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "password is required")
}

func TestDBConfig_Validate_NegativeMaxIdle(t *testing.T) {
	config := DBConfig{
		Host:     "localhost",
		Port:     5432,
		Database: "testdb",
		Username: "user",
		Password: "pass",
		Pool: PoolConfig{
			MaxIdle:     -1,
			MaxLifetime: 1 * time.Hour,
		},
	}
	err := config.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max idle connections must be non-negative")
}

func TestPoolConfig_MarshalJSON(t *testing.T) {
	config := PoolConfig{
		MaxOpen:     100,
		MaxIdle:     10,
		MaxLifetime: 2 * time.Hour,
	}

	data, err := json.Marshal(&config)
	require.NoError(t, err)
	assert.Contains(t, string(data), "2h0m0s")
}
