// SPDX-License-Identifier: MPL-2.0

// Package sql provides SQL database service configuration.
package sql

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/dispatcher"
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
		errMsg  string
		config  DBConfig
		wantErr bool
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
		errMsg  string
		config  SQLiteConfig
		wantErr bool
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

func TestCommandIDs(t *testing.T) {
	assert.Equal(t, dispatcher.CommandID(100), Query)
	assert.Equal(t, dispatcher.CommandID(101), Execute)
	assert.Equal(t, dispatcher.CommandID(102), Prepare)
	assert.Equal(t, dispatcher.CommandID(103), Begin)
	assert.Equal(t, dispatcher.CommandID(104), StmtQuery)
	assert.Equal(t, dispatcher.CommandID(105), StmtExecute)
	assert.Equal(t, dispatcher.CommandID(106), StmtClose)
	assert.Equal(t, dispatcher.CommandID(107), TxQuery)
	assert.Equal(t, dispatcher.CommandID(108), TxExecute)
	assert.Equal(t, dispatcher.CommandID(109), TxPrepare)
	assert.Equal(t, dispatcher.CommandID(110), TxCommit)
	assert.Equal(t, dispatcher.CommandID(111), TxRollback)
}

func TestQueryCmd(t *testing.T) {
	cmd := AcquireQueryCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, Query, cmd.CmdID())

	cmd.Query = "SELECT * FROM users"
	cmd.Params = []any{1, "test"}
	cmd.Release()

	cmd2 := AcquireQueryCmd()
	assert.Nil(t, cmd2.DB)
	assert.Empty(t, cmd2.Query)
	assert.Nil(t, cmd2.Params)
	cmd2.Release()
}

func TestExecuteCmd(t *testing.T) {
	cmd := AcquireExecuteCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, Execute, cmd.CmdID())

	cmd.Query = "INSERT INTO users VALUES (?, ?)"
	cmd.Params = []any{1, "test"}
	cmd.Release()

	cmd2 := AcquireExecuteCmd()
	assert.Nil(t, cmd2.DB)
	assert.Empty(t, cmd2.Query)
	assert.Nil(t, cmd2.Params)
	cmd2.Release()
}

func TestPrepareCmd(t *testing.T) {
	cmd := AcquirePrepareCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, Prepare, cmd.CmdID())

	cmd.Query = "SELECT * FROM users WHERE id = ?"
	cmd.Release()

	cmd2 := AcquirePrepareCmd()
	assert.Nil(t, cmd2.DB)
	assert.Empty(t, cmd2.Query)
	cmd2.Release()
}

func TestBeginCmd(t *testing.T) {
	cmd := AcquireBeginCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, Begin, cmd.CmdID())
	cmd.Release()

	cmd2 := AcquireBeginCmd()
	assert.Nil(t, cmd2.DB)
	assert.Nil(t, cmd2.Options)
	cmd2.Release()
}

func TestStmtQueryCmd(t *testing.T) {
	cmd := AcquireStmtQueryCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, StmtQuery, cmd.CmdID())

	cmd.Params = []any{1}
	cmd.Release()

	cmd2 := AcquireStmtQueryCmd()
	assert.Nil(t, cmd2.Stmt)
	assert.Nil(t, cmd2.Params)
	cmd2.Release()
}

func TestStmtExecuteCmd(t *testing.T) {
	cmd := AcquireStmtExecuteCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, StmtExecute, cmd.CmdID())

	cmd.Params = []any{"value"}
	cmd.Release()

	cmd2 := AcquireStmtExecuteCmd()
	assert.Nil(t, cmd2.Stmt)
	assert.Nil(t, cmd2.Params)
	cmd2.Release()
}

func TestStmtCloseCmd(t *testing.T) {
	cmd := AcquireStmtCloseCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, StmtClose, cmd.CmdID())
	cmd.Release()

	cmd2 := AcquireStmtCloseCmd()
	assert.Nil(t, cmd2.Stmt)
	cmd2.Release()
}

func TestTxQueryCmd(t *testing.T) {
	cmd := AcquireTxQueryCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, TxQuery, cmd.CmdID())

	cmd.Query = "SELECT * FROM users"
	cmd.Params = []any{1}
	cmd.Release()

	cmd2 := AcquireTxQueryCmd()
	assert.Nil(t, cmd2.Tx)
	assert.Empty(t, cmd2.Query)
	assert.Nil(t, cmd2.Params)
	cmd2.Release()
}

func TestTxExecuteCmd(t *testing.T) {
	cmd := AcquireTxExecuteCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, TxExecute, cmd.CmdID())

	cmd.Query = "UPDATE users SET name = ?"
	cmd.Params = []any{"new name"}
	cmd.Release()

	cmd2 := AcquireTxExecuteCmd()
	assert.Nil(t, cmd2.Tx)
	assert.Empty(t, cmd2.Query)
	assert.Nil(t, cmd2.Params)
	cmd2.Release()
}

func TestTxPrepareCmd(t *testing.T) {
	cmd := AcquireTxPrepareCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, TxPrepare, cmd.CmdID())

	cmd.Query = "SELECT * FROM users WHERE id = ?"
	cmd.Release()

	cmd2 := AcquireTxPrepareCmd()
	assert.Nil(t, cmd2.Tx)
	assert.Empty(t, cmd2.Query)
	cmd2.Release()
}

func TestTxCommitCmd(t *testing.T) {
	cmd := AcquireTxCommitCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, TxCommit, cmd.CmdID())
	cmd.Release()

	cmd2 := AcquireTxCommitCmd()
	assert.Nil(t, cmd2.Tx)
	cmd2.Release()
}

func TestTxRollbackCmd(t *testing.T) {
	cmd := AcquireTxRollbackCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, TxRollback, cmd.CmdID())
	cmd.Release()

	cmd2 := AcquireTxRollbackCmd()
	assert.Nil(t, cmd2.Tx)
	cmd2.Release()
}

func TestResponseTypes(t *testing.T) {
	t.Run("QueryResponse", func(t *testing.T) {
		resp := QueryResponse{
			Columns: []string{"id", "name"},
			Rows:    [][]any{{1, "test"}},
		}
		assert.Equal(t, []string{"id", "name"}, resp.Columns)
		assert.Len(t, resp.Rows, 1)
		assert.Nil(t, resp.Error)
	})

	t.Run("ExecuteResponse", func(t *testing.T) {
		resp := ExecuteResponse{
			LastInsertID: 42,
			RowsAffected: 1,
		}
		assert.Equal(t, int64(42), resp.LastInsertID)
		assert.Equal(t, int64(1), resp.RowsAffected)
		assert.Nil(t, resp.Error)
	})

	t.Run("PrepareResponse", func(t *testing.T) {
		resp := PrepareResponse{}
		assert.Nil(t, resp.Stmt)
		assert.Nil(t, resp.Error)
	})

	t.Run("BeginResponse", func(t *testing.T) {
		resp := BeginResponse{}
		assert.Nil(t, resp.Tx)
		assert.Nil(t, resp.Error)
	})
}

func TestErrorConstants(t *testing.T) {
	assert.Contains(t, ErrHostRequired.Error(), "host is required")
	assert.Contains(t, ErrInvalidPort.Error(), "port must be greater than 0")
	assert.Contains(t, ErrDatabaseRequired.Error(), "database is required")
	assert.Contains(t, ErrUsernameRequired.Error(), "username is required")
	assert.Contains(t, ErrPasswordRequired.Error(), "password is required")
	assert.Contains(t, ErrInvalidMaxOpen.Error(), "max open connections must be non-negative")
	assert.Contains(t, ErrInvalidMaxIdle.Error(), "max idle connections must be non-negative")
	assert.Contains(t, ErrInvalidMaxLifetime.Error(), "max lifetime must be greater than 0")
	assert.Contains(t, ErrFileRequired.Error(), "file path is required")
}
