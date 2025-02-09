package lua

import (
	"testing"

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
				Source:    "function test() return 'hello' end",
				Method:    "test",
				Libraries: []string{"lib1", "lib2"},
				Modules:   []string{"mod1", "mod2"},
				Pool: PoolConfig{
					Size:    5,
					Workers: 2,
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
			name: "invalid pool size",
			config: FunctionConfig{
				Source: "function test() return 'hello' end",
				Method: "test",
				Pool: PoolConfig{
					Size: 0,
				},
			},
			wantErr: true,
			errMsg:  "pool.num_vms must be greater than 0",
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
