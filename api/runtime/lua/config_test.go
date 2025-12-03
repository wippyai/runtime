// Package lua provides Lua runtime integration.
package lua

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
)

func TestFunctionConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  FunctionConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: FunctionConfig{
				Source:  "function test() return 'hello' end",
				Method:  "test",
				Modules: []string{"mod1", "mod2"},
				Pool: PoolConfig{
					Size:    5,
					Workers: 2,
				},
			},
			wantErr: false,
		},
		{
			name: "valid flex pool",
			config: FunctionConfig{
				Source: "function test() return 'hello' end",
				Method: "test",
				Pool: PoolConfig{
					Size:    0,
					Workers: 0,
					MaxSize: 50,
				},
			},
			wantErr: false,
		},
		{
			name: "valid flex pool with default MaxSize",
			config: FunctionConfig{
				Source: "function test() return 'hello' end",
				Method: "test",
				Pool: PoolConfig{
					Size:    0,
					Workers: 0,
					MaxSize: 0,
				},
			},
			wantErr: false,
		},
		{
			name: "empty source",
			config: FunctionConfig{
				Method: "test",
				Pool: PoolConfig{
					Size: 5,
				},
			},
			wantErr: true,
			errMsg:  "source is required",
		},
		{
			name: "empty method",
			config: FunctionConfig{
				Source: "function test() return 'hello' end",
				Pool: PoolConfig{
					Size: 5,
				},
			},
			wantErr: true,
			errMsg:  "method is required",
		},
		{
			name: "invalid non-flex pool size",
			config: FunctionConfig{
				Source: "function test() return 'hello' end",
				Method: "test",
				Pool: PoolConfig{
					Size:    0,
					Workers: 1,
				},
			},
			wantErr: true,
			errMsg:  "pool.size must be greater than 0 for non-flex pools",
		},
		{
			name: "workers without size",
			config: FunctionConfig{
				Source: "function test() return 'hello' end",
				Method: "test",
				Pool: PoolConfig{
					Size:    0,
					Workers: 2,
				},
			},
			wantErr: true,
			errMsg:  "pool.size must be greater than 0 for non-flex pools",
		},
		{
			name: "empty import alias",
			config: FunctionConfig{
				Source: "function test() return 'hello' end",
				Method: "test",
				Imports: map[string]registry.ID{
					"": {Name: "lib1"},
				},
				Pool: PoolConfig{Size: 1},
			},
			wantErr: true,
			errMsg:  "import alias cannot be empty",
		},
		{
			name: "empty import name",
			config: FunctionConfig{
				Source: "function test() return 'hello' end",
				Method: "test",
				Imports: map[string]registry.ID{
					"lib": {Name: ""},
				},
				Pool: PoolConfig{Size: 1},
			},
			wantErr: true,
			errMsg:  "import :name cannot be empty",
		},
		{
			name: "empty module",
			config: FunctionConfig{
				Source:  "function test() return 'hello' end",
				Method:  "test",
				Modules: []string{""},
				Pool:    PoolConfig{Size: 1},
			},
			wantErr: true,
			errMsg:  "module cannot be empty",
		},
		{
			name: "module with namespace",
			config: FunctionConfig{
				Source:  "function test() return 'hello' end",
				Method:  "test",
				Modules: []string{"ns:mod"},
				Pool:    PoolConfig{Size: 1},
			},
			wantErr: true,
			errMsg:  "module cannot have a namespace",
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

func TestLibraryConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  LibraryConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: LibraryConfig{
				Source: "local M = {} return M",
				Imports: map[string]registry.ID{
					"lib1": {Name: "library1"},
				},
				Modules: []string{"mod1", "mod2"},
			},
			wantErr: false,
		},
		{
			name: "valid config without imports and modules",
			config: LibraryConfig{
				Source: "local M = {} return M",
			},
			wantErr: false,
		},
		{
			name: "empty source",
			config: LibraryConfig{
				Source: "",
			},
			wantErr: true,
			errMsg:  "source is required",
		},
		{
			name: "empty import alias",
			config: LibraryConfig{
				Source: "local M = {} return M",
				Imports: map[string]registry.ID{
					"": {Name: "lib1"},
				},
			},
			wantErr: true,
			errMsg:  "import alias cannot be empty",
		},
		{
			name: "empty import name",
			config: LibraryConfig{
				Source: "local M = {} return M",
				Imports: map[string]registry.ID{
					"lib": {Name: ""},
				},
			},
			wantErr: true,
			errMsg:  "import :name cannot be empty",
		},
		{
			name: "empty module",
			config: LibraryConfig{
				Source:  "local M = {} return M",
				Modules: []string{""},
			},
			wantErr: true,
			errMsg:  "module cannot be empty",
		},
		{
			name: "module with namespace",
			config: LibraryConfig{
				Source:  "local M = {} return M",
				Modules: []string{"ns:mod"},
			},
			wantErr: true,
			errMsg:  "module cannot have a namespace",
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

func TestBytecodeFunctionConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  BytecodeFunctionConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: BytecodeFunctionConfig{
				FS:     "app:bytecode",
				Path:   "/functions/handler.luac",
				Hash:   "sha256:abc123",
				Method: "handle",
				Pool:   PoolConfig{Size: 4},
			},
			wantErr: false,
		},
		{
			name: "valid flex pool",
			config: BytecodeFunctionConfig{
				FS:     "app:bytecode",
				Path:   "/functions/handler.luac",
				Hash:   "sha256:abc123",
				Method: "handle",
				Pool:   PoolConfig{MaxSize: 16},
			},
			wantErr: false,
		},
		{
			name: "missing fs",
			config: BytecodeFunctionConfig{
				Path:   "/functions/handler.luac",
				Hash:   "sha256:abc123",
				Method: "handle",
				Pool:   PoolConfig{Size: 4},
			},
			wantErr: true,
			errMsg:  "fs is required",
		},
		{
			name: "missing path",
			config: BytecodeFunctionConfig{
				FS:     "app:bytecode",
				Hash:   "sha256:abc123",
				Method: "handle",
				Pool:   PoolConfig{Size: 4},
			},
			wantErr: true,
			errMsg:  "path is required",
		},
		{
			name: "missing hash",
			config: BytecodeFunctionConfig{
				FS:     "app:bytecode",
				Path:   "/functions/handler.luac",
				Method: "handle",
				Pool:   PoolConfig{Size: 4},
			},
			wantErr: true,
			errMsg:  "hash is required",
		},
		{
			name: "missing method",
			config: BytecodeFunctionConfig{
				FS:   "app:bytecode",
				Path: "/functions/handler.luac",
				Hash: "sha256:abc123",
				Pool: PoolConfig{Size: 4},
			},
			wantErr: true,
			errMsg:  "method is required",
		},
		{
			name: "empty import alias",
			config: BytecodeFunctionConfig{
				FS:     "app:bytecode",
				Path:   "/functions/handler.luac",
				Hash:   "sha256:abc123",
				Method: "handle",
				Pool:   PoolConfig{Size: 4},
				Imports: map[string]registry.ID{
					"": {Name: "lib"},
				},
			},
			wantErr: true,
			errMsg:  "import alias cannot be empty",
		},
		{
			name: "empty module",
			config: BytecodeFunctionConfig{
				FS:      "app:bytecode",
				Path:    "/functions/handler.luac",
				Hash:    "sha256:abc123",
				Method:  "handle",
				Pool:    PoolConfig{Size: 4},
				Modules: []string{""},
			},
			wantErr: true,
			errMsg:  "module cannot be empty",
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

func TestBytecodeLibraryConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  BytecodeLibraryConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: BytecodeLibraryConfig{
				FS:   "app:bytecode",
				Path: "/libs/utils.luac",
				Hash: "sha256:def456",
			},
			wantErr: false,
		},
		{
			name: "valid config with imports",
			config: BytecodeLibraryConfig{
				FS:   "app:bytecode",
				Path: "/libs/utils.luac",
				Hash: "sha256:def456",
				Imports: map[string]registry.ID{
					"helper": {Name: "helper-lib"},
				},
			},
			wantErr: false,
		},
		{
			name: "missing fs",
			config: BytecodeLibraryConfig{
				Path: "/libs/utils.luac",
				Hash: "sha256:def456",
			},
			wantErr: true,
			errMsg:  "fs is required",
		},
		{
			name: "missing path",
			config: BytecodeLibraryConfig{
				FS:   "app:bytecode",
				Hash: "sha256:def456",
			},
			wantErr: true,
			errMsg:  "path is required",
		},
		{
			name: "missing hash",
			config: BytecodeLibraryConfig{
				FS:   "app:bytecode",
				Path: "/libs/utils.luac",
			},
			wantErr: true,
			errMsg:  "hash is required",
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

func TestBytecodeProcessConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  BytecodeProcessConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: BytecodeProcessConfig{
				FS:     "app:bytecode",
				Path:   "/processes/worker.luac",
				Hash:   "sha256:ghi789",
				Method: "run",
			},
			wantErr: false,
		},
		{
			name: "missing fs",
			config: BytecodeProcessConfig{
				Path:   "/processes/worker.luac",
				Hash:   "sha256:ghi789",
				Method: "run",
			},
			wantErr: true,
			errMsg:  "fs is required",
		},
		{
			name: "missing method",
			config: BytecodeProcessConfig{
				FS:   "app:bytecode",
				Path: "/processes/worker.luac",
				Hash: "sha256:ghi789",
			},
			wantErr: true,
			errMsg:  "method is required",
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

func TestBytecodeWorkflowConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  BytecodeWorkflowConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: BytecodeWorkflowConfig{
				FS:     "app:bytecode",
				Path:   "/workflows/order.luac",
				Hash:   "sha256:jkl012",
				Method: "execute",
			},
			wantErr: false,
		},
		{
			name: "missing method",
			config: BytecodeWorkflowConfig{
				FS:   "app:bytecode",
				Path: "/workflows/order.luac",
				Hash: "sha256:jkl012",
			},
			wantErr: true,
			errMsg:  "method is required",
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

func TestBytecodeBteaConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  BytecodeBteaConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: BytecodeBteaConfig{
				FS:     "app:bytecode",
				Path:   "/apps/terminal.luac",
				Hash:   "sha256:mno345",
				Method: "init",
			},
			wantErr: false,
		},
		{
			name: "missing method",
			config: BytecodeBteaConfig{
				FS:   "app:bytecode",
				Path: "/apps/terminal.luac",
				Hash: "sha256:mno345",
			},
			wantErr: true,
			errMsg:  "method is required",
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
