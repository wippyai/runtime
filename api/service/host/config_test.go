// Package host provides host service configuration.
package host

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/supervisor"
)

func TestKindConstant(t *testing.T) {
	assert.Equal(t, "process.host", KindHost)
}

func TestConfig_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "complete config",
			config: Config{
				MaxProcesses:       1000,
				Workers:            4,
				BufferSize:         512,
				WorkerCount:        8,
				MessageWorkerCount: 8,
			},
			wantErr: false,
		},
		{
			name:    "zero values",
			config:  Config{},
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

			var decoded Config
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.config, decoded)
		})
	}
}

func TestEntryConfig_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		config  EntryConfig
		wantErr bool
	}{
		{
			name: "complete config",
			config: EntryConfig{
				HostConfig: Config{
					MaxProcesses: 100,
					Workers:      2,
				},
				Lifecycle: supervisor.LifecycleConfig{
					AutoStart: true,
				},
			},
			wantErr: false,
		},
		{
			name:    "empty config",
			config:  EntryConfig{},
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

			var decoded EntryConfig
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.config.HostConfig.MaxProcesses, decoded.HostConfig.MaxProcesses)
		})
	}
}

func TestEntryConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  EntryConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: EntryConfig{
				HostConfig: Config{
					MaxProcesses: 100,
					Workers:      4,
				},
			},
			wantErr: false,
		},
		{
			name: "negative max processes",
			config: EntryConfig{
				HostConfig: Config{
					MaxProcesses: -1,
				},
			},
			wantErr: true,
			errMsg:  "max_processes must be greater or equal 0",
		},
		{
			name: "zero workers after init",
			config: EntryConfig{
				HostConfig: Config{
					MaxProcesses: 100,
					Workers:      0,
				},
			},
			wantErr: false,
		},
		{
			name: "negative workers",
			config: EntryConfig{
				HostConfig: Config{
					MaxProcesses: 100,
					Workers:      -1,
				},
			},
			wantErr: true,
			errMsg:  "workers must be greater than 0",
		},
		{
			name: "negative buffer size",
			config: EntryConfig{
				HostConfig: Config{
					MaxProcesses: 100,
					Workers:      2,
					BufferSize:   -1,
				},
			},
			wantErr: true,
			errMsg:  "buffer_size must be greater than 0",
		},
		{
			name: "negative worker count",
			config: EntryConfig{
				HostConfig: Config{
					MaxProcesses: 100,
					Workers:      2,
					BufferSize:   1024,
					WorkerCount:  -1,
				},
			},
			wantErr: true,
			errMsg:  "worker_count must be greater than 0",
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
