// SPDX-License-Identifier: MPL-2.0

package eventualreg_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/system/eventbus"
	"github.com/wippyai/runtime/system/eventualreg"
)

// nodeLeftEvent builds the cluster.NodeLeft event the membership service
// publishes when a node departs.
func nodeLeftEvent(node string) event.Event {
	return event.Event{
		System: cluster.System,
		Kind:   cluster.NodeLeft,
		Data:   cluster.NodeEvent{Node: cluster.NodeInfo{ID: node}},
	}
}

// entryDeleted returns the live/deleted state of `name` straight from the
// underlying ORSWOT shard, so tests can assert a name is tombstoned (a delete
// entry exists) rather than merely absent.
func entryDeleted(t *testing.T, st *eventualreg.State, name string) (deleted, present bool) {
	t.Helper()
	for i := 0; i < eventualreg.ShardCount; i++ {
		for _, e := range st.ShardEntries(i) {
			if e.Name == name {
				return e.Deleted, true
			}
		}
	}
	return false, false
}

// remoteDeltaFrame builds a FrameTypeDelta wrapping a single register delta for
// `name` minted by origin `origin` with PID on that same origin — the realistic
// shape of a departed node's own binding as it lands on a surviving replica.
func remoteDeltaFrame(t *testing.T, name string, p pid.PID, origin string, counter uint64) []byte {
	t.Helper()
	e := &eventualreg.Entry{
		Name:    name,
		PID:     p,
		Counter: counter,
		Wall:    1000,
	}
	body, err := eventualreg.EncodeDelta(nil, e, origin)
	require.NoError(t, err)
	return append([]byte{byte(eventualreg.FrameTypeDelta)}, body...)
}

// TestNodeLeft_ReapsDepartedNodeEntries proves that when a node leaves, every
// live name resolving to a PID on that node is tombstoned (delete-wins entry)
// so Lookup returns not-found and the delete propagates via gossip.
//
// The binding enters node-A's state the production way: a REMOTE-origin delta
// (origin = node-B, PID.Node = node-B) arrives over gossip, so on node-A it
// lives at rec.dots[node-B], NOT rec.dots[node-A]. The old Unregister-based reap
// only tombstoned rec.dots[localNode] and was therefore a no-op for a departed
// peer's own bindings — this test fails against it (the name stays live forever).
func TestNodeLeft_ReapsDepartedNodeEntries(t *testing.T) {
	bus := eventbus.NewBus()
	svc := eventualreg.NewService(eventualreg.Config{
		LocalNodeID: "node-A",
		Bus:         bus,
	})
	require.NoError(t, svc.Start(context.Background()))
	defer svc.Stop()

	// node-B's own binding arrives over gossip: origin=node-B, PID on node-B.
	pOnB := pid.PID{Node: "node-B", Host: "worker", UniqID: "b1"}
	svc.OnFrame(remoteDeltaFrame(t, "session.on-b", pOnB, "node-B", 1))

	res, err := svc.Lookup(context.Background(), "session.on-b")
	require.NoError(t, err)
	require.True(t, res.Found)
	require.Equal(t, pOnB, res.PID)

	bus.Send(context.Background(), nodeLeftEvent("node-B"))

	require.Eventually(t, func() bool {
		r, _ := svc.Lookup(context.Background(), "session.on-b")
		return !r.Found
	}, 2*time.Second, 5*time.Millisecond, "departed node's foreign-origin binding must be reaped after NodeLeft")

	// The reap must produce a delete entry (tombstone), not just remove it,
	// so it propagates over gossip.
	deleted, present := entryDeleted(t, svc.State(), "session.on-b")
	require.True(t, present, "tombstone entry must remain for propagation")
	assert.True(t, deleted, "reaped entry must be a delete/tombstone")
}

// TestNodeLeft_ReapPropagatesWithDepartedOrigin proves the reap tombstone
// gossips with the DEPARTED node's origin, not the reaping node's. Two replicas
// A and C both hold node-B's remote-origin live dot. A reaps on NodeLeft(node-B)
// and broadcasts; replaying A's drained frames into C must tombstone the SAME
// dot (rec.dots[node-B]) so C converges to not-found. If the broadcast relabeled
// the tombstone with A's origin, it would land at rec.dots[node-A] on C, miss
// delete-wins against B's live dot, and C would keep serving the dead PID.
func TestNodeLeft_ReapPropagatesWithDepartedOrigin(t *testing.T) {
	busA := eventbus.NewBus()
	a := eventualreg.NewService(eventualreg.Config{LocalNodeID: "node-A", Bus: busA})
	c := eventualreg.NewService(eventualreg.Config{LocalNodeID: "node-C"})
	require.NoError(t, a.Start(context.Background()))
	require.NoError(t, c.Start(context.Background()))
	defer a.Stop()
	defer c.Stop()

	pOnB := pid.PID{Node: "node-B", Host: "worker", UniqID: "b1"}
	a.OnFrame(remoteDeltaFrame(t, "session.on-b", pOnB, "node-B", 1))
	c.OnFrame(remoteDeltaFrame(t, "session.on-b", pOnB, "node-B", 1))

	rc, err := c.Lookup(context.Background(), "session.on-b")
	require.NoError(t, err)
	require.True(t, rc.Found)

	busA.Send(context.Background(), nodeLeftEvent("node-B"))
	require.Eventually(t, func() bool {
		r, _ := a.Lookup(context.Background(), "session.on-b")
		return !r.Found
	}, 2*time.Second, 5*time.Millisecond)

	// Replay A's broadcast frames into C.
	frames := a.DrainBroadcasts(0, 0)
	require.NotEmpty(t, frames, "reap must enqueue a tombstone broadcast")
	for _, f := range frames {
		c.OnFrame(f)
	}

	r, err := c.Lookup(context.Background(), "session.on-b")
	require.NoError(t, err)
	assert.False(t, r.Found, "C must converge to not-found after the reap tombstone propagates")

	deleted, present := entryDeleted(t, c.State(), "session.on-b")
	require.True(t, present)
	assert.True(t, deleted, "C must hold the dot as a tombstone, not a separate live entry")
}

// TestNodeLeft_DoesNotReapOtherNodes proves the reap is scoped: entries whose
// PID lives on a surviving node are untouched.
func TestNodeLeft_DoesNotReapOtherNodes(t *testing.T) {
	bus := eventbus.NewBus()
	svc := eventualreg.NewService(eventualreg.Config{
		LocalNodeID: "node-A",
		Bus:         bus,
	})
	require.NoError(t, svc.Start(context.Background()))
	defer svc.Stop()

	pOnB := pid.PID{Node: "node-B", Host: "worker", UniqID: "b1"}
	pOnC := pid.PID{Node: "node-C", Host: "worker", UniqID: "c1"}
	svc.OnFrame(remoteDeltaFrame(t, "session.on-b", pOnB, "node-B", 1))
	svc.OnFrame(remoteDeltaFrame(t, "session.on-c", pOnC, "node-C", 1))

	bus.Send(context.Background(), nodeLeftEvent("node-B"))

	require.Eventually(t, func() bool {
		r, _ := svc.Lookup(context.Background(), "session.on-b")
		return !r.Found
	}, 2*time.Second, 5*time.Millisecond)

	// node-C's binding survives — its PID.Node != departed, so ReapNode skips it.
	res, err := svc.Lookup(context.Background(), "session.on-c")
	require.NoError(t, err)
	assert.True(t, res.Found)
	assert.Equal(t, pOnC, res.PID)
}

// nopSender satisfies MessageSender without doing any I/O, so a test can prime
// the per-peer shard-request cooldown via RequestShards.
type nopSender struct{}

func (nopSender) Send(string, []byte) error { return nil }

// TestNodeLeft_DropsShardRequestCooldown proves the departed peer's shard-pull
// cooldown entry is removed on NodeLeft, so lastShardRequest does not accumulate
// dead peers under churn. Observable via RequestShards: a primed cooldown
// suppresses an immediate retry; after NodeLeft the entry is gone so the next
// request is no longer suppressed.
func TestNodeLeft_DropsShardRequestCooldown(t *testing.T) {
	bus := eventbus.NewBus()
	svc := eventualreg.NewService(eventualreg.Config{
		LocalNodeID: "node-A",
		Bus:         bus,
		Sender:      nopSender{},
	})
	require.NoError(t, svc.Start(context.Background()))
	defer svc.Stop()

	// Prime the cooldown for node-B.
	require.True(t, svc.RequestShards("node-B", []uint16{0}))
	// Immediate retry is suppressed by the cooldown entry.
	require.False(t, svc.RequestShards("node-B", []uint16{0}), "cooldown must suppress immediate retry")

	bus.Send(context.Background(), nodeLeftEvent("node-B"))

	require.Eventually(t, func() bool {
		// Once the cooldown entry is dropped the next request is allowed again.
		return svc.RequestShards("node-B", []uint16{0})
	}, 2*time.Second, 5*time.Millisecond, "departed peer's cooldown entry must be removed on NodeLeft")
}

// TestNodeLeft_ForgetsPeerForGC proves the NodeLeft handler invokes
// OnPeerLeft so the tombstone tracker stops pinning safe-counter GC on the
// departed node. Before the fix OnPeerLeft has no caller, so the tracker keeps
// the departed peer's (low) acks and pins safe counters at that level.
func TestNodeLeft_ForgetsPeerForGC(t *testing.T) {
	bus := eventbus.NewBus()
	svc := eventualreg.NewService(eventualreg.Config{
		LocalNodeID: "node-A",
		Bus:         bus,
	})
	require.NoError(t, svc.Start(context.Background()))
	defer svc.Stop()

	// node-B has acked nothing (cv all-zero) — it pins the safe counter for
	// any origin to 0 while it is considered a contributor.
	svc.OnPeerDigest("node-B", []uint64{0, 0})
	// node-C has acked origin 0 up to counter 5.
	svc.OnPeerDigest("node-C", []uint64{5, 0})

	// Keep node-B in the alive set throughout: this isolates the effect of
	// ForgetPeer (OnPeerLeft) from the alive-set filter. While node-B's ack
	// is still tracked it pins origin-0 to 0; only ForgetPeer removes its
	// pinning contribution.
	alive := map[string]struct{}{"node-B": {}, "node-C": {}, "node-A": {}}
	before := svc.Tracker().SafeCounters(alive, 2)
	require.Equal(t, uint64(0), before[0], "node-B (no acks) pins origin-0 safe counter to 0")

	bus.Send(context.Background(), nodeLeftEvent("node-B"))

	require.Eventually(t, func() bool {
		after := svc.Tracker().SafeCounters(alive, 2)
		return after[0] == 5
	}, 2*time.Second, 5*time.Millisecond, "after OnPeerLeft forgets node-B, only node-C's ack of 5 contributes")
}
