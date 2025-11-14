// Package supervisor provides service lifecycle management and supervision.
package supervisor

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/topology"
)

func TestKindConstant(t *testing.T) {
	assert.Equal(t, "process.service", KindProcessService)
}

func TestServiceConfig_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		config  ServiceConfig
		wantErr bool
	}{
		{
			name: "complete config",
			config: ServiceConfig{
				Process: registry.ID{NS: "proc", Name: "worker"},
				HostID:  "node:worker1",
				Input:   []any{"arg1", "arg2"},
			},
			wantErr: false,
		},
		{
			name: "minimal config",
			config: ServiceConfig{
				Process: registry.ID{NS: "p", Name: "test"},
				HostID:  "node:host",
			},
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

			var decoded ServiceConfig
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.config.Process, decoded.Process)
			assert.Equal(t, tt.config.HostID, decoded.HostID)
		})
	}
}

func TestServiceConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  ServiceConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: ServiceConfig{
				Process: registry.ID{NS: "proc", Name: "worker"},
				HostID:  "node:worker1",
			},
			wantErr: false,
		},
		{
			name: "missing process name",
			config: ServiceConfig{
				Process: registry.ID{NS: "proc"},
				HostID:  "node:worker1",
			},
			wantErr: true,
			errMsg:  "process Process is required",
		},
		{
			name: "missing host ID",
			config: ServiceConfig{
				Process: registry.ID{NS: "proc", Name: "worker"},
			},
			wantErr: true,
			errMsg:  "host Process is required",
		},
		{
			name: "control host not allowed",
			config: ServiceConfig{
				Process: registry.ID{NS: "proc", Name: "worker"},
				HostID:  topology.ControlHost,
			},
			wantErr: true,
			errMsg:  "cannot be node:control",
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
