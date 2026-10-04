// SPDX-License-Identifier: MPL-2.0

package eventual_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/system/eventbus"
	"github.com/wippyai/runtime/system/topology/namereg/eventual"
)

// liveSender delivers frames only to nodes the local membership currently
// lists as live, as membership's reliable user messages do.
type liveSender struct {
	live  map[string]bool
	peers map[string]*eventual.Service
	mu    sync.Mutex
}

func (s *liveSender) Send(target string, payload []byte) error {
	s.mu.Lock()
	live, peer := s.live[target], s.peers[target]
	s.mu.Unlock()
	if !live || peer == nil {
		return errors.New("target node not in member list")
	}
	peer.OnFrame(append([]byte(nil), payload...))
	return nil
}

func (s *liveSender) join(node string) {
	s.mu.Lock()
	s.live[node] = true
	s.mu.Unlock()
}

func nodeJoinedEvent(node string) event.Event {
	return event.Event{
		System: cluster.System,
		Kind:   cluster.NodeJoined,
		Data:   cluster.NodeEvent{Node: cluster.NodeInfo{ID: node}},
	}
}

func allShards() []uint16 {
	ids := make([]uint16, eventual.ShardCount)
	for i := range ids {
		ids[i] = uint16(i)
	}
	return ids
}

// TestRejoin_ShardResponseReachesARequesterThatIsNotLiveYet covers a node
// rejoining under a name the cluster saw before: its shard request reaches a
// peer that still lists the previous incarnation as departed, so the reply
// cannot be sent yet. The reply must reach the requester once membership
// reports it joined, not wait for the next anti-entropy round.
func TestRejoin_ShardResponseReachesARequesterThatIsNotLiveYet(t *testing.T) {
	busA := eventbus.NewBus()
	toB := &liveSender{live: map[string]bool{}, peers: map[string]*eventual.Service{}}
	a := eventual.NewService(eventual.Config{LocalNodeID: "node-A", Bus: busA, Sender: toB})
	b := eventual.NewService(eventual.Config{LocalNodeID: "node-B"})
	toB.peers["node-B"] = b
	require.NoError(t, a.Start(context.Background()))
	require.NoError(t, b.Start(context.Background()))
	t.Cleanup(func() { _ = a.Stop(); _ = b.Stop() })

	supervisor := pid.PID{Node: "node-A", Host: "workers", UniqID: "sup"}
	_, err := a.Register("bee.hive.supervisor/node-A", supervisor)
	require.NoError(t, err)

	request, err := eventual.EncodeShardRequestFrame("node-B", allShards())
	require.NoError(t, err)
	a.OnFrame(request)

	found, err := b.Lookup(context.Background(), "bee.hive.supervisor/node-A")
	require.NoError(t, err)
	require.False(t, found.Found, "the reply cannot reach node-B before it is live")

	toB.join("node-B")
	busA.Send(context.Background(), nodeJoinedEvent("node-B"))

	require.Eventually(t, func() bool {
		r, err := b.Lookup(context.Background(), "bee.hive.supervisor/node-A")
		return err == nil && r.Found && r.PID == supervisor
	}, 2*time.Second, 5*time.Millisecond, "node-B must receive the held shard response when it joins")
}

// TestRejoin_HeldResponseIsDroppedWhenTheRequesterLeaves keeps held replies
// bounded: a requester that leaves without joining gets nothing later.
func TestRejoin_HeldResponseIsDroppedWhenTheRequesterLeaves(t *testing.T) {
	busA := eventbus.NewBus()
	toB := &liveSender{live: map[string]bool{}, peers: map[string]*eventual.Service{}}
	a := eventual.NewService(eventual.Config{LocalNodeID: "node-A", Bus: busA, Sender: toB})
	b := eventual.NewService(eventual.Config{LocalNodeID: "node-B"})
	toB.peers["node-B"] = b
	require.NoError(t, a.Start(context.Background()))
	require.NoError(t, b.Start(context.Background()))
	t.Cleanup(func() { _ = a.Stop(); _ = b.Stop() })

	_, err := a.Register("bee.hive.supervisor/node-A", pid.PID{Node: "node-A", Host: "workers", UniqID: "sup"})
	require.NoError(t, err)
	request, err := eventual.EncodeShardRequestFrame("node-B", allShards())
	require.NoError(t, err)
	a.OnFrame(request)

	busA.Send(context.Background(), nodeLeftEvent("node-B"))
	toB.join("node-B")
	busA.Send(context.Background(), nodeJoinedEvent("node-B"))

	require.Never(t, func() bool {
		r, err := b.Lookup(context.Background(), "bee.hive.supervisor/node-A")
		return err == nil && r.Found
	}, 300*time.Millisecond, 10*time.Millisecond, "a reply held for a requester that left must not be delivered to a later join")
}
