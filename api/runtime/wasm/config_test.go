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
				Imports: map[string]registry.ID{
					"http": {Name: "server"},
				},
				Pool: PoolConfig{Type: PoolTypeInline},
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
			name: "empty import alias",
			config: WATFunctionConfig{
				Source: "module",
				Method: "handle",
				Imports: map[string]registry.ID{
					"": {Name: "dep"},
				},
			},
			wantErr: true,
			errMsg:  "import alias cannot be empty",
		},
		{
			name: "empty import name",
			config: WATFunctionConfig{
				Source: "module",
				Method: "handle",
				Imports: map[string]registry.ID{
					"dep": {Name: ""},
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

func TestWASMFunctionConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		errMsg  string
		config  WASMFunctionConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: WASMFunctionConfig{
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
			config: WASMFunctionConfig{
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
			name: "missing fs",
			config: WASMFunctionConfig{
				Path:   "/svc/handler.wasm",
				Hash:   "sha256:abc123",
				Method: "handle",
			},
			wantErr: true,
			errMsg:  "fs is required",
		},
		{
			name: "missing path",
			config: WASMFunctionConfig{
				FS:     "app:wasm",
				Hash:   "sha256:abc123",
				Method: "handle",
			},
			wantErr: true,
			errMsg:  "path is required",
		},
		{
			name: "missing hash",
			config: WASMFunctionConfig{
				FS:     "app:wasm",
				Path:   "/svc/handler.wasm",
				Method: "handle",
			},
			wantErr: true,
			errMsg:  "hash is required",
		},
		{
			name: "missing method",
			config: WASMFunctionConfig{
				FS:   "app:wasm",
				Path: "/svc/handler.wasm",
				Hash: "sha256:abc123",
			},
			wantErr: true,
			errMsg:  "method is required",
		},
		{
			name: "invalid transport",
			config: WASMFunctionConfig{
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
			config: WASMFunctionConfig{
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
			config: WASMFunctionConfig{
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

func TestWASMFunctionConfig_EffectiveTransport(t *testing.T) {
	cfg := &WASMFunctionConfig{}
	assert.Equal(t, TransportTypePayload, cfg.EffectiveTransport())

	cfg.Transport = TransportTypeWASIHTTP
	assert.Equal(t, TransportTypeWASIHTTP, cfg.EffectiveTransport())
}
