// Package cluster centralizes event-bus metadata.
package cluster

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
)

func TestEventConstants(t *testing.T) {
	tests := []struct {
		name     string
		system   event.System
		kind     event.Kind
		expected string
	}{
		{"system", System, "", "cluster"},
		{"node joined", "", NodeJoinedEventKind, "node.joined"},
		{"node left", "", NodeLeftEventKind, "node.left"},
		{"node updated", "", NodeUpdatedEventKind, "node.updated"},
		{"kv put", "", KVPutEventKind, "kv.put"},
		{"kv delete", "", KVDeleteEventKind, "kv.delete"},
		{"raft leader elected", "", RaftLeaderElectedEventKind, "raft.leader.elected"},
		{"raft leader lost", "", RaftLeaderLostEventKind, "raft.leader.lost"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.system != "" {
				assert.Equal(t, tt.expected, string(tt.system))
			}
			if tt.kind != "" {
				assert.Equal(t, tt.expected, string(tt.kind))
			}
		})
	}
}

func TestNodeEvent_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		event   NodeEvent
		wantErr bool
	}{
		{
			name: "complete node event",
			event: NodeEvent{
				Node: NodeInfo{
					ID:   "node1",
					Addr: "127.0.0.1:8080",
					Meta: NodeMeta{
						"region": "us-west",
						"zone":   "a",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "minimal node event",
			event: NodeEvent{
				Node: NodeInfo{
					ID:   "node2",
					Addr: "192.168.1.1:9090",
				},
			},
			wantErr: false,
		},
		{
			name: "empty metadata",
			event: NodeEvent{
				Node: NodeInfo{
					ID:   "node3",
					Addr: ":8080",
					Meta: nil,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.event)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded NodeEvent
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.event, decoded)
		})
	}
}

func TestChange_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		change  Change
		wantErr bool
	}{
		{
			name: "put operation",
			change: Change{
				Key: "registry.service.foo",
				Rev: 42,
				Val: []byte("service data"),
			},
			wantErr: false,
		},
		{
			name: "delete operation",
			change: Change{
				Key: "registry.service.bar",
				Rev: 0,
				Val: nil,
			},
			wantErr: false,
		},
		{
			name: "empty value",
			change: Change{
				Key: "test.key",
				Rev: 1,
				Val: []byte{},
			},
			wantErr: false,
		},
		{
			name: "binary value",
			change: Change{
				Key: "binary.data",
				Rev: 100,
				Val: []byte{0x00, 0xFF, 0xAB, 0xCD},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.change)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded Change
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.change, decoded)
		})
	}
}
