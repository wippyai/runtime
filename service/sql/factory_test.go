package sql

import (
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3" // Import SQLite driver

	"github.com/ponyruntime/pony/api/registry"
	config "github.com/ponyruntime/pony/api/service/sql"
	"github.com/stretchr/testify/assert"
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

//nolint:unused // to be used in tests
func createTestSQLiteConfig() *config.SQLiteConfig {
	return &config.SQLiteConfig{
		File: ":memory:",
		Pool: config.PoolConfig{
			MaxLifetime: time.Hour,
		},
		Options: map[string]string{
			"_journal_mode": "WAL",
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
			kind: config.KindPostgres,
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
			name: "MySQL DSN",
			kind: config.KindMySQL,
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
		name    string
		kind    registry.Kind
		cfg     *config.DBConfig
		isError bool
		errMsg  string
	}{
		{
			name:    "Invalid configuration - empty host",
			kind:    config.KindPostgres,
			cfg:     &config.DBConfig{Host: "", Port: 5432, Database: "db", Username: "user", Password: "pass"},
			isError: true,
			errMsg:  "invalid configuration: host is required",
		},
		{
			name:    "Invalid configuration - zero port",
			kind:    config.KindPostgres,
			cfg:     &config.DBConfig{Host: "localhost", Port: 0, Database: "db", Username: "user", Password: "pass"},
			isError: true,
			errMsg:  "invalid configuration: port must be greater than 0",
		},
		{
			name:    "Invalid configuration - empty database",
			kind:    config.KindPostgres,
			cfg:     &config.DBConfig{Host: "localhost", Port: 5432, Database: "", Username: "user", Password: "pass"},
			isError: true,
			errMsg:  "invalid configuration: database is required",
		},
		{
			name:    "Invalid configuration - empty username",
			kind:    config.KindPostgres,
			cfg:     &config.DBConfig{Host: "localhost", Port: 5432, Database: "db", Username: "", Password: "pass"},
			isError: true,
			errMsg:  "invalid configuration: username is required",
		},
		{
			name:    "Invalid configuration - empty password",
			kind:    config.KindPostgres,
			cfg:     &config.DBConfig{Host: "localhost", Port: 5432, Database: "db", Username: "user", Password: ""},
			isError: true,
			errMsg:  "invalid configuration: password is required",
		},
		{
			name:    "Unsupported database type",
			kind:    "db.unsupported",
			cfg:     createTestDBConfig(),
			isError: true,
			errMsg:  "unsupported database type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool, err := factory.CreateStandardPool(tt.kind, tt.cfg)

			if tt.isError {
				assert.Error(t, err)
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
		name    string
		cfg     *config.SQLiteConfig
		isError bool
		errMsg  string
	}{
		{
			name:    "Invalid configuration - empty file",
			cfg:     &config.SQLiteConfig{File: "", Pool: config.PoolConfig{MaxLifetime: time.Hour}},
			isError: true,
			errMsg:  "invalid configuration: file is required",
		},
		{
			name:    "Invalid configuration - zero max lifetime",
			cfg:     &config.SQLiteConfig{File: ":memory:", Pool: config.PoolConfig{MaxLifetime: 0}},
			isError: true,
			errMsg:  "invalid configuration: pool.max_lifetime must be greater than 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool, err := factory.CreateSQLitePool(tt.cfg)

			if tt.isError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, pool)
			}
		})
	}
}
