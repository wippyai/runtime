// Package http provides HTTP service configuration.
package http

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
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
				ReadTimeout:  (30 * time.Second),
				WriteTimeout: (45 * time.Second),
				IdleTimeout:  (60 * time.Second),
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
				ReadTimeout:  (1*time.Hour + 30*time.Minute),
				WriteTimeout: (2*time.Hour + 15*time.Minute),
				IdleTimeout:  (45*time.Minute + 30*time.Second),
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
				ReadTimeout:  (30 * time.Second),
				WriteTimeout: (45 * time.Second),
				IdleTimeout:  (60 * time.Second),
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
				Addr: ":8080",
				Timeouts: TimeoutConfig{
					ReadTimeout:  (30 * time.Second),
					WriteTimeout: (45 * time.Second),
					IdleTimeout:  (60 * time.Second),
				},
				Lifecycle: supervisor.LifecycleConfig{
					AutoStart:    true,
					StartTimeout: (30 * time.Second),
					StopTimeout:  (60 * time.Second),
					RetryPolicy: supervisor.RetryPolicy{
						InitialDelay:  (1 * time.Second),
						MaxDelay:      (30 * time.Second),
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
					ReadTimeout:  (30 * time.Second),
					WriteTimeout: (45 * time.Second),
					IdleTimeout:  (60 * time.Second),
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
				Meta:       attrs.Bag{ServerID: "test-server"},
				Prefix:     "/api",
				Middleware: []string{"timeout", "recoverer"},
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
			name: "missing server alias",
			config: RouterConfig{
				Meta:   attrs.Bag{},
				Prefix: "/api",
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
				Meta: attrs.Bag{
					RouterID: "test-router", // Added required RouterID
				},
				Path:   "/test",
				Method: "GET",
				Func:   registry.NewID("default", "test_handler"),
			},
			wantErr: false,
		},
		{
			name: "empty path",
			config: EndpointConfig{
				Meta: attrs.Bag{
					RouterID: "test-router",
				},
				Method: "GET",
				Func:   registry.NewID("default", "test_handler"),
			},
			wantErr: true,
		},
		{
			name: "path without leading slash",
			config: EndpointConfig{
				Meta: attrs.Bag{
					RouterID: "test-router",
				},
				Path:   "test",
				Method: "GET",
				Func:   registry.NewID("default", "test_handler"),
			},
			wantErr: true,
		},
		{
			name: "empty method",
			config: EndpointConfig{
				Meta: attrs.Bag{
					RouterID: "test-router",
				},
				Path: "/test",
				Func: registry.NewID("default", "test_handler"),
			},
			wantErr: true,
		},
		{
			name: "invalid method",
			config: EndpointConfig{
				Meta: attrs.Bag{
					RouterID: "test-router",
				},
				Path:   "/test",
				Method: "INVALID",
				Func:   registry.NewID("default", "test_handler"),
			},
			wantErr: true,
		},
		{
			name: "nil metadata",
			config: EndpointConfig{
				Path:   "/test",
				Method: "GET",
				Func:   registry.NewID("default", "test_handler"),
			},
			wantErr: true,
		},
		{
			name: "missing router Source",
			config: EndpointConfig{
				Meta:   attrs.Bag{},
				Path:   "/test",
				Method: "GET",
				Func:   registry.NewID("default", "test_handler"),
			},
			wantErr: true,
		},
		{
			name: "empty function name",
			config: EndpointConfig{
				Meta: attrs.Bag{
					RouterID: "test-router",
				},
				Path:   "/test",
				Method: "GET",
				Func:   registry.NewID("default", ""),
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
					StartTimeout: (30 * time.Second),
					StopTimeout:  (60 * time.Second),
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
					StartTimeout: (-1 * time.Second),
					StopTimeout:  (30 * time.Second),
				},
			},
			wantErr: true,
		},
		{
			name: "negative stop timeout",
			config: ServerConfig{
				Addr: ":8080",
				Lifecycle: supervisor.LifecycleConfig{
					StartTimeout: (30 * time.Second),
					StopTimeout:  (-1 * time.Second),
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

func TestServerConfig_SetMeta(t *testing.T) {
	t.Run("sets meta when nil", func(t *testing.T) {
		config := &ServerConfig{}
		meta := attrs.Bag{"key": "value"}
		config.SetMeta(meta)
		assert.Equal(t, meta, config.Meta)
	})

	t.Run("does not override existing meta", func(t *testing.T) {
		existingMeta := attrs.Bag{"existing": "value"}
		config := &ServerConfig{Meta: existingMeta}
		newMeta := attrs.Bag{"new": "value"}
		config.SetMeta(newMeta)
		assert.Equal(t, existingMeta, config.Meta)
	})
}

func TestRouterConfig_SetMeta(t *testing.T) {
	t.Run("sets meta when nil", func(t *testing.T) {
		config := &RouterConfig{}
		meta := attrs.Bag{"key": "value"}
		config.SetMeta(meta)
		assert.Equal(t, meta, config.Meta)
	})

	t.Run("does not override existing meta", func(t *testing.T) {
		existingMeta := attrs.Bag{"existing": "value"}
		config := &RouterConfig{Meta: existingMeta}
		newMeta := attrs.Bag{"new": "value"}
		config.SetMeta(newMeta)
		assert.Equal(t, existingMeta, config.Meta)
	})
}

func TestEndpointConfig_SetMeta(t *testing.T) {
	t.Run("sets meta when nil", func(t *testing.T) {
		config := &EndpointConfig{}
		meta := attrs.Bag{"key": "value"}
		config.SetMeta(meta)
		assert.Equal(t, meta, config.Meta)
	})

	t.Run("does not override existing meta", func(t *testing.T) {
		existingMeta := attrs.Bag{"existing": "value"}
		config := &EndpointConfig{Meta: existingMeta}
		newMeta := attrs.Bag{"new": "value"}
		config.SetMeta(newMeta)
		assert.Equal(t, existingMeta, config.Meta)
	})
}

func TestStaticConfig_SetMeta(t *testing.T) {
	t.Run("sets meta when nil", func(t *testing.T) {
		config := &StaticConfig{}
		meta := attrs.Bag{"key": "value"}
		config.SetMeta(meta)
		assert.Equal(t, meta, config.Meta)
	})

	t.Run("does not override existing meta", func(t *testing.T) {
		existingMeta := attrs.Bag{"existing": "value"}
		config := &StaticConfig{Meta: existingMeta}
		newMeta := attrs.Bag{"new": "value"}
		config.SetMeta(newMeta)
		assert.Equal(t, existingMeta, config.Meta)
	})
}

func TestStaticConfig_Validate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		config := &StaticConfig{
			Path: "/static",
			Meta: attrs.Bag{
				ServerID: "server1",
			},
		}
		err := config.Validate()
		assert.NoError(t, err)
	})

	t.Run("empty path", func(t *testing.T) {
		config := &StaticConfig{
			Path: "",
			Meta: attrs.Bag{ServerID: "server1"},
		}
		err := config.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "path cannot be empty")
	})

	t.Run("path without leading slash", func(t *testing.T) {
		config := &StaticConfig{
			Path: "static",
			Meta: attrs.Bag{ServerID: "server1"},
		}
		err := config.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must start with /")
	})

	t.Run("nil metadata", func(t *testing.T) {
		config := &StaticConfig{
			Path: "/static",
			Meta: nil,
		}
		err := config.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "metadata cannot be nil")
	})

	t.Run("missing server in metadata", func(t *testing.T) {
		config := &StaticConfig{
			Path: "/static",
			Meta: attrs.Bag{},
		}
		err := config.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "server in metadata cannot be empty")
	})
}

func TestRequestContext_Pooling(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	t.Run("SetRequest", func(t *testing.T) {
		reqCtx := &RequestContext{}
		reqCtx.SetRequest(req)
		assert.Equal(t, req, reqCtx.Request())
	})

	t.Run("SetResponseWriter", func(t *testing.T) {
		reqCtx := &RequestContext{}
		reqCtx.SetResponseWriter(w)
		assert.Equal(t, w, reqCtx.ResponseWriter())
	})

	t.Run("ResetHandled", func(t *testing.T) {
		reqCtx := &RequestContext{}
		reqCtx.MarkHandled()
		assert.True(t, reqCtx.ResponseHandled())
		reqCtx.ResetHandled()
		assert.False(t, reqCtx.ResponseHandled())
	})
}

func TestStaticConfig_UnmarshalJSON_BackwardCompatibility(t *testing.T) {
	t.Run("migrate spa from options map (bool)", func(t *testing.T) {
		jsonData := `{
			"meta": {},
			"path": "/app",
			"fs": {"ns": "fs", "name": "public"},
			"directory": "/",
			"options": {
				"spa": true,
				"index": "index.html",
				"cache": "public, max-age=3600"
			}
		}`

		var config StaticConfig
		err := json.Unmarshal([]byte(jsonData), &config)
		require.NoError(t, err)

		assert.True(t, config.StaticOptions.SPA)
		assert.Equal(t, "index.html", config.StaticOptions.IndexFile)
		assert.Equal(t, "public, max-age=3600", config.StaticOptions.CacheControl)
		assert.Empty(t, config.Options)
	})

	t.Run("migrate spa from options map (string true)", func(t *testing.T) {
		jsonData := `{
			"meta": {},
			"path": "/app",
			"fs": {"ns": "fs", "name": "public"},
			"directory": "/",
			"options": {
				"spa": "true"
			}
		}`

		var config StaticConfig
		err := json.Unmarshal([]byte(jsonData), &config)
		require.NoError(t, err)

		assert.True(t, config.StaticOptions.SPA)
	})

	t.Run("spa false as string", func(t *testing.T) {
		jsonData := `{
			"meta": {},
			"path": "/app",
			"fs": {"ns": "fs", "name": "public"},
			"directory": "/",
			"options": {
				"spa": "false"
			}
		}`

		var config StaticConfig
		err := json.Unmarshal([]byte(jsonData), &config)
		require.NoError(t, err)

		assert.False(t, config.StaticOptions.SPA)
	})

	t.Run("new format with static_options", func(t *testing.T) {
		jsonData := `{
			"meta": {},
			"path": "/app",
			"fs": {"ns": "fs", "name": "public"},
			"directory": "/",
			"static_options": {
				"spa": true,
				"index": "index.html",
				"cache": "public, max-age=3600"
			}
		}`

		var config StaticConfig
		err := json.Unmarshal([]byte(jsonData), &config)
		require.NoError(t, err)

		assert.True(t, config.StaticOptions.SPA)
		assert.Equal(t, "index.html", config.StaticOptions.IndexFile)
		assert.Equal(t, "public, max-age=3600", config.StaticOptions.CacheControl)
	})

	t.Run("middleware options preserved", func(t *testing.T) {
		jsonData := `{
			"meta": {},
			"path": "/app",
			"fs": {"ns": "fs", "name": "public"},
			"directory": "/",
			"options": {
				"cors.allow.origins": "*",
				"compress.level": "best"
			}
		}`

		var config StaticConfig
		err := json.Unmarshal([]byte(jsonData), &config)
		require.NoError(t, err)

		assert.Equal(t, "*", config.Options["cors.allow.origins"])
		assert.Equal(t, "best", config.Options["compress.level"])
	})

	t.Run("mixed old and new options", func(t *testing.T) {
		jsonData := `{
			"meta": {},
			"path": "/app",
			"fs": {"ns": "fs", "name": "public"},
			"directory": "/",
			"options": {
				"spa": true,
				"cors.allow.origins": "*"
			},
			"static_options": {
				"index": "app.html"
			}
		}`

		var config StaticConfig
		err := json.Unmarshal([]byte(jsonData), &config)
		require.NoError(t, err)

		assert.True(t, config.StaticOptions.SPA)
		assert.Equal(t, "app.html", config.StaticOptions.IndexFile)
		assert.Equal(t, "*", config.Options["cors.allow.origins"])
	})
}
