// SPDX-License-Identifier: MPL-2.0

// Package memstore provides in-memory store service configuration.
package memory

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKindConstant(t *testing.T) {
	assert.Equal(t, "store.memory", KV)
}

func TestConfig_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		config   Config
		wantErr  bool
	}{
		{
			name: "complete config",
			config: Config{
				MaxSize:         1000,
				CleanupInterval: 5 * time.Minute,
			},
			expected: `{"max_size":1000,"cleanup_interval":"5m0s","lifecycle":{"auto_start":false,"restart":{"backoff_factor":0,"jitter":0,"max_attempts":0},"requires":null}}`,
			wantErr:  false,
		},
		{
			name: "zero values",
			config: Config{
				MaxSize:         0,
				CleanupInterval: 0,
			},
			expected: `{"max_size":0,"lifecycle":{"auto_start":false,"restart":{"backoff_factor":0,"jitter":0,"max_attempts":0},"requires":null}}`,
			wantErr:  false,
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
			assert.JSONEq(t, tt.expected, string(data))
		})
	}
}

func TestConfig_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		expected Config
		wantErr  bool
	}{
		{
			name: "valid config",
			json: `{
				"max_size": 1000,
				"cleanup_interval": "5m"
			}`,
			expected: Config{
				MaxSize:         1000,
				CleanupInterval: 5 * time.Minute,
			},
			wantErr: false,
		},
		{
			name: "zero values",
			json: `{}`,
			expected: Config{
				MaxSize:         0,
				CleanupInterval: 0,
			},
			wantErr: false,
		},
		{
			name: "invalid cleanup interval",
			json: `{
				"cleanup_interval": "invalid"
			}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var config Config
			err := json.Unmarshal([]byte(tt.json), &config)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected.MaxSize, config.MaxSize)
			assert.Equal(t, tt.expected.CleanupInterval, config.CleanupInterval)
		})
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		errMsg  string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				MaxSize:         1000,
				CleanupInterval: 5 * time.Minute,
			},
			wantErr: false,
		},
		{
			name: "zero max size is valid",
			config: Config{
				MaxSize:         0,
				CleanupInterval: 1 * time.Minute,
			},
			wantErr: false,
		},
		{
			name: "negative max size",
			config: Config{
				MaxSize:         -1,
				CleanupInterval: 1 * time.Minute,
			},
			wantErr: true,
			errMsg:  "max size must be non-negative",
		},
		{
			name: "negative cleanup interval",
			config: Config{
				MaxSize:         100,
				CleanupInterval: -1 * time.Minute,
			},
			wantErr: true,
			errMsg:  "cleanup interval must be non-negative",
		},
		{
			name: "zero cleanup interval is valid",
			config: Config{
				MaxSize:         100,
				CleanupInterval: 0,
			},
			wantErr: false,
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

func TestConfig_InitDefaults(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected Config
	}{
		{
			name:   "zero values get defaults",
			config: Config{},
			expected: Config{
				MaxSize:         10000,
				CleanupInterval: 5 * time.Minute,
			},
		},
		{
			name: "existing values preserved",
			config: Config{
				MaxSize:         5000,
				CleanupInterval: 10 * time.Minute,
			},
			expected: Config{
				MaxSize:         5000,
				CleanupInterval: 10 * time.Minute,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.config.InitDefaults()
			assert.Equal(t, tt.expected.MaxSize, tt.config.MaxSize)
			assert.Equal(t, tt.expected.CleanupInterval, tt.config.CleanupInterval)
		})
	}
}
