// SPDX-License-Identifier: MPL-2.0

package eventual

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// syncShard applies every dot src holds for name's shard onto dst, re-interning
// origins by their string ids as the gossip wire does.
func syncShard(src, dst *State, name string) {
	for _, e := range src.ShardEntries(ShardFor(name)) {
		e.Node = dst.internNode(src.NodeString(e.Node))
		dst.Apply(e)
	}
}

func localDot(s *State, name string) *Entry {
	sh := &s.shards[ShardFor(name)]
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	rec, ok := sh.entries[name]
	if !ok {
		return nil
	}
	return rec.dots[s.localNode]
}

// A node restarted without its peers observing NodeLeft registers a name at a
// low counter, then learns its prior incarnation's live dot for the same name
// at a higher counter. The fresh registration stays visible, is re-minted above
// the stale counter, and replaces the stale dot on the peer.
func TestState_RestartedOriginSupersedesPriorIncarnationDot(t *testing.T) {
	const name = "supervisor"
	oldPID := makePID("node-A", "h", "old")
	newPID := makePID("node-A", "h", "new")

	peer := NewState("node-B")
	peer.Apply(&Entry{Name: name, PID: oldPID, Node: peer.internNode("node-A"), Counter: 5, Wall: 1})

	a := NewState("node-A")
	res := a.Register(name, newPID, 2, 0)
	require.True(t, res.Won)
	require.Equal(t, uint64(1), res.Entry.Counter)

	syncShard(peer, a, name)

	got, ok := a.Lookup(name)
	require.True(t, ok)
	require.True(t, got.Equal(newPID), "restarted node must keep its fresh registration, got %v", got)
	dot := localDot(a, name)
	require.NotNil(t, dot)
	require.Greater(t, dot.Counter, uint64(5))

	syncShard(a, peer, name)

	got, ok = peer.Lookup(name)
	require.True(t, ok)
	require.True(t, got.Equal(newPID), "peer must converge to the fresh registration, got %v", got)
	for i := 0; i < ShardCount; i++ {
		require.Equal(t, a.ShardHash(i), peer.ShardHash(i), "shard %d diverged", i)
	}
}

// A prior incarnation's live dot learned before the restarted node registers
// the name is withdrawn above its counter; the name then registers and
// converges on both replicas.
func TestState_RestartedOriginWithdrawsUnclaimedPriorIncarnationDot(t *testing.T) {
	const name = "supervisor"
	oldPID := makePID("node-A", "h", "old")
	newPID := makePID("node-A", "h", "new")

	peer := NewState("node-B")
	peer.Apply(&Entry{Name: name, PID: oldPID, Node: peer.internNode("node-A"), Counter: 5, Wall: 1})

	a := NewState("node-A")
	syncShard(peer, a, name)

	_, ok := a.Lookup(name)
	require.False(t, ok, "a dot this run did not mint must not resolve")
	dot := localDot(a, name)
	require.NotNil(t, dot)
	require.True(t, dot.Deleted)
	require.Greater(t, dot.Counter, uint64(5))

	syncShard(a, peer, name)
	_, ok = peer.Lookup(name)
	require.False(t, ok, "peer must drop the prior incarnation's binding")

	res := a.Register(name, newPID, 2, 0)
	require.True(t, res.Won)
	syncShard(a, peer, name)
	got, ok := peer.Lookup(name)
	require.True(t, ok)
	require.True(t, got.Equal(newPID))
}

// A prior incarnation may have minted the same counter for the same name. The
// restarted node's dot differs only by pid, so it is re-minted above.
func TestState_RestartedOriginSupersedesEqualCounterPriorIncarnationDot(t *testing.T) {
	const name = "supervisor"
	oldPID := makePID("node-A", "h", "old")
	newPID := makePID("node-A", "h", "new")

	peer := NewState("node-B")
	peer.Apply(&Entry{Name: name, PID: oldPID, Node: peer.internNode("node-A"), Counter: 1, Wall: 1})

	a := NewState("node-A")
	require.True(t, a.Register(name, newPID, 2, 0).Won)

	syncShard(peer, a, name)
	syncShard(a, peer, name)

	got, ok := peer.Lookup(name)
	require.True(t, ok)
	require.True(t, got.Equal(newPID), "peer must converge to the fresh registration, got %v", got)
	got, ok = a.Lookup(name)
	require.True(t, ok)
	require.True(t, got.Equal(newPID))
}
