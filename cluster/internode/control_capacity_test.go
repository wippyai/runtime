// SPDX-License-Identifier: MPL-2.0
package internode

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
)

// Application backlog must not
// consume all control capacity, including while a writer owns drained frames.
// This does not prove wire preemption or isolation from Raft snapshot traffic.
func TestBulkCannotConsumeProtectedControlCapacity(t *testing.T) {
	for _, scope := range []string{"peer", "aggregate"} {
		for _, pressure := range []string{"entries", "bytes"} {
			for _, inFlight := range []bool{false, true} {
				state := "queued"
				if inFlight {
					state = "inflight"
				}
				t.Run(scope+"/"+pressure+"/"+state, func(t *testing.T) {
					cfg := DefaultManagerConfig()
					cfg.OutboundQueueSize, cfg.OutboundPeerBytes = 4, 16
					cfg.OutboundTotalEntries, cfg.OutboundTotalBytes = 8, 32
					cfg.OutboundControlPeerEntries, cfg.OutboundControlPeerBytes = 1, 4
					cfg.OutboundControlTotalEntries, cfg.OutboundControlTotalBytes = 2, 8
					states := NewNodeStateManager(cfg, newTelemetry(nil), zap.NewNop())
					peers := []string{"bulk-a"}
					target := "bulk-a"
					if scope == "aggregate" {
						peers = append(peers, "bulk-b")
						target = "control"
						states.CreateNodeState(target)
					}
					defer func() {
						for _, peer := range peers {
							states.RemoveNodeState(peer)
						}
						if scope == "aggregate" {
							states.RemoveNodeState(target)
						}
					}()
					for _, peer := range peers {
						states.CreateNodeState(peer)
						if pressure == "entries" {
							for range 3 {
								require.NoError(t, states.QueueMessageClass(peer, []byte("x"), ClassPGBroadcast))
							}
						} else {
							require.NoError(t, states.QueueMessageClass(peer, make([]byte, 12), ClassPGBroadcast))
						}
						if inFlight {
							frames := states.DrainMessages(peer, 4)
							require.NotEmpty(t, frames)
							defer releaseOutbound(frames)
						}
					}
					require.NoError(t, states.QueueMessageClass(target, []byte("c"), ClassRaftRPC), "bulk backlog must leave bounded control capacity")
				})
			}
		}
	}
}

func TestDefaultControlReserveUnderAggregateSaturation(t *testing.T) {
	cfg := DefaultManagerConfig()
	states := NewNodeStateManager(cfg, newTelemetry(nil), zap.NewNop())
	require.NoError(t, states.budgetErr)
	require.Positive(t, cfg.OutboundControlPeerBytes)
	require.Positive(t, cfg.OutboundControlTotalBytes)
	ordinaryPerPeer := uint64(cfg.OutboundQueueSize) - cfg.OutboundControlPeerEntries
	ordinaryTotal := cfg.OutboundTotalEntries - cfg.OutboundControlTotalEntries
	require.Zero(t, ordinaryTotal%ordinaryPerPeer)
	peers := int(ordinaryTotal / ordinaryPerPeer)
	for i := 0; i < peers; i++ {
		peer := fmt.Sprintf("peer-%d", i)
		states.CreateNodeState(peer)
		defer states.RemoveNodeState(peer)
		for j := uint64(0); j < ordinaryPerPeer; j++ {
			require.NoError(t, states.QueueMessageClass(peer, []byte("d"), ClassPGBroadcast))
		}
	}
	states.CreateNodeState("empty")
	defer states.RemoveNodeState("empty")
	require.ErrorIs(t, states.QueueMessageClass("empty", []byte("d"), ClassPGBroadcast), ErrQueueFull)
	for i := 0; i < peers; i++ {
		peer := fmt.Sprintf("peer-%d", i)
		for j := uint64(0); j < cfg.OutboundControlPeerEntries; j++ {
			class := ClassRaftControl
			if j%2 != 0 {
				class = ClassRaftRPC
			}
			require.NoError(t, states.QueueMessageClass(peer, []byte("c"), class))
		}
	}
	require.Equal(t, cfg.OutboundTotalEntries, states.reliable.entries)
	require.ErrorIs(t, states.QueueMessageClass("empty", []byte("c"), ClassRaftRPC), ErrQueueFull)
}
