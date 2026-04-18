// SPDX-License-Identifier: MPL-2.0

// Package http provides HTTP service configuration.
package http

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

func TestTimeoutConfig_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		config   TimeoutConfig
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
			expected: `{}`,
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
				Addr:    ":8080",
				Network: registry.NewID("app.net", "overlay"),
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
				Addr:    ":8080",
				Network: registry.NewID("app.net", "overlay"),
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
					assert.Contains(t, err.Error(), "must be non-negative")
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
		assert.Contains(t, err.Error(), "path is required")
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
		assert.Contains(t, err.Error(), "metadata is required")
	})

	t.Run("missing server in metadata", func(t *testing.T) {
		config := &StaticConfig{
			Path: "/static",
			Meta: attrs.Bag{},
		}
		err := config.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "server metadata is required")
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

	t.Run("non-string option values are converted", func(t *testing.T) {
		jsonData := `{
			"meta": {},
			"path": "/app",
			"fs": {"ns": "fs", "name": "public"},
			"directory": "/",
			"options": {
				"timeout": 30,
				"enabled": true,
				"rate": 1.5
			}
		}`

		var config StaticConfig
		err := json.Unmarshal([]byte(jsonData), &config)
		require.NoError(t, err)

		assert.Equal(t, "30", config.Options["timeout"])
		assert.Equal(t, "true", config.Options["enabled"])
		assert.Equal(t, "1.5", config.Options["rate"])
	})
}

func TestCommandIDs(t *testing.T) {
	assert.Equal(t, dispatcher.CommandID(60), Request)
	assert.Equal(t, dispatcher.CommandID(61), RequestBatch)
}

func TestRequestCmd(t *testing.T) {
	cmd := AcquireRequestCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, Request, cmd.CmdID())

	cmd.Method = "POST"
	cmd.URL = "https://example.com/api"
	cmd.Headers = map[string][]string{"Content-Type": {"application/json"}}
	cmd.Body = []byte(`{"test": true}`)
	cmd.Timeout = 30 * time.Second
	cmd.UnixSocket = "/var/run/socket.sock"
	cmd.Query = map[string]string{"q": "test"}
	cmd.Cookies = map[string]string{"session": "abc"}
	cmd.Form = map[string]string{"field": "value"}
	cmd.Files = []FileUpload{{FieldName: "file", FileName: "test.txt", Data: []byte("content")}}
	cmd.BasicAuthUser = "user"
	cmd.BasicAuthPass = "pass"
	cmd.Stream = true
	cmd.MaxResponseBody = 1024
	cmd.Release()

	cmd2 := AcquireRequestCmd()
	assert.Empty(t, cmd2.Method)
	assert.Empty(t, cmd2.URL)
	assert.Nil(t, cmd2.Headers)
	assert.Nil(t, cmd2.Body)
	assert.Zero(t, cmd2.Timeout)
	assert.Empty(t, cmd2.UnixSocket)
	assert.Nil(t, cmd2.Query)
	assert.Nil(t, cmd2.Cookies)
	assert.Nil(t, cmd2.Form)
	assert.Nil(t, cmd2.Files)
	assert.Empty(t, cmd2.BasicAuthUser)
	assert.Empty(t, cmd2.BasicAuthPass)
	assert.False(t, cmd2.Stream)
	assert.Zero(t, cmd2.MaxResponseBody)
	cmd2.Release()
}

func TestRequestBatchCmd(t *testing.T) {
	cmd := AcquireRequestBatchCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, RequestBatch, cmd.CmdID())

	req := AcquireRequestCmd()
	req.URL = "https://example.com"
	cmd.Requests = []*RequestCmd{req}
	cmd.Release()

	cmd2 := AcquireRequestBatchCmd()
	assert.Nil(t, cmd2.Requests)
	cmd2.Release()
}

func TestResponse(t *testing.T) {
	resp := Response{
		StatusCode: 200,
		Headers:    map[string][]string{"Content-Type": {"application/json"}},
		Cookies:    map[string]string{"session": "xyz"},
		Body:       []byte(`{"ok": true}`),
		URL:        "https://example.com/api",
		StreamID:   123,
	}

	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, []string{"application/json"}, resp.Headers["Content-Type"])
	assert.Equal(t, "xyz", resp.Cookies["session"])
	assert.NotNil(t, resp.Body)
	assert.Equal(t, uint64(123), resp.StreamID)
}

func TestBatchResponse(t *testing.T) {
	batch := BatchResponse{
		Responses: []Response{
			{StatusCode: 200},
			{StatusCode: 201},
		},
	}
	assert.Len(t, batch.Responses, 2)
}

func TestFileUpload(t *testing.T) {
	file := FileUpload{
		FieldName: "document",
		FileName:  "report.pdf",
		Data:      []byte("pdf content"),
	}

	assert.Equal(t, "document", file.FieldName)
	assert.Equal(t, "report.pdf", file.FileName)
	assert.NotNil(t, file.Data)
}

func TestServerTLSConfig_Validate(t *testing.T) {
	tests := []struct {
		wantErr error
		tls     ServerTLSConfig
		name    string
	}{
		{
			name: "off with no inputs is valid",
			tls:  ServerTLSConfig{Mode: TLSModeOff},
		},
		{
			name:    "off rejects cert inputs",
			tls:     ServerTLSConfig{Mode: TLSModeOff, Cert: "x", Key: "y"},
			wantErr: ErrTLSOffHasInputs,
		},
		{
			name:    "off rejects mTLS inputs",
			tls:     ServerTLSConfig{Mode: TLSModeOff, ClientAuth: ClientAuthRequest},
			wantErr: ErrTLSOffHasInputs,
		},
		{
			name: "auto with no inputs is valid",
			tls:  ServerTLSConfig{Mode: TLSModeAuto},
		},
		{
			name:    "auto rejects cert inputs",
			tls:     ServerTLSConfig{Mode: TLSModeAuto, Cert: "x", Key: "y"},
			wantErr: ErrTLSAutoHasCertInputs,
		},
		{
			name:    "auto rejects mTLS",
			tls:     ServerTLSConfig{Mode: TLSModeAuto, ClientAuth: ClientAuthRequest},
			wantErr: ErrTLSMTLSRequiresManual,
		},
		{
			name:    "manual requires some cert input",
			tls:     ServerTLSConfig{Mode: TLSModeManual},
			wantErr: ErrTLSManualMissingCert,
		},
		{
			name: "manual inline cert is valid",
			tls:  ServerTLSConfig{Mode: TLSModeManual, Cert: "pem", Key: "pem"},
		},
		{
			name: "manual env cert is valid",
			tls:  ServerTLSConfig{Mode: TLSModeManual, CertEnv: "A", KeyEnv: "B"},
		},
		{
			name:    "manual inline+env is ambiguous",
			tls:     ServerTLSConfig{Mode: TLSModeManual, Cert: "pem", Key: "pem", CertEnv: "A", KeyEnv: "B"},
			wantErr: ErrTLSManualAmbiguousCert,
		},
		{
			name:    "manual inline partial (cert without key)",
			tls:     ServerTLSConfig{Mode: TLSModeManual, Cert: "pem"},
			wantErr: ErrTLSManualPartialCert,
		},
		{
			name:    "manual env partial (cert_env without key_env)",
			tls:     ServerTLSConfig{Mode: TLSModeManual, CertEnv: "A"},
			wantErr: ErrTLSManualPartialCertEnv,
		},
		{
			name:    "manual inline partial after env set",
			tls:     ServerTLSConfig{Mode: TLSModeManual, Cert: "pem", CertEnv: "A", KeyEnv: "B"},
			wantErr: ErrTLSManualPartialCert,
		},
		{
			name:    "manual env partial after inline set",
			tls:     ServerTLSConfig{Mode: TLSModeManual, Cert: "pem", Key: "pem", CertEnv: "A"},
			wantErr: ErrTLSManualPartialCertEnv,
		},
		{
			name:    "manual mTLS with both CA sources",
			tls:     ServerTLSConfig{Mode: TLSModeManual, Cert: "pem", Key: "pem", ClientCA: "ca", ClientCAEnv: "X", ClientAuth: ClientAuthRequireAndVerify},
			wantErr: ErrTLSMTLSAmbiguousCA,
		},
		{
			name:    "manual CA without auth",
			tls:     ServerTLSConfig{Mode: TLSModeManual, Cert: "pem", Key: "pem", ClientCA: "ca"},
			wantErr: ErrTLSMTLSCAWithoutAuth,
		},
		{
			name:    "manual verify_if_given without CA",
			tls:     ServerTLSConfig{Mode: TLSModeManual, Cert: "pem", Key: "pem", ClientAuth: ClientAuthVerifyIfGiven},
			wantErr: ErrTLSMTLSMissingCA,
		},
		{
			name:    "manual require_and_verify without CA",
			tls:     ServerTLSConfig{Mode: TLSModeManual, Cert: "pem", Key: "pem", ClientAuth: ClientAuthRequireAndVerify},
			wantErr: ErrTLSMTLSMissingCA,
		},
		{
			name: "manual request auth without CA is fine",
			tls:  ServerTLSConfig{Mode: TLSModeManual, Cert: "pem", Key: "pem", ClientAuth: ClientAuthRequest},
		},
		{
			name: "manual require_any without CA is fine",
			tls:  ServerTLSConfig{Mode: TLSModeManual, Cert: "pem", Key: "pem", ClientAuth: ClientAuthRequireAny},
		},
		{
			name: "manual full mTLS inline is valid",
			tls:  ServerTLSConfig{Mode: TLSModeManual, Cert: "pem", Key: "pem", ClientCA: "ca", ClientAuth: ClientAuthRequireAndVerify},
		},
		{
			name: "manual full mTLS env is valid",
			tls:  ServerTLSConfig{Mode: TLSModeManual, CertEnv: "A", KeyEnv: "B", ClientCAEnv: "C", ClientAuth: ClientAuthRequireAndVerify},
		},
		{
			name:    "invalid tls mode",
			tls:     ServerTLSConfig{Mode: "bogus"},
			wantErr: nil, // message asserted below
		},
		{
			name:    "invalid client_auth value",
			tls:     ServerTLSConfig{Mode: TLSModeManual, Cert: "pem", Key: "pem", ClientAuth: ClientAuthType("handwavy")},
			wantErr: nil, // message asserted below
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.tls.Validate()
			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			if tt.name == "invalid tls mode" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "invalid tls.mode")
				return
			}
			if tt.name == "invalid client_auth value" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "invalid tls.client_auth")
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestErrorConstants(t *testing.T) {
	assert.Contains(t, ErrEmptyAddr.Error(), "address is required")
	assert.Contains(t, ErrNilMetadata.Error(), "metadata is required")
	assert.Contains(t, ErrEmptyFuncName.Error(), "function name is required")
	assert.Contains(t, ErrEmptyPath.Error(), "path is required")
	assert.Contains(t, ErrEmptyMethod.Error(), "method is required")
}

func TestErrorFactories(t *testing.T) {
	t.Run("NewMissingMetadataError", func(t *testing.T) {
		err := NewMissingMetadataError("router")
		assert.Contains(t, err.Error(), "router metadata is required")
	})

	t.Run("NewPathMustStartWithSlashError", func(t *testing.T) {
		err := NewPathMustStartWithSlashError()
		assert.Contains(t, err.Error(), "must start with /")
	})

	t.Run("NewInvalidHTTPMethodError", func(t *testing.T) {
		err := NewInvalidHTTPMethodError("PATCH2")
		assert.Contains(t, err.Error(), "invalid HTTP method: PATCH2")
	})

	t.Run("NewInvalidTimeoutConfigError", func(t *testing.T) {
		cause := errors.New("config error")
		err := NewInvalidTimeoutConfigError(cause)
		assert.Contains(t, err.Error(), "invalid timeout configuration")
	})

	t.Run("NewInvalidTimeoutError", func(t *testing.T) {
		err := NewInvalidTimeoutError("read_timeout")
		assert.Contains(t, err.Error(), "read_timeout must be non-negative")
	})

	t.Run("NewNegativeConfigError", func(t *testing.T) {
		err := NewNegativeConfigError("max_connections")
		assert.Contains(t, err.Error(), "max_connections must be non-negative")
	})
}
