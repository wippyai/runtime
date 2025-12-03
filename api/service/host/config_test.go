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
				Workers:        4,
				QueueSize:      512,
				LocalQueueSize: 128,
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
					Workers:   2,
					QueueSize: 1024,
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
			assert.Equal(t, tt.config.HostConfig.Workers, decoded.HostConfig.Workers)
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
					Workers:        4,
					QueueSize:      1024,
					LocalQueueSize: 256,
				},
			},
			wantErr: false,
		},
		{
			name: "zero workers defaults to NumCPU",
			config: EntryConfig{
				HostConfig: Config{
					Workers: 0,
				},
			},
			wantErr: false,
		},
		{
			name: "negative workers",
			config: EntryConfig{
				HostConfig: Config{
					Workers: -1,
				},
			},
			wantErr: true,
			errMsg:  "workers must be greater than 0",
		},
		{
			name: "negative queue size",
			config: EntryConfig{
				HostConfig: Config{
					Workers:   2,
					QueueSize: -1,
				},
			},
			wantErr: true,
			errMsg:  "queue_size must be greater than 0",
		},
		{
			name: "negative local queue size",
			config: EntryConfig{
				HostConfig: Config{
					Workers:        2,
					QueueSize:      1024,
					LocalQueueSize: -1,
				},
			},
			wantErr: true,
			errMsg:  "local_queue_size must be greater than 0",
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
