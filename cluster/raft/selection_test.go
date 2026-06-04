// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	raftapi "github.com/wippyai/runtime/api/cluster/raft"
)

// node is a tiny test helper to build cluster.NodeInfo with sane meta defaults.
func node(id, addr string, meta map[string]string) cluster.NodeInfo {
	m := cluster.NodeMeta{}
	for k, v := range meta {
		m[k] = v
	}
	return cluster.NodeInfo{ID: id, Addr: addr, Meta: m}
}

func TestPickForwardTarget(t *testing.T) {
	srv := func(id string) cluster.NodeInfo {
		return node(id, id, map[string]string{"raft_eligible": "true", "raft_priority": "100"})
	}
	client := func(id string) cluster.NodeInfo {
		return node(id, id, map[string]string{"raft_eligible": "false"})
	}

	t.Run("picks lowest-ranked eligible member, excludes self", func(t *testing.T) {
		// Unordered gossip view; self is an eligible member but must be skipped.
		got, ok := PickForwardTarget([]cluster.NodeInfo{srv("n3"), srv("n1"), srv("n2")}, "n1")
		require.True(t, ok)
		assert.Equal(t, "n2", got, "first eligible by priority/ID order, excluding self")
	})

	t.Run("deterministic regardless of gossip order", func(t *testing.T) {
		a, _ := PickForwardTarget([]cluster.NodeInfo{srv("a"), srv("b"), srv("c")}, "client")
		b, _ := PickForwardTarget([]cluster.NodeInfo{srv("c"), srv("b"), srv("a")}, "client")
		assert.Equal(t, a, b)
		assert.Equal(t, "a", a)
	})

	t.Run("ineligible members are never targeted", func(t *testing.T) {
		got, ok := PickForwardTarget([]cluster.NodeInfo{client("c1"), srv("s1"), client("c2")}, "c1")
		require.True(t, ok)
		assert.Equal(t, "s1", got)
	})

	t.Run("departed target drops out so the next pick rolls over", func(t *testing.T) {
		// Membership reports only live peers: once s1 leaves the snapshot, the
		// deterministic next choice is s2 with no extra failover bookkeeping.
		got, ok := PickForwardTarget([]cluster.NodeInfo{srv("s2"), srv("s3")}, "client")
		require.True(t, ok)
		assert.Equal(t, "s2", got)
	})

	t.Run("no eligible peer yet", func(t *testing.T) {
		_, ok := PickForwardTarget([]cluster.NodeInfo{client("c1")}, "c1")
		assert.False(t, ok)
		_, ok = PickForwardTarget(nil, "self")
		assert.False(t, ok)
		// Only self is eligible -> nothing to forward to.
		_, ok = PickForwardTarget([]cluster.NodeInfo{srv("self")}, "self")
		assert.False(t, ok)
	})
}

func TestDesiredVoterCount(t *testing.T) {
	cases := []struct {
		name      string
		eligible  int
		maxVoters int
		want      int
	}{
		{"zero eligible", 0, 5, 0},
		{"single eligible", 1, 5, 1},
		{"two eligible kept as two (transient post-failure window)", 2, 5, 2},
		{"two eligible clamped to maxVoters=1", 2, 1, 1},
		{"three eligible", 3, 5, 3},
		{"four rounds to three", 4, 5, 3},
		{"five at cap", 5, 5, 5},
		{"six exceeds cap", 6, 5, 5},
		{"ten exceeds cap", 10, 5, 5},
		{"low cap clamps", 7, 3, 3},
		{"max=1 always one", 9, 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := desiredVoterCount(tc.eligible, tc.maxVoters)
			assert.Equal(t, tc.want, got)
			// Result must be odd for N>=3, or 0/1/2 for the smaller cases.
			if got > 2 {
				assert.Equal(t, 1, got%2, "voter count must be odd for N>=3")
			}
		})
	}
}

func TestCandidatesFromMembership_FiltersAndSorts(t *testing.T) {
	// Verifies raft_eligible filtering and deterministic priority/ID ordering.
	nodes := []cluster.NodeInfo{
		// Out of order on purpose.
		node("n3", "10.0.0.3", map[string]string{"raft_priority": "100"}),
		// Ineligible: explicit false.
		node("n4", "10.0.0.4", map[string]string{"raft_eligible": "false"}),
		node("n5", "10.0.0.5", nil),
		node("n1", "10.0.0.1", map[string]string{"raft_priority": "10"}),
		node("n2", "10.0.0.2", map[string]string{"raft_priority": "10"}),
	}

	got := candidatesFromMembership(nodes)
	require.Len(t, got, 4)
	// Sorted: priority 10 first (n1, n2 alphabetically), then 100 (n3, n5).
	assert.Equal(t, "n1", got[0].ID)
	assert.Equal(t, "n2", got[1].ID)
	assert.Equal(t, "n3", got[2].ID)
	assert.Equal(t, "n5", got[3].ID)
	// Under the mesh transport, candidate.Addr is the NodeID itself.
	assert.Equal(t, "n1", got[0].Addr)
}

func TestCandidatesFromMembership_DefaultPriorityAndEligible(t *testing.T) {
	nodes := []cluster.NodeInfo{
		node("a", "10.0.0.1", nil), // no priority, no eligible flag → defaults
	}
	got := candidatesFromMembership(nodes)
	require.Len(t, got, 1)
	assert.Equal(t, 100, got[0].Priority)
}

// TestDeriveMembers_DeterministicAcrossCallers proves the seam non-members
// rely on: same gossip snapshot + same caps → same ordered member set on every
// caller. Two invocations with the same input must yield byte-equal output, and
// changing input order (gossip arrival is order-insensitive) must not change
// the result.
func TestDeriveMembers_DeterministicAcrossCallers(t *testing.T) {
	a := node("a", "10.0.0.1", map[string]string{"raft_priority": "100"})
	b := node("b", "10.0.0.2", map[string]string{"raft_priority": "100"})
	c := node("c", "10.0.0.3", map[string]string{"raft_priority": "10"})
	d := node("d", "10.0.0.4", map[string]string{"raft_priority": "100"})
	e := node("e", "10.0.0.5", map[string]string{"raft_eligible": "false"})

	first := DeriveMembers([]cluster.NodeInfo{a, b, c, d, e}, 3, 2)
	second := DeriveMembers([]cluster.NodeInfo{e, d, c, b, a}, 3, 2) // reordered
	assert.Equal(t, first, second,
		"non-leader node + leader node + reordered input all produce the same member set")

	// 3 voters + up to 2 standbys = 4 eligible candidates retained, ranked.
	// Priority 10 first (c), then priority 100 by ID (a, b, d).
	assert.Equal(t, []cluster.NodeID{"c", "a", "b", "d"}, first)
}

// TestDeriveMembers_BoundsToCaps proves the standby cap holds: extra eligible
// nodes beyond MaxVoters+MaxStandbys are excluded from the derived set so a
// non-member doesn't fan out to a node that won't be in the raft config.
func TestDeriveMembers_BoundsToCaps(t *testing.T) {
	nodes := []cluster.NodeInfo{
		node("a", "10.0.0.1", nil),
		node("b", "10.0.0.2", nil),
		node("c", "10.0.0.3", nil),
		node("d", "10.0.0.4", nil),
		node("e", "10.0.0.5", nil),
		node("f", "10.0.0.6", nil),
		node("g", "10.0.0.7", nil),
	}
	out := DeriveMembers(nodes, 3, 1) // 3 voters + 1 standby = 4 members max
	assert.Len(t, out, 4, "derived set capped to MaxVoters+MaxStandbys")
}

func TestPickVoters_FailureDomainSpread(t *testing.T) {
	// Three domains, four candidates. Target=3 → one per domain.
	cs := []candidate{
		{ID: "a1", FailureDomain: "az-a", Priority: 100, Addr: "x:1"},
		{ID: "a2", FailureDomain: "az-a", Priority: 100, Addr: "x:1"},
		{ID: "b1", FailureDomain: "az-b", Priority: 100, Addr: "x:1"},
		{ID: "c1", FailureDomain: "az-c", Priority: 100, Addr: "x:1"},
	}
	rankCandidates(cs)
	picked := pickVoters(cs, nil, 3)
	require.Len(t, picked, 3)

	// One node per domain expected.
	domains := map[string]int{}
	for _, c := range cs {
		if _, in := picked[c.ID]; in {
			domains[c.FailureDomain]++
		}
	}
	assert.Equal(t, 1, domains["az-a"], "a2 should not have been picked when a1 already was")
	assert.Equal(t, 1, domains["az-b"])
	assert.Equal(t, 1, domains["az-c"])
}

func TestPickVoters_EmptyDomainNotCollapsed(t *testing.T) {
	// Empty failure_domain is its own bucket per node — homogeneous clusters
	// should not collapse to a single voter.
	cs := []candidate{
		{ID: "a", Priority: 100},
		{ID: "b", Priority: 100},
		{ID: "c", Priority: 100},
		{ID: "d", Priority: 100},
		{ID: "e", Priority: 100},
	}
	rankCandidates(cs)
	picked := pickVoters(cs, nil, 5)
	assert.Len(t, picked, 5)
}

func TestPickVoters_FillsRemainingAfterSpread(t *testing.T) {
	// Two domains, target=3 → spread fills 2, then one extra from rank order.
	cs := []candidate{
		{ID: "a1", FailureDomain: "az-a", Priority: 100},
		{ID: "a2", FailureDomain: "az-a", Priority: 100},
		{ID: "b1", FailureDomain: "az-b", Priority: 100},
	}
	rankCandidates(cs)
	picked := pickVoters(cs, nil, 3)
	assert.Len(t, picked, 3)
	assert.Contains(t, picked, cluster.NodeID("a1"))
	assert.Contains(t, picked, cluster.NodeID("a2"))
	assert.Contains(t, picked, cluster.NodeID("b1"))
}

func TestPickVoters_TargetGreaterThanPool(t *testing.T) {
	cs := []candidate{
		{ID: "a", Priority: 100},
		{ID: "b", Priority: 100},
	}
	rankCandidates(cs)
	picked := pickVoters(cs, nil, 5)
	assert.Len(t, picked, 2)
}

func TestPickVoters_StickinessKeepsCurrentVoter(t *testing.T) {
	// Three candidates, target=2. Plain rank picks {a, b}. But b is current
	// voter and ranks at cutoff (target+1=3). Stickiness keeps b in.
	cs := []candidate{
		{ID: "a", Priority: 10},
		{ID: "b", Priority: 30},
		{ID: "c", Priority: 20},
	}
	rankCandidates(cs)
	// Ranked: a (10), c (20), b (30). target=2 → spread picks {a, c}.
	current := map[cluster.NodeID]struct{}{"b": {}}
	picked := pickVoters(cs, current, 2)
	assert.Len(t, picked, 2)
	assert.Contains(t, picked, cluster.NodeID("a"))
	// Stickiness: b ranks within target+1=3 → kept. c gets evicted.
	assert.Contains(t, picked, cluster.NodeID("b"))
	assert.NotContains(t, picked, cluster.NodeID("c"))
}

func TestReconcileDiff_BootstrapEmptyCluster(t *testing.T) {
	cs := []candidate{
		{ID: "a", Addr: "10.0.0.1:7960"},
		{ID: "b", Addr: "10.0.0.2:7960"},
		{ID: "c", Addr: "10.0.0.3:7960"},
	}
	addrs := map[cluster.NodeID]string{"a": "10.0.0.1:7960", "b": "10.0.0.2:7960", "c": "10.0.0.3:7960"}
	desired := map[cluster.NodeID]struct{}{"a": {}, "b": {}, "c": {}}

	ops := reconcileDiff(desired, cs, nil, addrs)
	require.Len(t, ops, 3)
	for _, op := range ops {
		assert.Equal(t, opAddVoter, op.Kind)
	}
}

func TestReconcileDiff_PromoteAndDemote(t *testing.T) {
	cs := []candidate{
		{ID: "a", Addr: "10.0.0.1:7960"},
		{ID: "b", Addr: "10.0.0.2:7960"},
		{ID: "c", Addr: "10.0.0.3:7960"},
	}
	addrs := map[cluster.NodeID]string{"a": "10.0.0.1:7960", "b": "10.0.0.2:7960", "c": "10.0.0.3:7960"}
	current := []raftapi.Server{
		{ID: "a", Address: "10.0.0.1:7960", IsVoter: true},
		{ID: "b", Address: "10.0.0.2:7960", IsVoter: true},
		{ID: "c", Address: "10.0.0.3:7960", IsVoter: false}, // currently nonvoter
	}
	desired := map[cluster.NodeID]struct{}{"a": {}, "c": {}}

	ops := reconcileDiff(desired, cs, current, addrs)
	// Expected: promote c (voter add), demote b (nonvoter). Promote ordered before demote.
	require.Len(t, ops, 2)
	assert.Equal(t, opPromote, ops[0].Kind)
	assert.Equal(t, "c", ops[0].ID)
	assert.Equal(t, opDemote, ops[1].Kind)
	assert.Equal(t, "b", ops[1].ID)
}

func TestReconcileDiff_RemoveIneligible(t *testing.T) {
	cs := []candidate{
		{ID: "a", Addr: "10.0.0.1:7960"},
	}
	addrs := map[cluster.NodeID]string{"a": "10.0.0.1:7960"}
	current := []raftapi.Server{
		{ID: "a", Address: "10.0.0.1:7960", IsVoter: true},
		{ID: "ghost", Address: "10.0.0.99:7960", IsVoter: false},
	}
	desired := map[cluster.NodeID]struct{}{"a": {}}

	ops := reconcileDiff(desired, cs, current, addrs)
	require.Len(t, ops, 1)
	assert.Equal(t, opRemove, ops[0].Kind)
	assert.Equal(t, "ghost", ops[0].ID)
}

func TestReconcileDiff_AddressDriftReAdds(t *testing.T) {
	cs := []candidate{{ID: "a", Addr: "10.0.0.1:7961"}} // new port
	addrs := map[cluster.NodeID]string{"a": "10.0.0.1:7961"}
	current := []raftapi.Server{{ID: "a", Address: "10.0.0.1:7960", IsVoter: true}}
	desired := map[cluster.NodeID]struct{}{"a": {}}

	ops := reconcileDiff(desired, cs, current, addrs)
	require.Len(t, ops, 1)
	assert.Equal(t, opAddVoter, ops[0].Kind, "address change should trigger AddVoter (in-place update)")
	assert.Equal(t, "10.0.0.1:7961", ops[0].Addr)
}

func TestReconcileDiff_NoOpWhenStable(t *testing.T) {
	cs := []candidate{
		{ID: "a", Addr: "10.0.0.1:7960"},
		{ID: "b", Addr: "10.0.0.2:7960"},
	}
	addrs := map[cluster.NodeID]string{"a": "10.0.0.1:7960", "b": "10.0.0.2:7960"}
	current := []raftapi.Server{
		{ID: "a", Address: "10.0.0.1:7960", IsVoter: true},
		{ID: "b", Address: "10.0.0.2:7960", IsVoter: false},
	}
	desired := map[cluster.NodeID]struct{}{"a": {}}

	ops := reconcileDiff(desired, cs, current, addrs)
	assert.Empty(t, ops, "stable state should produce no ops")
}

func TestReconcileDiff_PromoteOrderedBeforeRemove(t *testing.T) {
	// Critical for quorum safety: never shrink before growing.
	cs := []candidate{
		{ID: "new", Addr: "10.0.0.2:7960"},
	}
	addrs := map[cluster.NodeID]string{"new": "10.0.0.2:7960"}
	current := []raftapi.Server{
		{ID: "old", Address: "10.0.0.1:7960", IsVoter: true},
	}
	desired := map[cluster.NodeID]struct{}{"new": {}}

	ops := reconcileDiff(desired, cs, current, addrs)
	require.Len(t, ops, 2)
	// Add must come first.
	assert.Equal(t, opAddVoter, ops[0].Kind)
	assert.Equal(t, "new", ops[0].ID)
	assert.Equal(t, opRemove, ops[1].Kind)
	assert.Equal(t, "old", ops[1].ID)
}
