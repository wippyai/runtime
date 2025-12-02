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
	assert.Equal(t, "store.memory", KindMemoryKV)
}

func TestMemoryConfig_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		config   MemoryConfig
		expected string
		wantErr  bool
	}{
		{
			name: "complete config",
			config: MemoryConfig{
				MaxSize:         1000,
				CleanupInterval: 5 * time.Minute,
			},
			expected: `{"max_size":1000,"cleanup_interval":"5m0s","lifecycle":{"auto_start":false,"start_timeout":"0s","stop_timeout":"0s","stable_threshold":"0s","restart":{"initial_delay":"0s","max_delay":"0s","backoff_factor":0,"jitter":0,"max_attempts":0},"depends_on":null}}`,
			wantErr:  false,
		},
		{
			name: "zero values",
			config: MemoryConfig{
				MaxSize:         0,
				CleanupInterval: 0,
			},
			expected: `{"max_size":0,"cleanup_interval":"0s","lifecycle":{"auto_start":false,"start_timeout":"0s","stop_timeout":"0s","stable_threshold":"0s","restart":{"initial_delay":"0s","max_delay":"0s","backoff_factor":0,"jitter":0,"max_attempts":0},"depends_on":null}}`,
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

func TestMemoryConfig_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		expected MemoryConfig
		wantErr  bool
	}{
		{
			name: "valid config",
			json: `{
				"max_size": 1000,
				"cleanup_interval": "5m"
			}`,
			expected: MemoryConfig{
				MaxSize:         1000,
				CleanupInterval: 5 * time.Minute,
			},
			wantErr: false,
		},
		{
			name: "zero values",
			json: `{}`,
			expected: MemoryConfig{
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
			var config MemoryConfig
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

func TestMemoryConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  MemoryConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: MemoryConfig{
				MaxSize:         1000,
				CleanupInterval: 5 * time.Minute,
			},
			wantErr: false,
		},
		{
			name: "zero max size is valid",
			config: MemoryConfig{
				MaxSize:         0,
				CleanupInterval: 1 * time.Minute,
			},
			wantErr: false,
		},
		{
			name: "negative max size",
			config: MemoryConfig{
				MaxSize:         -1,
				CleanupInterval: 1 * time.Minute,
			},
			wantErr: true,
			errMsg:  "max_size must be greater than or equal to 0",
		},
		{
			name: "negative cleanup interval",
			config: MemoryConfig{
				MaxSize:         100,
				CleanupInterval: -1 * time.Minute,
			},
			wantErr: true,
			errMsg:  "cleanup_interval must be greater than or equal to 0",
		},
		{
			name: "zero cleanup interval is valid",
			config: MemoryConfig{
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

func TestMemoryConfig_InitDefaults(t *testing.T) {
	tests := []struct {
		name     string
		config   MemoryConfig
		expected MemoryConfig
	}{
		{
			name:   "zero values get defaults",
			config: MemoryConfig{},
			expected: MemoryConfig{
				MaxSize:         10000,
				CleanupInterval: 5 * time.Minute,
			},
		},
		{
			name: "existing values preserved",
			config: MemoryConfig{
				MaxSize:         5000,
				CleanupInterval: 10 * time.Minute,
			},
			expected: MemoryConfig{
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
