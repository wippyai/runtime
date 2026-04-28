// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"errors"
	"testing"

	"github.com/wippyai/runtime/api/cluster"
	"go.uber.org/zap"
)

func TestQueueIsBounded_RaftControl_DropsOldest(t *testing.T) {
	cfg := DefaultManagerConfig()
	cfg.Logger = zap.NewNop()
	cfg.RaftControlQueueCap = 8
	nsm := NewNodeStateManager(cfg, newTelemetry(nil), zap.NewNop())
	const node cluster.NodeID = "peer"
	nsm.CreateNodeState(node)

	for i := 0; i < 100; i++ {
		if err := nsm.QueueMessageClass(node, []byte{byte(i)}, ClassRaftControl); err != nil {
			t.Fatalf("RaftControl must never reject (drop-oldest): got %v at i=%d", err, i)
		}
	}
	got := nsm.DrainMessages(node, 100)
	if len(got) != 8 {
		t.Fatalf("expected exactly cap (8) drained, got %d", len(got))
	}
	// Newest 8 entries are 92..99
	for idx, want := byte(92), 0; want < 8; idx, want = idx+1, want+1 {
		if got[want][0] != idx {
			t.Fatalf("want byte %d at idx %d, got %d", idx, want, got[want][0])
		}
	}
}

func TestQueueIsBounded_PGBroadcast_RejectsNewest(t *testing.T) {
	cfg := DefaultManagerConfig()
	cfg.Logger = zap.NewNop()
	cfg.PGBroadcastQueueCap = 4
	nsm := NewNodeStateManager(cfg, newTelemetry(nil), zap.NewNop())
	const node cluster.NodeID = "peer"
	nsm.CreateNodeState(node)

	for i := 0; i < 4; i++ {
		if err := nsm.QueueMessageClass(node, []byte{byte(i)}, ClassPGBroadcast); err != nil {
			t.Fatalf("first 4 PGBroadcast must accept: got %v at i=%d", err, i)
		}
	}
	for i := 4; i < 100; i++ {
		err := nsm.QueueMessageClass(node, []byte{byte(i)}, ClassPGBroadcast)
		if !errors.Is(err, ErrQueueFull) {
			t.Fatalf("expected ErrQueueFull at i=%d, got %v", i, err)
		}
	}
	got := nsm.DrainMessages(node, 100)
	if len(got) != 4 {
		t.Fatalf("expected exactly 4 drained, got %d", len(got))
	}
	// Oldest 4 entries are 0..3 (drop-newest preserves arrival order)
	for i, b := range got {
		if b[0] != byte(i) {
			t.Fatalf("want byte %d at idx %d, got %d", i, i, b[0])
		}
	}
}

func TestDrainPriority_ControlBeforeBroadcast(t *testing.T) {
	cfg := DefaultManagerConfig()
	cfg.Logger = zap.NewNop()
	nsm := NewNodeStateManager(cfg, newTelemetry(nil), zap.NewNop())
	const node cluster.NodeID = "peer"
	nsm.CreateNodeState(node)

	_ = nsm.QueueMessageClass(node, []byte("bcast"), ClassPGBroadcast)
	_ = nsm.QueueMessageClass(node, []byte("ctrl"), ClassRaftControl)

	got := nsm.DrainMessages(node, 2)
	if len(got) != 2 {
		t.Fatalf("expected 2 drained, got %d", len(got))
	}
	if string(got[0]) != "ctrl" {
		t.Fatalf("expected ctrl first, got %q", string(got[0]))
	}
	if string(got[1]) != "bcast" {
		t.Fatalf("expected bcast second, got %q", string(got[1]))
	}
}
