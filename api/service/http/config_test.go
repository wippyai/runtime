package http

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
				ReadTimeout:  30 * time.Second,
				WriteTimeout: 45 * time.Second,
				IdleTimeout:  60 * time.Second,
			},
			expected: `{"read":"30s","write":"45s","idle":"1m0s"}`,
			wantErr:  false,
		},
		{
			name:     "zero values",
			config:   TimeoutConfig{},
			expected: `{"read":"0s","write":"0s","idle":"0s"}`,
			wantErr:  false,
		},
		{
			name: "complex durations",
			config: TimeoutConfig{
				ReadTimeout:  1*time.Hour + 30*time.Minute,
				WriteTimeout: 2*time.Hour + 15*time.Minute,
				IdleTimeout:  45*time.Minute + 30*time.Second,
			},
			expected: `{"read":"1h30m0s","write":"2h15m0s","idle":"45m30s"}`,
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
				"read": "30s",
				"write": "45s",
				"idle": "1m"
			}`,
			expected: TimeoutConfig{
				ReadTimeout:  30 * time.Second,
				WriteTimeout: 45 * time.Second,
				IdleTimeout:  60 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "invalid read timeout",
			json: `{
				"read": "invalid",
				"write": "30s",
				"idle": "1m"
			}`,
			wantErr: true,
		},
		{
			name: "invalid write timeout",
			json: `{
				"read": "30s",
				"write": "invalid",
				"idle": "1m"
			}`,
			wantErr: true,
		},
		{
			name: "invalid idle timeout",
			json: `{
				"read": "30s",
				"write": "45s",
				"idle": "invalid"
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

func TestServerConfig_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		config  ServerConfig
		wantErr bool
	}{
		{
			name: "full config",
			config: ServerConfig{
				Meta: registry.Metadata{"version": "1.0"},
				Addr: ":8080",
				Timeouts: TimeoutConfig{
					ReadTimeout:  30 * time.Second,
					WriteTimeout: 45 * time.Second,
					IdleTimeout:  60 * time.Second,
				},
				Lifecycle: supervisor.LifecycleConfig{
					AutoStart:    true,
					StartTimeout: 30 * time.Second,
					StopTimeout:  60 * time.Second,
					RetryPolicy: supervisor.RetryPolicy{
						InitialDelay:  1 * time.Second,
						MaxDelay:      30 * time.Second,
						BackoffFactor: 2.0,
						Jitter:        0.1,
						MaxAttempts:   5,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "minimal config",
			config: ServerConfig{
				Meta: registry.Metadata{},
				Addr: ":8080",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal
			data, err := json.Marshal(&tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Unmarshal and compare
			var decoded ServerConfig
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.config, decoded)
		})
	}
}

func TestServerConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  ServerConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: ServerConfig{
				Addr: ":8080",
				Timeouts: TimeoutConfig{
					ReadTimeout:  30 * time.Second,
					WriteTimeout: 45 * time.Second,
					IdleTimeout:  60 * time.Second,
				},
			},
			wantErr: false,
		},
		{
			name: "empty address",
			config: ServerConfig{
				Addr: "",
			},
			wantErr: true,
		},
		{
			name: "negative read timeout",
			config: ServerConfig{
				Addr: ":8080",
				Timeouts: TimeoutConfig{
					ReadTimeout: -1 * time.Second,
				},
			},
			wantErr: true,
		},
		{
			name: "negative write timeout",
			config: ServerConfig{
				Addr: ":8080",
				Timeouts: TimeoutConfig{
					WriteTimeout: -1 * time.Second,
				},
			},
			wantErr: true,
		},
		{
			name: "negative idle timeout",
			config: ServerConfig{
				Addr: ":8080",
				Timeouts: TimeoutConfig{
					IdleTimeout: -1 * time.Second,
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

func TestRouterConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  RouterConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: RouterConfig{
				Meta:        registry.Metadata{ServerID: "test-server"},
				Prefix:      "/api",
				Middlewares: []string{"timeout", "recoverer"},
				Options: map[string]string{
					"timeout": "30s",
				},
			},
			wantErr: false,
		},
		{
			name: "nil metadata",
			config: RouterConfig{
				Meta:   nil,
				Prefix: "/api",
			},
			wantErr: true,
		},
		{
			name: "missing server ID",
			config: RouterConfig{
				Meta:   registry.Metadata{},
				Prefix: "/api",
			},
			wantErr: true,
		},
		{
			name: "invalid middleware",
			config: RouterConfig{
				Meta:        registry.Metadata{ServerID: "test-server"},
				Prefix:      "/api",
				Middlewares: []string{"invalid"},
			},
			wantErr: true,
		},
		{
			name: "invalid timeout value",
			config: RouterConfig{
				Meta:        registry.Metadata{ServerID: "test-server"},
				Prefix:      "/api",
				Middlewares: []string{"timeout"},
				Options: map[string]string{
					"timeout": "invalid",
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

func TestEndpointConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  EndpointConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: EndpointConfig{
				Meta:   registry.Metadata{ServerID: "test-server"},
				Path:   "/test",
				Method: "GET",
				Target: "test_handler",
			},
			wantErr: false,
		},
		{
			name: "empty path",
			config: EndpointConfig{
				Meta:   registry.Metadata{ServerID: "test-server"},
				Method: "GET",
			},
			wantErr: true,
		},
		{
			name: "path without leading slash",
			config: EndpointConfig{
				Meta:   registry.Metadata{ServerID: "test-server"},
				Path:   "test",
				Method: "GET",
			},
			wantErr: true,
		},
		{
			name: "empty method",
			config: EndpointConfig{
				Meta: registry.Metadata{ServerID: "test-server"},
				Path: "/test",
			},
			wantErr: true,
		},
		{
			name: "invalid method",
			config: EndpointConfig{
				Meta:   registry.Metadata{ServerID: "test-server"},
				Path:   "/test",
				Method: "INVALID",
			},
			wantErr: true,
		},
		{
			name: "nil metadata",
			config: EndpointConfig{
				Path:   "/test",
				Method: "GET",
			},
			wantErr: true,
		},
		{
			name: "missing server ID",
			config: EndpointConfig{
				Meta:   registry.Metadata{},
				Path:   "/test",
				Method: "GET",
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

func TestServerConfig_Validate_Lifecycle(t *testing.T) {
	tests := []struct {
		name    string
		config  ServerConfig
		wantErr bool
	}{
		{
			name: "valid lifecycle timeouts",
			config: ServerConfig{
				Addr: ":8080",
				Lifecycle: supervisor.LifecycleConfig{
					StartTimeout: 30 * time.Second,
					StopTimeout:  60 * time.Second,
				},
			},
			wantErr: false,
		},
		{
			name: "zero lifecycle timeouts",
			config: ServerConfig{
				Addr: ":8080",
				Lifecycle: supervisor.LifecycleConfig{
					StartTimeout: 0,
					StopTimeout:  0,
				},
			},
			wantErr: false,
		},
		{
			name: "negative start timeout",
			config: ServerConfig{
				Addr: ":8080",
				Lifecycle: supervisor.LifecycleConfig{
					StartTimeout: -1 * time.Second,
					StopTimeout:  30 * time.Second,
				},
			},
			wantErr: true,
		},
		{
			name: "negative stop timeout",
			config: ServerConfig{
				Addr: ":8080",
				Lifecycle: supervisor.LifecycleConfig{
					StartTimeout: 30 * time.Second,
					StopTimeout:  -1 * time.Second,
				},
			},
			wantErr: true,
		},
		{
			name: "both negative timeouts",
			config: ServerConfig{
				Addr: ":8080",
				Lifecycle: supervisor.LifecycleConfig{
					StartTimeout: -1 * time.Second,
					StopTimeout:  -1 * time.Second,
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
				if err != nil {
					assert.Contains(t, err.Error(), "timeout must be positive or zero")
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestServerConfig_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{
			name: "valid json",
			json: `{
				"addr": ":8080",
				"meta": {"version": "1.0"},
				"timeouts": {
					"read": "30s",
					"write": "45s",
					"idle": "60s"
				},
				"lifecycle": {
					"auto_start": true,
					"start_timeout": "30s",
					"stop_timeout": "60s"
				}
			}`,
			wantErr: false,
		},
		{
			name:    "invalid json syntax",
			json:    `{invalid json}`,
			wantErr: true,
		},
		{
			name: "invalid timeout format",
			json: `{
				"addr": ":8080",
				"timeouts": {
					"read": "invalid"
				}
			}`,
			wantErr: true,
		},
		{
			name: "invalid lifecycle timeout format",
			json: `{
				"addr": ":8080",
				"lifecycle": {
					"start_timeout": "invalid"
				}
			}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var config ServerConfig
			err := json.Unmarshal([]byte(tt.json), &config)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
