// Package sqlstore provides SQL-backed store service configuration.
package sqlstore

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
)

func TestKindConstant(t *testing.T) {
	assert.Equal(t, "store.sql", string(KindSQLKV))
}

func TestSQLConfig_Marshal(t *testing.T) {
	config := SQLConfig{
		Database:          registry.ID{NS: "db", Name: "main"},
		TableName:         "kv_store",
		IDColumnName:      "key",
		PayloadColumnName: "value",
		ExpireColumnName:  "expires_at",
		CleanupInterval:   5 * time.Minute,
	}

	data, err := json.Marshal(&config)
	require.NoError(t, err)
	assert.NotEmpty(t, data)
}

func TestSQLConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  SQLConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: SQLConfig{
				Database:          registry.ID{NS: "db", Name: "main"},
				TableName:         "kv_store",
				IDColumnName:      "key",
				PayloadColumnName: "value",
				ExpireColumnName:  "expires_at",
			},
			wantErr: false,
		},
		{
			name:    "missing database",
			config:  SQLConfig{TableName: "kv_store"},
			wantErr: true,
			errMsg:  "database ID is required",
		},
		{
			name: "missing table name",
			config: SQLConfig{
				Database: registry.ID{NS: "db", Name: "main"},
			},
			wantErr: true,
			errMsg:  "table_name is required",
		},
		{
			name: "invalid table name with SQL keywords",
			config: SQLConfig{
				Database:          registry.ID{NS: "db", Name: "main"},
				TableName:         "select",
				IDColumnName:      "key",
				PayloadColumnName: "value",
				ExpireColumnName:  "expires_at",
			},
			wantErr: true,
			errMsg:  "table_name is invalid",
		},
		{
			name: "negative cleanup interval",
			config: SQLConfig{
				Database:          registry.ID{NS: "db", Name: "main"},
				TableName:         "kv_store",
				IDColumnName:      "key",
				PayloadColumnName: "value",
				ExpireColumnName:  "expires_at",
				CleanupInterval:   -1 * time.Minute,
			},
			wantErr: true,
			errMsg:  "cleanup_interval must be greater than or equal to 0",
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

func TestSQLConfig_IsSafe(t *testing.T) {
	config := SQLConfig{}

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"valid identifier", "my_table", true},
		{"valid with numbers", "table123", true},
		{"starts with number", "123table", false},
		{"SQL keyword", "select", false},
		{"SQL injection attempt", "table'; DROP TABLE users--", false},
		{"with quotes", "table'", false},
		{"too long", "verylongtablenamethatexceedssixtythreecharactersandshouldfailabc", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := config.IsSafe(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSQLConfig_InitDefaults(t *testing.T) {
	config := SQLConfig{}
	config.InitDefaults()

	assert.Equal(t, "key", config.IDColumnName)
	assert.Equal(t, "value", config.PayloadColumnName)
	assert.Equal(t, "expires_at", config.ExpireColumnName)
}

func TestSQLConfig_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr bool
		check   func(*testing.T, SQLConfig)
	}{
		{
			name: "valid config with duration",
			json: `{
				"database": {"ns":"db","name":"main"},
				"table_name": "kv_store",
				"id_column_name": "key",
				"payload_column_name": "value",
				"expire_column_name": "expires_at",
				"cleanup_interval": "5m"
			}`,
			wantErr: false,
			check: func(t *testing.T, c SQLConfig) {
				assert.Equal(t, 5*time.Minute, c.CleanupInterval)
				assert.Equal(t, "kv_store", c.TableName)
			},
		},
		{
			name: "invalid duration format",
			json: `{
				"database": {"ns":"db","name":"main"},
				"table_name": "kv_store",
				"cleanup_interval": "invalid"
			}`,
			wantErr: true,
		},
		{
			name: "no cleanup interval",
			json: `{
				"database": {"ns":"db","name":"main"},
				"table_name": "kv_store"
			}`,
			wantErr: false,
			check: func(t *testing.T, c SQLConfig) {
				assert.Equal(t, time.Duration(0), c.CleanupInterval)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var config SQLConfig
			err := json.Unmarshal([]byte(tt.json), &config)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				if tt.check != nil {
					tt.check(t, config)
				}
			}
		})
	}
}

func TestSQLConfig_Validate_MissingColumns(t *testing.T) {
	tests := []struct {
		name   string
		config SQLConfig
		errMsg string
	}{
		{
			name: "missing id column",
			config: SQLConfig{
				Database:          registry.ID{NS: "db", Name: "main"},
				TableName:         "kv_store",
				PayloadColumnName: "value",
				ExpireColumnName:  "expires_at",
			},
			errMsg: "id_column_name is required",
		},
		{
			name: "missing payload column",
			config: SQLConfig{
				Database:         registry.ID{NS: "db", Name: "main"},
				TableName:        "kv_store",
				IDColumnName:     "key",
				ExpireColumnName: "expires_at",
			},
			errMsg: "payload_column_name is required",
		},
		{
			name: "missing expire column",
			config: SQLConfig{
				Database:          registry.ID{NS: "db", Name: "main"},
				TableName:         "kv_store",
				IDColumnName:      "key",
				PayloadColumnName: "value",
			},
			errMsg: "expire_column_name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

func TestSQLConfig_Validate_InvalidIdentifiers(t *testing.T) {
	tests := []struct {
		name   string
		config SQLConfig
		errMsg string
	}{
		{
			name: "invalid database name",
			config: SQLConfig{
				Database:          registry.ID{NS: "db", Name: "main; DROP TABLE"},
				TableName:         "kv_store",
				IDColumnName:      "key",
				PayloadColumnName: "value",
				ExpireColumnName:  "expires_at",
			},
			errMsg: "database ID is invalid",
		},
		{
			name: "invalid id column name",
			config: SQLConfig{
				Database:          registry.ID{NS: "db", Name: "main"},
				TableName:         "kv_store",
				IDColumnName:      "key; DROP TABLE",
				PayloadColumnName: "value",
				ExpireColumnName:  "expires_at",
			},
			errMsg: "id_column_name is invalid",
		},
		{
			name: "invalid payload column name",
			config: SQLConfig{
				Database:          registry.ID{NS: "db", Name: "main"},
				TableName:         "kv_store",
				IDColumnName:      "key",
				PayloadColumnName: "value; DROP",
				ExpireColumnName:  "expires_at",
			},
			errMsg: "payload_column_name is invalid",
		},
		{
			name: "invalid expire column name",
			config: SQLConfig{
				Database:          registry.ID{NS: "db", Name: "main"},
				TableName:         "kv_store",
				IDColumnName:      "key",
				PayloadColumnName: "value",
				ExpireColumnName:  "expires--",
			},
			errMsg: "expire_column_name is invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

func TestSQLConfig_IsSafe_AdditionalCases(t *testing.T) {
	config := SQLConfig{}

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"with underscore", "my_table_name", true},
		{"SQL comment", "table--comment", false},
		{"block comment", "table/*comment*/", false},
		{"union select", "union select", false},
		{"drop table", "drop table", false},
		{"delete from", "delete from", false},
		{"with quotes", "table\"name", false},
		{"valid caps", "TableName", true},
		{"mixed case with underscore", "My_Table_123", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := config.IsSafe(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
