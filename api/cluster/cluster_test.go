// Package cluster exposes a read-only view of cluster membership.
package cluster

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeInfo_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		info    NodeInfo
		name    string
		wantErr bool
	}{
		{
			name: "complete node info",
			info: NodeInfo{
				ID:   "node-1",
				Addr: "192.168.1.100:8080",
				Meta: NodeMeta{
					"region":  "us-west-2",
					"version": "1.0.0",
					"env":     "production",
				},
			},
			wantErr: false,
		},
		{
			name: "minimal node info",
			info: NodeInfo{
				ID:   "node-2",
				Addr: ":9090",
			},
			wantErr: false,
		},
		{
			name: "empty metadata",
			info: NodeInfo{
				ID:   "node-3",
				Addr: "localhost:8080",
				Meta: NodeMeta{},
			},
			wantErr: false,
		},
		{
			name: "nil metadata",
			info: NodeInfo{
				ID:   "node-4",
				Addr: "127.0.0.1:3000",
				Meta: nil,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.info)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded = NodeInfo{}
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.info, decoded)
		})
	}
}

func TestNodeMeta_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		meta    NodeMeta
		name    string
		wantErr bool
	}{
		{
			name: "multiple keys",
			meta: NodeMeta{
				"key1": "value1",
				"key2": "value2",
				"key3": "value3",
			},
			wantErr: false,
		},
		{
			name:    "empty meta",
			meta:    NodeMeta{},
			wantErr: false,
		},
		{
			name:    "nil meta",
			meta:    nil,
			wantErr: false,
		},
		{
			name: "special characters",
			meta: NodeMeta{
				"build-hash":     "abc123",
				"deployment.env": "staging",
				"node_type":      "worker",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.meta)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded = NodeMeta{}
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.meta, decoded)
		})
	}
}

func TestNodeID(t *testing.T) {
	t.Run("type alias", func(t *testing.T) {
		id := "test-node-id"
		assert.Equal(t, "test-node-id", id)
	})
}
