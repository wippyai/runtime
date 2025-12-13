// Package host provides host service configuration.
package host

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
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
		{
			name: "deprecated BufferSize migration",
			config: EntryConfig{
				HostConfig: Config{
					Workers:    2,
					BufferSize: 2048,
				},
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

func TestEntryConfig_BufferSizeMigration(t *testing.T) {
	cfg := EntryConfig{
		HostConfig: Config{
			Workers:    2,
			BufferSize: 2048,
		},
	}
	err := cfg.Validate()
	require.NoError(t, err)
	assert.Equal(t, 2048, cfg.HostConfig.QueueSize)
}

func TestSentinelErrors(t *testing.T) {
	assert.EqualError(t, ErrHostNotRunning, "host is not running")
	assert.EqualError(t, ErrHostShuttingDown, "host is shutting down")
	assert.EqualError(t, ErrHostAlreadyRunning, "host already running")
}

func TestError_Interface(t *testing.T) {
	t.Run("ErrInvalidWorkers", func(t *testing.T) {
		err := ErrInvalidWorkers
		assert.Equal(t, "workers must be greater than 0", err.Error())
		assert.Equal(t, apierror.KindInvalid, err.Kind())
		assert.Equal(t, apierror.False, err.Retryable())
		assert.Nil(t, err.Details())
		assert.Nil(t, err.Unwrap())
	})

	t.Run("ErrInvalidQueueSize", func(t *testing.T) {
		err := ErrInvalidQueueSize
		assert.Equal(t, "queue_size must be greater than 0", err.Error())
		assert.Equal(t, apierror.KindInvalid, err.Kind())
	})

	t.Run("ErrInvalidLocalQueueSize", func(t *testing.T) {
		err := ErrInvalidLocalQueueSize
		assert.Equal(t, "local_queue_size must be greater than 0", err.Error())
		assert.Equal(t, apierror.KindInvalid, err.Kind())
	})
}

func TestNewDecodeConfigError(t *testing.T) {
	cause := errors.New("parse error")
	err := NewDecodeConfigError(cause)

	assert.Contains(t, err.Error(), "failed to decode host config")
	assert.Equal(t, apierror.KindInvalid, err.Kind())
	assert.Equal(t, apierror.False, err.Retryable())
	assert.Nil(t, err.Details())
	assert.Equal(t, cause, err.Unwrap())
}
