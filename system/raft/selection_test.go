// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	raftapi "github.com/wippyai/runtime/api/raft"
)

// node is a tiny test helper to build cluster.NodeInfo with sane meta defaults.
func node(id, addr, raftPort string, meta map[string]string) cluster.NodeInfo {
	m := cluster.NodeMeta{}
	if raftPort != "" {
		m["raft_port"] = raftPort
	}
	for k, v := range meta {
		m[k] = v
	}
	return cluster.NodeInfo{ID: id, Addr: addr, Meta: m}
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
	// Mesh transport: the raft_port gossip metadata is ignored — the
	// only filter is raft_eligible. raft_port is still injected by the
	// cluster boot for diagnostics during the one-cycle deprecation
	// window but selection no longer reads it.
	nodes := []cluster.NodeInfo{
		// Out of order on purpose.
		node("n3", "10.0.0.3", "7960", map[string]string{"raft_priority": "100"}),
		// Ineligible: explicit false.
		node("n4", "10.0.0.4", "7960", map[string]string{"raft_eligible": "false"}),
		// Eligible even without raft_port (mesh ignores it).
		node("n5", "10.0.0.5", "", nil),
		node("n1", "10.0.0.1", "7960", map[string]string{"raft_priority": "10"}),
		node("n2", "10.0.0.2", "7960", map[string]string{"raft_priority": "10"}),
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
		node("a", "10.0.0.1", "7960", nil), // no priority, no eligible flag → defaults
	}
	got := candidatesFromMembership(nodes)
	require.Len(t, got, 1)
	assert.Equal(t, 100, got[0].Priority)
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
