// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"errors"
	"testing"

	"github.com/wippyai/runtime/api/cluster"
	"go.uber.org/zap"
)

func TestReliableQueue_RaftControl_GrowsPastConfiguredCap(t *testing.T) {
	cfg := DefaultManagerConfig()
	cfg.Logger = zap.NewNop()
	nsm := NewNodeStateManager(cfg, newTelemetry(nil), zap.NewNop())
	const node cluster.NodeID = "peer"
	nsm.CreateNodeState(node)

	for i := 0; i < 100; i++ {
		if err := nsm.QueueMessageClass(node, []byte{byte(i)}, ClassRaftControl); err != nil {
			t.Fatalf("RaftControl must never reject: got %v at i=%d", err, i)
		}
	}
	got := nsm.DrainMessages(node, 100)
	if len(got) != 100 {
		t.Fatalf("expected all 100 drained, got %d", len(got))
	}
	for i := 0; i < 100; i++ {
		if got[i].Data[0] != byte(i) {
			t.Fatalf("want byte %d at idx %d, got %d", i, i, got[i].Data[0])
		}
		if got[i].Class != ClassRaftControl {
			t.Fatalf("want ClassRaftControl, got %s", got[i].Class)
		}
	}
}

func TestReliableQueue_PGBroadcast_GrowsPastConfiguredCap(t *testing.T) {
	cfg := DefaultManagerConfig()
	cfg.Logger = zap.NewNop()
	nsm := NewNodeStateManager(cfg, newTelemetry(nil), zap.NewNop())
	const node cluster.NodeID = "peer"
	nsm.CreateNodeState(node)

	for i := 0; i < 100; i++ {
		if err := nsm.QueueMessageClass(node, []byte{byte(i)}, ClassPGBroadcast); err != nil {
			t.Fatalf("PGBroadcast must accept while managed: got %v at i=%d", err, i)
		}
	}
	got := nsm.DrainMessages(node, 100)
	if len(got) != 100 {
		t.Fatalf("expected all 100 drained, got %d", len(got))
	}
	for i, b := range got {
		if b.Data[0] != byte(i) {
			t.Fatalf("want byte %d at idx %d, got %d", i, i, b.Data[0])
		}
		if b.Class != ClassPGBroadcast {
			t.Fatalf("want ClassPGBroadcast, got %s", b.Class)
		}
	}
}

func TestQueueIsBounded_Gossip_RejectsNewest(t *testing.T) {
	cfg := DefaultManagerConfig()
	cfg.Logger = zap.NewNop()
	cfg.GossipQueueCap = 4
	nsm := NewNodeStateManager(cfg, newTelemetry(nil), zap.NewNop())
	const node cluster.NodeID = "peer"
	nsm.CreateNodeState(node)

	for i := 0; i < 4; i++ {
		if err := nsm.QueueMessageClass(node, []byte{byte(i)}, ClassGossip); err != nil {
			t.Fatalf("first 4 Gossip must accept: got %v at i=%d", err, i)
		}
	}
	for i := 4; i < 100; i++ {
		err := nsm.QueueMessageClass(node, []byte{byte(i)}, ClassGossip)
		if !errors.Is(err, ErrQueueFull) {
			t.Fatalf("expected ErrQueueFull at i=%d, got %v", i, err)
		}
	}
	got := nsm.DrainMessages(node, 100)
	if len(got) != 4 {
		t.Fatalf("expected exactly 4 drained, got %d", len(got))
	}
	for i, b := range got {
		if b.Data[0] != byte(i) {
			t.Fatalf("want byte %d at idx %d, got %d", i, i, b.Data[0])
		}
		if b.Class != ClassGossip {
			t.Fatalf("want ClassGossip, got %s", b.Class)
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
	if string(got[0].Data) != "ctrl" || got[0].Class != ClassRaftControl {
		t.Fatalf("expected ctrl/raft first, got %q/%s", string(got[0].Data), got[0].Class)
	}
	if string(got[1].Data) != "bcast" || got[1].Class != ClassPGBroadcast {
		t.Fatalf("expected bcast/pg second, got %q/%s", string(got[1].Data), got[1].Class)
	}
}

func TestGossipRequeueRespectsCap(t *testing.T) {
	cfg := DefaultManagerConfig()
	cfg.Logger = zap.NewNop()
	cfg.GossipQueueCap = 4
	nsm := NewNodeStateManager(cfg, newTelemetry(nil), zap.NewNop())
	const node cluster.NodeID = "peer"
	nsm.CreateNodeState(node)

	// Fill the queue.
	for i := 0; i < 4; i++ {
		_ = nsm.QueueMessageClass(node, []byte{byte(i)}, ClassGossip)
	}
	// Try to requeue 100 stale messages from a stuck connection — must not
	// grow past the cap (current bug duplicates them).
	stale := make([][]byte, 100)
	for i := range stale {
		stale[i] = []byte{byte(200 + i)}
	}
	nsm.RequeueMessagesClass(node, stale, ClassGossip)

	got := nsm.DrainMessages(node, 1000)
	if len(got) > 4 {
		t.Fatalf("queue exceeded cap after requeue: got %d, want <=4", len(got))
	}
}
