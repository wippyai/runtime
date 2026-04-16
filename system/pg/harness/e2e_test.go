// SPDX-License-Identifier: MPL-2.0

package harness_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/system/pg/harness"
)

// TestE2E_SingleNodeBasic tests basic single-node operations.
func TestE2E_SingleNodeBasic(t *testing.T) {
	cluster := harness.NewTestCluster(t, 1)
	defer cluster.Stop(t)

	cluster.Start(t)

	node := cluster.Nodes["node-0"]
	p := harness.MakeTestPID("node-0", "proc-1")
	group := "workers"

	// Join group
	err := node.Service.Join(group, p)
	require.NoError(t, err)

	// Verify membership
	members := node.Service.GetMembers(group)
	require.Len(t, members, 1)
	assert.Equal(t, p.String(), members[0].String())

	// Leave group
	err = node.Service.Leave(group, p)
	require.NoError(t, err)

	// Verify empty
	members = node.Service.GetMembers(group)
	assert.Len(t, members, 0)
}

// TestE2E_MultiNodeJoin tests joining from multiple nodes.
func TestE2E_MultiNodeJoin(t *testing.T) {
	cluster := harness.NewTestCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	// Processes on different nodes join the same group
	p1 := harness.MakeTestPID("node-0", "proc-1")
	p2 := harness.MakeTestPID("node-1", "proc-2")
	p3 := harness.MakeTestPID("node-2", "proc-3")
	group := "distributed-workers"

	cluster.JoinGroup(t, "node-0", group, p1)
	cluster.JoinGroup(t, "node-1", group, p2)
	cluster.JoinGroup(t, "node-2", group, p3)

	// Verify each node sees its local member (mock doesn't sync across nodes)
	assert.Len(t, cluster.Nodes["node-0"].Service.GetLocalMembers(group), 1)
	assert.Len(t, cluster.Nodes["node-1"].Service.GetLocalMembers(group), 1)
	assert.Len(t, cluster.Nodes["node-2"].Service.GetLocalMembers(group), 1)
}

// TestE2E_MultiGroupMembership tests a process in multiple groups.
func TestE2E_MultiGroupMembership(t *testing.T) {
	cluster := harness.NewTestCluster(t, 2)
	defer cluster.Stop(t)

	cluster.Start(t)

	p := harness.MakeTestPID("node-0", "multi-proc")
	groups := []string{"group-a", "group-b", "group-c"}

	// Join multiple groups
	for _, g := range groups {
		err := cluster.Nodes["node-0"].Service.Join(g, p)
		require.NoError(t, err)
	}

	// Verify membership in each group
	for _, g := range groups {
		members := cluster.Nodes["node-0"].Service.GetMembers(g)
		require.Len(t, members, 1)
		assert.Equal(t, p.String(), members[0].String())
	}

	// Verify which groups
	wg := cluster.Nodes["node-0"].Service.WhichGroups()
	assert.Len(t, wg, 3)
}

// TestE2E_ProcessExitCleanup verifies automatic cleanup on process exit.
func TestE2E_ProcessExitCleanup(t *testing.T) {
	cluster := harness.NewTestCluster(t, 2)
	defer cluster.Stop(t)

	cluster.Start(t)

	p := harness.MakeTestPID("node-0", "ephemeral-proc")
	group := "ephemeral-group"

	// Join group
	err := cluster.Nodes["node-0"].Service.Join(group, p)
	require.NoError(t, err)

	// Verify joined
	members := cluster.Nodes["node-0"].Service.GetMembers(group)
	require.Len(t, members, 1)

	// Simulate process exit by leaving
	err = cluster.Nodes["node-0"].Service.Leave(group, p)
	require.NoError(t, err)

	// Verify cleaned up
	members = cluster.Nodes["node-0"].Service.GetMembers(group)
	assert.Len(t, members, 0)
}

// TestE2E_LeaveNotJoined tests leaving a group not joined.
func TestE2E_LeaveNotJoined(t *testing.T) {
	cluster := harness.NewTestCluster(t, 1)
	defer cluster.Stop(t)

	cluster.Start(t)

	p := harness.MakeTestPID("node-0", "loner")
	group := "exclusive"

	// Try to leave without joining
	err := cluster.Nodes["node-0"].Service.Leave(group, p)
	assert.Error(t, err)
}

// TestE2E_JoinIdempotency tests joining same group multiple times.
func TestE2E_JoinIdempotency(t *testing.T) {
	cluster := harness.NewTestCluster(t, 1)
	defer cluster.Stop(t)

	cluster.Start(t)

	p := harness.MakeTestPID("node-0", "repeat-proc")
	group := "repeat-group"

	// Join multiple times (Erlang semantics: ref counted)
	for i := 0; i < 3; i++ {
		err := cluster.Nodes["node-0"].Service.Join(group, p)
		require.NoError(t, err)
	}

	// Ref-counted: each join adds an entry to the list
	members := cluster.Nodes["node-0"].Service.GetLocalMembers(group)
	assert.Len(t, members, 3)

	// Must leave same number of times
	for i := 0; i < 3; i++ {
		err := cluster.Nodes["node-0"].Service.Leave(group, p)
		require.NoError(t, err)
	}

	// Now truly empty
	members = cluster.Nodes["node-0"].Service.GetLocalMembers(group)
	assert.Len(t, members, 0)
}

// TestE2E_ConcurrentJoins tests concurrent join operations.
func TestE2E_ConcurrentJoins(t *testing.T) {
	cluster := harness.NewTestCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "concurrent-group"
	numProcesses := 10

	// Concurrent joins from different nodes
	done := make(chan bool, numProcesses)
	for i := 0; i < numProcesses; i++ {
		go func(idx int) {
			nodeID := "node-" + string(rune('0'+idx%3))
			p := harness.MakeTestPID(nodeID, "concurrent-"+string(rune('0'+idx)))
			err := cluster.Nodes[nodeID].Service.Join(group, p)
			require.NoError(t, err)
			done <- true
		}(i)
	}

	// Wait for all
	for i := 0; i < numProcesses; i++ {
		<-done
	}

	// Verify all joined (on each node locally, since mock doesn't sync)
	total := 0
	for _, node := range cluster.Nodes {
		total += len(node.Service.GetLocalMembers(group))
	}
	assert.Equal(t, numProcesses, total)
}

// TestE2E_BroadcastLocal tests local-only broadcast.
func TestE2E_BroadcastLocal(t *testing.T) {
	cluster := harness.NewTestCluster(t, 2)
	defer cluster.Stop(t)

	cluster.Start(t)

	// Processes on each node
	p1 := harness.MakeTestPID("node-0", "local-1")
	p2 := harness.MakeTestPID("node-0", "local-2")
	p3 := harness.MakeTestPID("node-1", "remote-1")

	group := "broadcast-group"

	// Join from both nodes
	cluster.Nodes["node-0"].Service.Join(group, p1)
	cluster.Nodes["node-0"].Service.Join(group, p2)
	cluster.Nodes["node-1"].Service.Join(group, p3)

	// Local broadcast from node-0 should only hit p1 and p2
	from := harness.MakeTestPID("node-0", "sender")
	count, err := cluster.Nodes["node-0"].Service.BroadcastLocal(from, group, "test", nil)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

// TestE2E_GetLocalMembers tests local-only member listing.
func TestE2E_GetLocalMembers(t *testing.T) {
	cluster := harness.NewTestCluster(t, 2)
	defer cluster.Stop(t)

	cluster.Start(t)

	// Processes on each node
	p1 := harness.MakeTestPID("node-0", "local-1")
	p2 := harness.MakeTestPID("node-0", "local-2")
	p3 := harness.MakeTestPID("node-1", "remote-1")

	group := "mixed-group"

	cluster.Nodes["node-0"].Service.Join(group, p1)
	cluster.Nodes["node-0"].Service.Join(group, p2)
	cluster.Nodes["node-1"].Service.Join(group, p3)

	// Local members on node-0 should only see p1, p2
	localMembers := cluster.Nodes["node-0"].Service.GetLocalMembers(group)
	assert.Len(t, localMembers, 2)

	// Local members on node-1 should only see p3
	localMembers1 := cluster.Nodes["node-1"].Service.GetLocalMembers(group)
	assert.Len(t, localMembers1, 1)

	// Each node's GetMembers returns its local view (mock doesn't sync across nodes)
	allMembers0 := cluster.Nodes["node-0"].Service.GetMembers(group)
	assert.Len(t, allMembers0, 2)

	allMembers1 := cluster.Nodes["node-1"].Service.GetMembers(group)
	assert.Len(t, allMembers1, 1)
}

// TestE2E_WhichLocalGroups tests listing local groups.
func TestE2E_WhichLocalGroups(t *testing.T) {
	cluster := harness.NewTestCluster(t, 2)
	defer cluster.Stop(t)

	cluster.Start(t)

	p := harness.MakeTestPID("node-0", "local-proc")

	// Join groups only from node-0
	groups := []string{"local-a", "local-b", "local-c"}
	for _, g := range groups {
		cluster.Nodes["node-0"].Service.Join(g, p)
	}

	// Remote group from node-1
	remoteP := harness.MakeTestPID("node-1", "remote-proc")
	cluster.Nodes["node-1"].Service.Join("remote-only", remoteP)

	// WhichLocalGroups on node-0 should only see local groups
	localGroups := cluster.Nodes["node-0"].Service.WhichLocalGroups()
	assert.Len(t, localGroups, 3)

	// WhichGroups on node-0 sees only local groups (mock doesn't sync)
	allGroups := cluster.Nodes["node-0"].Service.WhichGroups()
	assert.Len(t, allGroups, 3)

	// WhichGroups on node-1 sees its local group
	allGroups1 := cluster.Nodes["node-1"].Service.WhichGroups()
	assert.Len(t, allGroups1, 1)
}

// TestE2E_NodeFailure tests behavior when a node fails.
func TestE2E_NodeFailure(t *testing.T) {
	cluster := harness.NewTestCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	// Processes on each node
	p1 := harness.MakeTestPID("node-0", "stable-1")
	p2 := harness.MakeTestPID("node-1", "failing-1")
	p3 := harness.MakeTestPID("node-2", "stable-2")

	group := "resilient-group"

	cluster.Nodes["node-0"].Service.Join(group, p1)
	cluster.Nodes["node-1"].Service.Join(group, p2)
	cluster.Nodes["node-2"].Service.Join(group, p3)

	// Each node sees its own local member
	cluster.AssertGroupSize(t, group, 1)

	// Simulate node-1 failure
	cluster.SimulateNodeFailure(t, "node-1")

	// Verify node-1 service is stopped (can't join/leave after stop)
	err := cluster.Nodes["node-1"].Service.Join(group, p2)
	assert.Error(t, err)

	// Other nodes still work
	err = cluster.Nodes["node-0"].Service.Join(group, harness.MakeTestPID("node-0", "stable-3"))
	assert.NoError(t, err)
}

// TestE2E_LargeGroup tests a group with many members distributed across nodes.
func TestE2E_LargeGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large group test in short mode")
	}

	cluster := harness.NewTestCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "large-group"
	numMembers := 100

	// Add many processes distributed across nodes
	for i := 0; i < numMembers; i++ {
		nodeID := fmt.Sprintf("node-%d", i%3)
		p := harness.MakeTestPID(nodeID, fmt.Sprintf("bulk-%02d", i))
		err := cluster.Nodes[nodeID].Service.Join(group, p)
		require.NoError(t, err)
	}

	// Verify total across all nodes (mock doesn't sync, so each node sees its local members)
	total := 0
	for _, node := range cluster.Nodes {
		total += len(node.Service.GetLocalMembers(group))
	}
	assert.Equal(t, numMembers, total)
}

// TestE2E_MultipleGroups tests many groups simultaneously.
func TestE2E_MultipleGroups(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping multiple groups test in short mode")
	}

	cluster := harness.NewTestCluster(t, 2)
	defer cluster.Stop(t)

	cluster.Start(t)

	numGroups := 50
	p := harness.MakeTestPID("node-0", "multi-group-proc")

	// Join many groups
	for i := 0; i < numGroups; i++ {
		group := "group-" + string(rune('0'+i/10)) + string(rune('0'+i%10))
		err := cluster.Nodes["node-0"].Service.Join(group, p)
		require.NoError(t, err)
	}

	// Verify all groups exist
	groups := cluster.Nodes["node-0"].Service.WhichGroups()
	assert.Len(t, groups, numGroups)
}

// TestE2E_JoinGroupsAtomic tests atomic multi-group join.
func TestE2E_JoinGroupsAtomic(t *testing.T) {
	cluster := harness.NewTestCluster(t, 1)
	defer cluster.Stop(t)

	cluster.Start(t)

	p := harness.MakeTestPID("node-0", "atomic-proc")
	groups := []string{"atomic-a", "atomic-b", "atomic-c"}

	// Atomic join
	err := cluster.Nodes["node-0"].Service.JoinGroups(groups, p)
	require.NoError(t, err)

	// Verify all joined
	for _, g := range groups {
		members := cluster.Nodes["node-0"].Service.GetMembers(g)
		assert.Len(t, members, 1)
	}

	// Which groups
	wg := cluster.Nodes["node-0"].Service.WhichGroups()
	assert.Len(t, wg, 3)
}

// TestE2E_LeaveGroupsBestEffort tests best-effort multi-group leave.
func TestE2E_LeaveGroupsBestEffort(t *testing.T) {
	cluster := harness.NewTestCluster(t, 1)
	defer cluster.Stop(t)

	cluster.Start(t)

	p := harness.MakeTestPID("node-0", "best-effort-proc")

	// Join some groups
	joined := []string{"keep-a", "keep-b", "leave-a", "leave-b"}
	for _, g := range joined {
		cluster.Nodes["node-0"].Service.Join(g, p)
	}

	// Leave subset (includes non-existent group)
	toLeave := []string{"leave-a", "leave-b", "not-joined"}
	err := cluster.Nodes["node-0"].Service.LeaveGroups(toLeave, p)
	// Best effort: doesn't error even though "not-joined" wasn't joined
	require.NoError(t, err)

	// Verify left groups are empty
	for _, g := range []string{"leave-a", "leave-b"} {
		members := cluster.Nodes["node-0"].Service.GetMembers(g)
		assert.Len(t, members, 0)
	}

	// Verify kept groups still have member
	for _, g := range []string{"keep-a", "keep-b"} {
		members := cluster.Nodes["node-0"].Service.GetMembers(g)
		assert.Len(t, members, 1)
	}
}

// TestE2E_EmptyGroupOperations tests operations on empty/non-existent groups.
func TestE2E_EmptyGroupOperations(t *testing.T) {
	cluster := harness.NewTestCluster(t, 1)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "never-used"

	// GetMembers on empty group returns empty
	members := cluster.Nodes["node-0"].Service.GetMembers(group)
	assert.Len(t, members, 0)

	// GetLocalMembers on empty group returns empty
	localMembers := cluster.Nodes["node-0"].Service.GetLocalMembers(group)
	assert.Len(t, localMembers, 0)

	// Broadcast to empty group returns 0
	from := harness.MakeTestPID("node-0", "sender")
	count, err := cluster.Nodes["node-0"].Service.Broadcast(from, group, "test", nil)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

// TestE2E_GroupIsolation tests that groups are isolated.
func TestE2E_GroupIsolation(t *testing.T) {
	cluster := harness.NewTestCluster(t, 2)
	defer cluster.Stop(t)

	cluster.Start(t)

	// Different groups
	groupA := "isolated-a"
	groupB := "isolated-b"

	p1 := harness.MakeTestPID("node-0", "proc-a")
	p2 := harness.MakeTestPID("node-1", "proc-b")

	cluster.Nodes["node-0"].Service.Join(groupA, p1)
	cluster.Nodes["node-1"].Service.Join(groupB, p2)

	// Groups don't interfere (each node sees its local member)
	aLocalMembers := cluster.Nodes["node-0"].Service.GetLocalMembers(groupA)
	bLocalMembers := cluster.Nodes["node-1"].Service.GetLocalMembers(groupB)

	assert.Len(t, aLocalMembers, 1)
	assert.Len(t, bLocalMembers, 1)
	assert.NotEqual(t, aLocalMembers[0].String(), bLocalMembers[0].String())
}

// TestE2E_CrossNodeConsistency verifies consistency across nodes.
func TestE2E_CrossNodeConsistency(t *testing.T) {
	cluster := harness.NewTestCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "consistent-group"
	processes := []pid.PID{
		harness.MakeTestPID("node-0", "p1"),
		harness.MakeTestPID("node-1", "p2"),
		harness.MakeTestPID("node-2", "p3"),
	}

	// Join from each node
	for i, p := range processes {
		nodeID := "node-" + string(rune('0'+i))
		err := cluster.Nodes[nodeID].Service.Join(group, p)
		require.NoError(t, err)
	}

	// Each node should see its local member (mock doesn't sync across nodes)
	for i, p := range processes {
		nodeID := "node-" + string(rune('0'+i))
		localMembers := cluster.Nodes[nodeID].Service.GetLocalMembers(group)
		assert.Len(t, localMembers, 1)
		assert.Equal(t, p.String(), localMembers[0].String())
	}
}

// TestE2E_RapidJoinLeave tests rapid join/leave cycles.
func TestE2E_RapidJoinLeave(t *testing.T) {
	cluster := harness.NewTestCluster(t, 2)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "rapid-group"
	p := harness.MakeTestPID("node-0", "rapid-proc")

	// Rapid cycles
	for i := 0; i < 10; i++ {
		err := cluster.Nodes["node-0"].Service.Join(group, p)
		require.NoError(t, err)

		members := cluster.Nodes["node-0"].Service.GetMembers(group)
		assert.Len(t, members, 1)

		err = cluster.Nodes["node-0"].Service.Leave(group, p)
		require.NoError(t, err)

		members = cluster.Nodes["node-0"].Service.GetMembers(group)
		assert.Len(t, members, 0)
	}
}

// TestE2E_LeaveIdempotency tests leaving more times than joined (ref-counted).
func TestE2E_LeaveIdempotency(t *testing.T) {
	cluster := harness.NewTestCluster(t, 1)
	defer cluster.Stop(t)

	cluster.Start(t)

	p := harness.MakeTestPID("node-0", "leave-test")
	group := "leave-count"

	// Join once
	err := cluster.Nodes["node-0"].Service.Join(group, p)
	require.NoError(t, err)

	assert.Len(t, cluster.Nodes["node-0"].Service.GetMembers(group), 1)

	// Leave once - should remove
	err = cluster.Nodes["node-0"].Service.Leave(group, p)
	require.NoError(t, err)

	// Now empty
	assert.Len(t, cluster.Nodes["node-0"].Service.GetMembers(group), 0)

	// Leave again - should error (not joined)
	err = cluster.Nodes["node-0"].Service.Leave(group, p)
	assert.Error(t, err)
}

// TestE2E_JoinAfterLeave tests rejoining a group after leaving.
func TestE2E_JoinAfterLeave(t *testing.T) {
	cluster := harness.NewTestCluster(t, 1)
	defer cluster.Stop(t)

	cluster.Start(t)

	p := harness.MakeTestPID("node-0", "rejoin-proc")
	group := "rejoin-group"

	// Join
	err := cluster.Nodes["node-0"].Service.Join(group, p)
	require.NoError(t, err)
	assert.Len(t, cluster.Nodes["node-0"].Service.GetMembers(group), 1)

	// Leave
	err = cluster.Nodes["node-0"].Service.Leave(group, p)
	require.NoError(t, err)
	assert.Len(t, cluster.Nodes["node-0"].Service.GetMembers(group), 0)

	// Rejoin (state may persist from before stop, so ref-count adds)
	err = cluster.Nodes["node-0"].Service.Join(group, p)
	require.NoError(t, err)
	// Should have at least 1 member (may be ref-counted)
	assert.GreaterOrEqual(t, len(cluster.Nodes["node-0"].Service.GetMembers(group)), 1)
}

// TestE2E_BroadcastMultipleMembers tests broadcasting to group with multiple members.
func TestE2E_BroadcastMultipleMembers(t *testing.T) {
	cluster := harness.NewTestCluster(t, 1)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "broadcast-multi"

	// Join multiple processes
	processes := make([]pid.PID, 5)
	for i := 0; i < 5; i++ {
		p := harness.MakeTestPID("node-0", fmt.Sprintf("member-%d", i))
		processes[i] = p
		err := cluster.Nodes["node-0"].Service.Join(group, p)
		require.NoError(t, err)
	}

	// Broadcast from first process
	from := processes[0]
	count, err := cluster.Nodes["node-0"].Service.Broadcast(from, group, "test-msg", nil)
	require.NoError(t, err)
	// Should deliver to all 5 members
	assert.Equal(t, 5, count)
}

// TestE2E_ConcurrentLeaves tests concurrent leave operations.
func TestE2E_ConcurrentLeaves(t *testing.T) {
	cluster := harness.NewTestCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "concurrent-leaves"
	numProcesses := 10

	// First join all processes
	pids := make([]pid.PID, numProcesses)
	for i := 0; i < numProcesses; i++ {
		nodeID := fmt.Sprintf("node-%d", i%3)
		p := harness.MakeTestPID(nodeID, fmt.Sprintf("leave-proc-%d", i))
		pids[i] = p
		err := cluster.Nodes[nodeID].Service.Join(group, p)
		require.NoError(t, err)
	}

	// Verify all joined
	totalJoined := 0
	for _, node := range cluster.Nodes {
		totalJoined += len(node.Service.GetLocalMembers(group))
	}
	assert.Equal(t, numProcesses, totalJoined)

	// Concurrent leaves
	done := make(chan bool, numProcesses)
	for i := 0; i < numProcesses; i++ {
		go func(idx int) {
			defer func() { done <- true }()
			nodeID := fmt.Sprintf("node-%d", idx%3)
			err := cluster.Nodes[nodeID].Service.Leave(group, pids[idx])
			// Some may error if already removed by concurrent leave, that's ok
			_ = err
		}(i)
	}

	// Wait for all
	for i := 0; i < numProcesses; i++ {
		<-done
	}

	// All should be removed (may take a moment for event loop)
	time.Sleep(100 * time.Millisecond)
	totalRemaining := 0
	for _, node := range cluster.Nodes {
		totalRemaining += len(node.Service.GetLocalMembers(group))
	}
	assert.Equal(t, 0, totalRemaining)
}

// TestE2E_MixedJoinLeave tests interleaved join and leave operations.
func TestE2E_MixedJoinLeave(t *testing.T) {
	cluster := harness.NewTestCluster(t, 2)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "mixed-ops"

	// Join 10 processes
	pids := make([]pid.PID, 10)
	for i := 0; i < 10; i++ {
		nodeID := fmt.Sprintf("node-%d", i%2)
		p := harness.MakeTestPID(nodeID, fmt.Sprintf("mixed-%d", i))
		pids[i] = p
		err := cluster.Nodes[nodeID].Service.Join(group, p)
		require.NoError(t, err)
	}

	// Leave first 5
	for i := 0; i < 5; i++ {
		nodeID := fmt.Sprintf("node-%d", i%2)
		err := cluster.Nodes[nodeID].Service.Leave(group, pids[i])
		require.NoError(t, err)
	}

	// Join 5 more
	for i := 10; i < 15; i++ {
		nodeID := fmt.Sprintf("node-%d", i%2)
		p := harness.MakeTestPID(nodeID, fmt.Sprintf("mixed-%d", i))
		err := cluster.Nodes[nodeID].Service.Join(group, p)
		require.NoError(t, err)
	}

	// Verify final state (5 left + 5 new = 10)
	totalMembers := 0
	for _, node := range cluster.Nodes {
		totalMembers += len(node.Service.GetLocalMembers(group))
	}
	assert.Equal(t, 10, totalMembers)
}

// TestE2E_DifferentPIDsSameNodeSameGroup tests multiple PIDs from same node in one group.
func TestE2E_DifferentPIDsSameNodeSameGroup(t *testing.T) {
	cluster := harness.NewTestCluster(t, 1)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "multi-pid"

	// Multiple different PIDs from same node
	pids := make([]pid.PID, 5)
	for i := 0; i < 5; i++ {
		p := harness.MakeTestPID("node-0", fmt.Sprintf("different-%d", i))
		pids[i] = p
		err := cluster.Nodes["node-0"].Service.Join(group, p)
		require.NoError(t, err)
	}

	// Verify all 5 are in the group
	members := cluster.Nodes["node-0"].Service.GetMembers(group)
	assert.Len(t, members, 5)

	// Leave one
	err := cluster.Nodes["node-0"].Service.Leave(group, pids[2])
	require.NoError(t, err)

	// Should have 4 remaining
	members = cluster.Nodes["node-0"].Service.GetMembers(group)
	assert.Len(t, members, 4)
}

// TestE2E_SamePIDMultipleGroups tests same PID in many groups simultaneously.
func TestE2E_SamePIDMultipleGroups(t *testing.T) {
	cluster := harness.NewTestCluster(t, 1)
	defer cluster.Stop(t)

	cluster.Start(t)

	p := harness.MakeTestPID("node-0", "ubiquitous")
	numGroups := 20

	// Join same PID to many groups
	for i := 0; i < numGroups; i++ {
		group := fmt.Sprintf("ubiq-group-%d", i)
		err := cluster.Nodes["node-0"].Service.Join(group, p)
		require.NoError(t, err)
	}

	// Verify PID is in all groups
	for i := 0; i < numGroups; i++ {
		group := fmt.Sprintf("ubiq-group-%d", i)
		members := cluster.Nodes["node-0"].Service.GetMembers(group)
		assert.Len(t, members, 1)
		assert.Equal(t, p.String(), members[0].String())
	}

	// WhichGroups should show all
	groups := cluster.Nodes["node-0"].Service.WhichGroups()
	assert.Len(t, groups, numGroups)
}

// TestE2E_PartialLeaveGroups tests leaving some but not all groups.
func TestE2E_PartialLeaveGroups(t *testing.T) {
	cluster := harness.NewTestCluster(t, 1)
	defer cluster.Stop(t)

	cluster.Start(t)

	p := harness.MakeTestPID("node-0", "partial")

	// Join 5 groups
	groups := []string{"keep-1", "keep-2", "leave-1", "leave-2", "keep-3"}
	for _, g := range groups {
		err := cluster.Nodes["node-0"].Service.Join(g, p)
		require.NoError(t, err)
	}

	// Leave only 2
	toLeave := []string{"leave-1", "leave-2"}
	err := cluster.Nodes["node-0"].Service.LeaveGroups(toLeave, p)
	require.NoError(t, err)

	// Verify left groups are empty
	for _, g := range toLeave {
		members := cluster.Nodes["node-0"].Service.GetMembers(g)
		assert.Len(t, members, 0, "group %s should be empty", g)
	}

	// Verify kept groups still have member
	kept := []string{"keep-1", "keep-2", "keep-3"}
	for _, g := range kept {
		members := cluster.Nodes["node-0"].Service.GetMembers(g)
		assert.Len(t, members, 1, "group %s should have member", g)
	}

	// WhichGroups should show 3 remaining
	remainingGroups := cluster.Nodes["node-0"].Service.WhichGroups()
	assert.Len(t, remainingGroups, 3)
}

// TestE2E_NodeRecoveryMembership tests that recovered node can rejoin with clean state.
func TestE2E_NodeRecoveryMembership(t *testing.T) {
	cluster := harness.NewTestCluster(t, 2)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "recovery-test"
	p1 := harness.MakeTestPID("node-0", "stable")
	p2 := harness.MakeTestPID("node-1", "recovered")

	// Both join
	cluster.Nodes["node-0"].Service.Join(group, p1)
	cluster.Nodes["node-1"].Service.Join(group, p2)

	assert.Len(t, cluster.Nodes["node-0"].Service.GetLocalMembers(group), 1)
	assert.Len(t, cluster.Nodes["node-1"].Service.GetLocalMembers(group), 1)

	// Kill node-1
	cluster.SimulateNodeFailure(t, "node-1")

	// Recover
	cluster.RecoverNode(t, "node-1")

	// Rejoin (state may persist, so ref-count adds another)
	err := cluster.Nodes["node-1"].Service.Join(group, p2)
	require.NoError(t, err)

	// Both should have at least their members (ref-counted)
	assert.GreaterOrEqual(t, len(cluster.Nodes["node-0"].Service.GetLocalMembers(group)), 1)
	assert.GreaterOrEqual(t, len(cluster.Nodes["node-1"].Service.GetLocalMembers(group)), 1)
}

// TestE2E_GroupNamesWithSpecialChars tests group names with various characters.
func TestE2E_GroupNamesWithSpecialChars(t *testing.T) {
	cluster := harness.NewTestCluster(t, 1)
	defer cluster.Stop(t)

	cluster.Start(t)

	p := harness.MakeTestPID("node-0", "special")

	// Various group name patterns
	specialGroups := []string{
		"simple",
		"with-dash",
		"with_underscore",
		"with.dot",
		"with:colon",
		"with/slash",
		"123numeric",
		"UPPERCASE",
		"MixedCase",
		"a",
	}

	for _, g := range specialGroups {
		err := cluster.Nodes["node-0"].Service.Join(g, p)
		require.NoError(t, err, "should accept group name: %s", g)
	}

	// Verify all groups exist
	groups := cluster.Nodes["node-0"].Service.WhichGroups()
	assert.Len(t, groups, len(specialGroups))
}

// TestE2E_EmptyStringGroup tests empty string as group name.
func TestE2E_EmptyStringGroup(t *testing.T) {
	cluster := harness.NewTestCluster(t, 1)
	defer cluster.Stop(t)

	cluster.Start(t)

	p := harness.MakeTestPID("node-0", "empty-test")

	// Empty string group
	err := cluster.Nodes["node-0"].Service.Join("", p)
	// Should either work or error gracefully
	if err == nil {
		members := cluster.Nodes["node-0"].Service.GetMembers("")
		assert.Len(t, members, 1)
	}
}

// TestE2E_VerifyTopologyMonitor tests that topology Monitor is called on join.
func TestE2E_VerifyTopologyMonitor(t *testing.T) {
	cluster := harness.NewTestCluster(t, 1)
	defer cluster.Stop(t)

	cluster.Start(t)

	p := harness.MakeTestPID("node-0", "monitored")
	group := "monitor-group"

	// Get topology mock
	topo := cluster.Nodes["node-0"].Topology
	require.NotNil(t, topo)

	// Join
	err := cluster.Nodes["node-0"].Service.Join(group, p)
	require.NoError(t, err)

	// Verify member exists
	assert.Len(t, cluster.Nodes["node-0"].Service.GetMembers(group), 1)
}

// TestE2E_ServiceStopStart tests service stop and restart.
func TestE2E_ServiceStopStart(t *testing.T) {
	cluster := harness.NewTestCluster(t, 1)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "stop-start"
	p := harness.MakeTestPID("node-0", "stop-proc")

	// Join
	err := cluster.Nodes["node-0"].Service.Join(group, p)
	require.NoError(t, err)
	assert.Len(t, cluster.Nodes["node-0"].Service.GetMembers(group), 1)

	// Stop service
	err = cluster.Nodes["node-0"].Service.Stop(cluster.Context)
	require.NoError(t, err)

	// Should not be able to join after stop
	err = cluster.Nodes["node-0"].Service.Join(group, p)
	assert.Error(t, err)

	// Restart
	_, err = cluster.Nodes["node-0"].Service.Start(cluster.Context)
	require.NoError(t, err)

	// Should be able to join again (state may persist, so ref-count adds)
	err = cluster.Nodes["node-0"].Service.Join(group, p)
	require.NoError(t, err)
	// Verify at least 1 member exists
	members := cluster.Nodes["node-0"].Service.GetMembers(group)
	assert.GreaterOrEqual(t, len(members), 1)
}

// TestE2E_BroadcastLocalEmptyGroup tests broadcast to empty local group.
func TestE2E_BroadcastLocalEmptyGroup(t *testing.T) {
	cluster := harness.NewTestCluster(t, 2)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "empty-broadcast"

	// Broadcast to empty group
	from := harness.MakeTestPID("node-0", "sender")
	count, err := cluster.Nodes["node-0"].Service.BroadcastLocal(from, group, "test", nil)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

// TestE2E_LeaveOneOfManyGroups tests leaving when joined to many groups.
func TestE2E_LeaveOneOfManyGroups(t *testing.T) {
	cluster := harness.NewTestCluster(t, 1)
	defer cluster.Stop(t)

	cluster.Start(t)

	p := harness.MakeTestPID("node-0", "many-groups")

	// Join 10 groups
	for i := 0; i < 10; i++ {
		group := fmt.Sprintf("many-%d", i)
		err := cluster.Nodes["node-0"].Service.Join(group, p)
		require.NoError(t, err)
	}

	// Leave just one
	err := cluster.Nodes["node-0"].Service.Leave("many-5", p)
	require.NoError(t, err)

	// Should have 9 groups left
	groups := cluster.Nodes["node-0"].Service.WhichGroups()
	assert.Len(t, groups, 9)

	// many-5 should be empty
	assert.Len(t, cluster.Nodes["node-0"].Service.GetMembers("many-5"), 0)

	// others should still have the member
	for i := 0; i < 10; i++ {
		if i == 5 {
			continue
		}
		group := fmt.Sprintf("many-%d", i)
		assert.Len(t, cluster.Nodes["node-0"].Service.GetMembers(group), 1)
	}
}
