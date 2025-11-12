package lua

import (
	"testing"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
