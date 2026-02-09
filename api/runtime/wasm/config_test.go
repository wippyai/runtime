package wasm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
)

func TestWATFunctionConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		errMsg  string
		config  WATFunctionConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: WATFunctionConfig{
				Source: "module",
				Method: "handle",
				Pool: PoolConfig{
					Size: 4,
				},
			},
		},
		{
			name: "valid with optional wit",
			config: WATFunctionConfig{
				Source: "module",
				Method: "handle",
				WIT:    "interface svc { handle: func() }",
				Pool: PoolConfig{
					Size: 1,
				},
			},
		},
		{
			name: "valid flex pool",
			config: WATFunctionConfig{
				Source: "module",
				Method: "handle",
				Pool: PoolConfig{
					MaxSize: 16,
				},
			},
		},
		{
			name: "valid with imports and transport",
			config: WATFunctionConfig{
				Source:    "module",
				Method:    "handle",
				Transport: TransportTypeWASIHTTP,
				Imports: []registry.ID{
					{Name: "server"},
				},
				Pool: PoolConfig{Type: PoolTypeInline},
			},
		},
		{
			name: "valid with wasi env and mounts",
			config: WATFunctionConfig{
				Source: "module",
				Method: "handle",
				WASI: WASIConfig{
					Cwd:  "/work",
					Args: []string{"--mode", "fast"},
					Env: []WASIEnvVarConfig{
						{
							ID:   registry.ParseID("app.env:api_key"),
							Name: "API_KEY",
						},
					},
					Mounts: []WASIMountConfig{
						{
							FS:       registry.ParseID("app.fs:data"),
							Guest:    "/data",
							ReadOnly: true,
						},
					},
				},
				Pool: PoolConfig{
					Size: 1,
				},
			},
		},
		{
			name: "missing source",
			config: WATFunctionConfig{
				Method: "handle",
			},
			wantErr: true,
			errMsg:  "source is required",
		},
		{
			name: "missing method",
			config: WATFunctionConfig{
				Source: "module",
			},
			wantErr: true,
			errMsg:  "method is required",
		},
		{
			name: "empty import name",
			config: WATFunctionConfig{
				Source: "module",
				Method: "handle",
				Imports: []registry.ID{
					{},
				},
			},
			wantErr: true,
			errMsg:  "import :name cannot be empty",
		},
		{
			name: "invalid transport",
			config: WATFunctionConfig{
				Source:    "module",
				Method:    "handle",
				Transport: "grpc",
			},
			wantErr: true,
			errMsg:  "invalid transport type",
		},
		{
			name: "invalid pool type",
			config: WATFunctionConfig{
				Source: "module",
				Method: "handle",
				Pool: PoolConfig{
					Type: "burst",
				},
			},
			wantErr: true,
			errMsg:  "invalid pool type",
		},
		{
			name: "negative pool value",
			config: WATFunctionConfig{
				Source: "module",
				Method: "handle",
				Pool: PoolConfig{
					Size: -1,
				},
			},
			wantErr: true,
			errMsg:  "pool values cannot be negative",
		},
		{
			name: "workers without size",
			config: WATFunctionConfig{
				Source: "module",
				Method: "handle",
				Pool: PoolConfig{
					Workers: 2,
				},
			},
			wantErr: true,
			errMsg:  "pool.size must be greater than 0 for non-flex pools",
		},
		{
			name: "negative limits",
			config: WATFunctionConfig{
				Source: "module",
				Method: "handle",
				Limits: LimitsConfig{
					MaxExecutionMS: -1,
				},
			},
			wantErr: true,
			errMsg:  "limits.max_execution_ms cannot be negative",
		},
		{
			name: "invalid wasi cwd relative",
			config: WATFunctionConfig{
				Source: "module",
				Method: "handle",
				WASI: WASIConfig{
					Cwd: "tmp",
				},
			},
			wantErr: true,
			errMsg:  "wasi.cwd must be absolute",
		},
		{
			name: "invalid wasi env missing id",
			config: WATFunctionConfig{
				Source: "module",
				Method: "handle",
				WASI: WASIConfig{
					Env: []WASIEnvVarConfig{
						{Name: "TOKEN"},
					},
				},
			},
			wantErr: true,
			errMsg:  "wasi.env[].id is required",
		},
		{
			name: "invalid wasi env duplicate names",
			config: WATFunctionConfig{
				Source: "module",
				Method: "handle",
				WASI: WASIConfig{
					Env: []WASIEnvVarConfig{
						{ID: registry.ParseID("app.env:a"), Name: "TOKEN"},
						{ID: registry.ParseID("app.env:b"), Name: "TOKEN"},
					},
				},
			},
			wantErr: true,
			errMsg:  "wasi.env[].name must be unique",
		},
		{
			name: "invalid wasi mount guest relative",
			config: WATFunctionConfig{
				Source: "module",
				Method: "handle",
				WASI: WASIConfig{
					Mounts: []WASIMountConfig{
						{
							FS:    registry.ParseID("app.fs:data"),
							Guest: "data",
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "wasi.mounts[].guest must be absolute",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestWATFunctionConfig_EffectiveTransport(t *testing.T) {
	cfg := &WATFunctionConfig{}
	assert.Equal(t, TransportTypePayload, cfg.EffectiveTransport())

	cfg.Transport = TransportTypeWASIHTTP
	assert.Equal(t, TransportTypeWASIHTTP, cfg.EffectiveTransport())
}

func TestFunctionConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		errMsg  string
		config  FunctionConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: FunctionConfig{
				FS:     "app:wasm",
				Path:   "/svc/handler.wasm",
				Hash:   "sha256:abc123",
				Method: "handle",
				Pool: PoolConfig{
					Size: 4,
				},
			},
		},
		{
			name: "valid with optional wit",
			config: FunctionConfig{
				FS:        "app:wasm",
				Path:      "/svc/handler.wasm",
				Hash:      "sha256:abc123",
				Method:    "handle",
				WIT:       "interface svc { handle: func() }",
				Transport: TransportTypePayload,
				Pool:      PoolConfig{Type: PoolTypeInline},
			},
		},
		{
			name: "valid with wasi mappings",
			config: FunctionConfig{
				FS:     "app:wasm",
				Path:   "/svc/handler.wasm",
				Hash:   "sha256:abc123",
				Method: "handle",
				WASI: WASIConfig{
					Env: []WASIEnvVarConfig{
						{
							ID:       registry.ParseID("app.env:api_key"),
							Name:     "API_KEY",
							Required: true,
						},
					},
					Mounts: []WASIMountConfig{
						{
							FS:    registry.ParseID("app.fs:assets"),
							Guest: "/assets",
						},
					},
				},
				Pool: PoolConfig{Size: 1},
			},
		},
		{
			name: "missing fs",
			config: FunctionConfig{
				Path:   "/svc/handler.wasm",
				Hash:   "sha256:abc123",
				Method: "handle",
			},
			wantErr: true,
			errMsg:  "fs is required",
		},
		{
			name: "missing path",
			config: FunctionConfig{
				FS:     "app:wasm",
				Hash:   "sha256:abc123",
				Method: "handle",
			},
			wantErr: true,
			errMsg:  "path is required",
		},
		{
			name: "missing hash",
			config: FunctionConfig{
				FS:     "app:wasm",
				Path:   "/svc/handler.wasm",
				Method: "handle",
			},
			wantErr: true,
			errMsg:  "hash is required",
		},
		{
			name: "missing method",
			config: FunctionConfig{
				FS:   "app:wasm",
				Path: "/svc/handler.wasm",
				Hash: "sha256:abc123",
			},
			wantErr: true,
			errMsg:  "method is required",
		},
		{
			name: "invalid transport",
			config: FunctionConfig{
				FS:        "app:wasm",
				Path:      "/svc/handler.wasm",
				Hash:      "sha256:abc123",
				Method:    "handle",
				Transport: "grpc",
			},
			wantErr: true,
			errMsg:  "invalid transport type",
		},
		{
			name: "negative limits",
			config: FunctionConfig{
				FS:     "app:wasm",
				Path:   "/svc/handler.wasm",
				Hash:   "sha256:abc123",
				Method: "handle",
				Limits: LimitsConfig{
					MaxExecutionMS: -10,
				},
			},
			wantErr: true,
			errMsg:  "limits.max_execution_ms cannot be negative",
		},
		{
			name: "workers without size",
			config: FunctionConfig{
				FS:     "app:wasm",
				Path:   "/svc/handler.wasm",
				Hash:   "sha256:abc123",
				Method: "handle",
				Pool: PoolConfig{
					Workers: 2,
				},
			},
			wantErr: true,
			errMsg:  "pool.size must be greater than 0 for non-flex pools",
		},
		{
			name: "invalid wasi mount duplicate guest",
			config: FunctionConfig{
				FS:     "app:wasm",
				Path:   "/svc/handler.wasm",
				Hash:   "sha256:abc123",
				Method: "handle",
				WASI: WASIConfig{
					Mounts: []WASIMountConfig{
						{FS: registry.ParseID("app.fs:a"), Guest: "/data"},
						{FS: registry.ParseID("app.fs:b"), Guest: "/data"},
					},
				},
			},
			wantErr: true,
			errMsg:  "wasi.mounts[].guest must be unique",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestFunctionConfig_EffectiveTransport(t *testing.T) {
	cfg := &FunctionConfig{}
	assert.Equal(t, TransportTypePayload, cfg.EffectiveTransport())

	cfg.Transport = TransportTypeWASIHTTP
	assert.Equal(t, TransportTypeWASIHTTP, cfg.EffectiveTransport())
}
