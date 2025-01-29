package terminal

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTimeoutConfig_InitDefaults(t *testing.T) {
	tests := []struct {
		name     string
		input    TimeoutConfig
		expected TimeoutConfig
	}{
		{
			name:  "empty config",
			input: TimeoutConfig{},
			expected: TimeoutConfig{
				StopTimeout:  DefaultStopTimeout,
				StartTimeout: DefaultStartTimeout,
				CloseTimeout: DefaultCloseTimeout,
			},
		},
		{
			name: "partial config",
			input: TimeoutConfig{
				StopTimeout: 20 * time.Second,
			},
			expected: TimeoutConfig{
				StopTimeout:  20 * time.Second,
				StartTimeout: DefaultStartTimeout,
				CloseTimeout: DefaultCloseTimeout,
			},
		},
		{
			name: "complete config",
			input: TimeoutConfig{
				StopTimeout:  15 * time.Second,
				StartTimeout: 25 * time.Second,
				CloseTimeout: 8 * time.Second,
			},
			expected: TimeoutConfig{
				StopTimeout:  15 * time.Second,
				StartTimeout: 25 * time.Second,
				CloseTimeout: 8 * time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.input.InitDefaults()
			assert.Equal(t, tt.expected, tt.input)
		})
	}
}

func TestTimeoutConfig_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		config   TimeoutConfig
		expected string
		wantErr  bool
	}{
		{
			name: "basic timeouts",
			config: TimeoutConfig{
				StopTimeout:  30 * time.Second,
				StartTimeout: 45 * time.Second,
				CloseTimeout: 15 * time.Second,
			},
			expected: `{"stop":"30s","update":"45s","close":"15s"}`,
			wantErr:  false,
		},
		{
			name:     "zero values",
			config:   TimeoutConfig{},
			expected: `{"stop":"0s","update":"0s","close":"0s"}`,
			wantErr:  false,
		},
		{
			name: "complex durations",
			config: TimeoutConfig{
				StopTimeout:  1*time.Hour + 30*time.Minute,
				StartTimeout: 2*time.Hour + 15*time.Minute,
				CloseTimeout: 45 * time.Minute,
			},
			expected: `{"stop":"1h30m0s","update":"2h15m0s","close":"45m0s"}`,
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

			// Verify roundtrip
			var decoded TimeoutConfig
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.config, decoded)
		})
	}
}

func TestTimeoutConfig_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		expected TimeoutConfig
		wantErr  bool
	}{
		{
			name: "valid timeouts",
			json: `{
				"stop": "30s",
				"update": "45s",
				"close": "15s"
			}`,
			expected: TimeoutConfig{
				StopTimeout:  30 * time.Second,
				StartTimeout: 45 * time.Second,
				CloseTimeout: 15 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "invalid stop timeout",
			json: `{
				"stop": "invalid",
				"update": "30s",
				"close": "15s"
			}`,
			wantErr: true,
		},
		{
			name: "invalid update timeout",
			json: `{
				"stop": "30s",
				"update": "invalid",
				"close": "15s"
			}`,
			wantErr: true,
		},
		{
			name: "invalid close timeout",
			json: `{
				"stop": "30s",
				"update": "45s",
				"close": "invalid"
			}`,
			wantErr: true,
		},
		{
			name:     "empty object",
			json:     `{}`,
			expected: TimeoutConfig{},
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var config TimeoutConfig
			err := json.Unmarshal([]byte(tt.json), &config)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, config)
		})
	}
}

func TestTimeoutConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  TimeoutConfig
		wantErr bool
	}{
		{
			name: "valid timeouts",
			config: TimeoutConfig{
				StopTimeout:  30 * time.Second,
				StartTimeout: 45 * time.Second,
				CloseTimeout: 15 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "negative stop timeout",
			config: TimeoutConfig{
				StopTimeout: -1 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "negative start timeout",
			config: TimeoutConfig{
				StartTimeout: -1 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "negative close timeout",
			config: TimeoutConfig{
				CloseTimeout: -1 * time.Second,
			},
			wantErr: true,
		},
		{
			name:    "zero values",
			config:  TimeoutConfig{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestServiceConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  ServiceConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: ServiceConfig{
				Meta:   registry.Metadata{"version": "1.0"},
				Target: "test-terminal",
				Timeouts: TimeoutConfig{
					StopTimeout:  30 * time.Second,
					StartTimeout: 45 * time.Second,
					CloseTimeout: 15 * time.Second,
				},
				Lifecycle: supervisor.LifecycleConfig{
					StartTimeout: 30 * time.Second,
					StopTimeout:  60 * time.Second,
				},
			},
			wantErr: false,
		},
		{
			name: "nil metadata",
			config: ServiceConfig{
				Target: "test-terminal",
			},
			wantErr: true,
		},
		{
			name: "empty target",
			config: ServiceConfig{
				Meta: registry.Metadata{"version": "1.0"},
			},
			wantErr: true,
		},
		{
			name: "invalid timeouts",
			config: ServiceConfig{
				Meta:   registry.Metadata{"version": "1.0"},
				Target: "test-terminal",
				Timeouts: TimeoutConfig{
					StopTimeout: -1 * time.Second,
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
