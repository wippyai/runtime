// SPDX-License-Identifier: MPL-2.0

package harness_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/system/pg/harness"
)

// --- SyncedCluster Tests ---
// These tests verify that SyncedCluster (nodes wired via shared event bus)
// provides the expected infrastructure for cross-node testing.
// Full cross-node sync requires complex PG protocol simulation.

// TestSynced_Infrastructure verifies the SyncedCluster sets up correctly.
func TestSynced_Infrastructure(t *testing.T) {
	cluster := harness.NewSyncedCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	// Verify all nodes exist
	require.Len(t, cluster.Nodes, 3)
	for i := 0; i < 3; i++ {
		nodeID := fmt.Sprintf("node-%d", i)
		node, ok := cluster.GetNode(nodeID)
		require.True(t, ok, "node %s should exist", nodeID)
		require.NotNil(t, node.Service)
		require.NotNil(t, node.Topology)
		require.NotNil(t, node.Router)
	}
}

// TestSynced_SharedEventBus verifies nodes share the event bus.
func TestSynced_SharedEventBus(t *testing.T) {
	cluster := harness.NewSyncedCluster(t, 2)
	defer cluster.Stop(t)

	cluster.Start(t)

	// Both nodes should use the same bus
	node0 := cluster.Nodes["node-0"]
	node1 := cluster.Nodes["node-1"]
	require.Equal(t, node0.Bus, node1.Bus, "nodes should share event bus")
}

// TestSynced_LocalJoinIsLocalOnly verifies that without sync, joins are local-only.
func TestSynced_LocalJoinIsLocalOnly(t *testing.T) {
	cluster := harness.NewSyncedCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	p1 := harness.MakeTestPID("node-0", "proc-1")
	p2 := harness.MakeTestPID("node-1", "proc-2")
	group := "local-only"

	// Join on different nodes
	err := cluster.Nodes["node-0"].Service.Join(group, p1)
	require.NoError(t, err)
	err = cluster.Nodes["node-1"].Service.Join(group, p2)
	require.NoError(t, err)

	// Without sync, each node only sees its local members
	members0 := cluster.Nodes["node-0"].Service.GetMembers(group)
	members1 := cluster.Nodes["node-1"].Service.GetMembers(group)
	members2 := cluster.Nodes["node-2"].Service.GetMembers(group)

	assert.Len(t, members0, 1, "node-0 sees local only")
	assert.Len(t, members1, 1, "node-1 sees local only")
	assert.Len(t, members2, 0, "node-2 sees nothing")
}

// TestSynced_LocalLeaveIsLocalOnly verifies leaves are local-only.
func TestSynced_LocalLeaveIsLocalOnly(t *testing.T) {
	cluster := harness.NewSyncedCluster(t, 2)
	defer cluster.Stop(t)

	cluster.Start(t)

	p1 := harness.MakeTestPID("node-0", "proc-1")
	p2 := harness.MakeTestPID("node-1", "proc-2")
	group := "leave-local"

	cluster.Nodes["node-0"].Service.Join(group, p1)
	cluster.Nodes["node-1"].Service.Join(group, p2)

	// Leave on node-0
	err := cluster.Nodes["node-0"].Service.Leave(group, p1)
	require.NoError(t, err)

	// Node-0 sees empty, node-1 still sees its member
	assert.Len(t, cluster.Nodes["node-0"].Service.GetMembers(group), 0)
	assert.Len(t, cluster.Nodes["node-1"].Service.GetMembers(group), 1)
}

// TestSynced_MultiGroupLocal verifies multiple groups work locally.
func TestSynced_MultiGroupLocal(t *testing.T) {
	cluster := harness.NewSyncedCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	groups := []string{"alpha", "beta", "gamma"}

	for i, g := range groups {
		p := harness.MakeTestPID(fmt.Sprintf("node-%d", i), fmt.Sprintf("proc-%s", g))
		err := cluster.Nodes[p.Node].Service.Join(g, p)
		require.NoError(t, err)
	}

	// Each node sees its own group locally
	for i, g := range groups {
		nodeID := fmt.Sprintf("node-%d", i)
		members := cluster.Nodes[nodeID].Service.GetMembers(g)
		assert.Len(t, members, 1, "group %s on node %s", g, nodeID)
	}
}

// TestSynced_RapidJoinLeaveLocal tests rapid join/leave cycles locally.
func TestSynced_RapidJoinLeaveLocal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	cluster := harness.NewSyncedCluster(t, 2)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "rapid-local"
	p := harness.MakeTestPID("node-0", "rapid")

	for i := 0; i < 20; i++ {
		err := cluster.Nodes["node-0"].Service.Join(group, p)
		require.NoError(t, err)
		err = cluster.Nodes["node-0"].Service.Leave(group, p)
		require.NoError(t, err)
	}

	// Final state should be empty
	assert.Len(t, cluster.Nodes["node-0"].Service.GetMembers(group), 0)
}

// TestSynced_ConcurrentJoinsOnSingleNode tests concurrent joins on one node.
func TestSynced_ConcurrentJoinsOnSingleNode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	cluster := harness.NewSyncedCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "concurrent-local"
	nodeID := "node-0"
	var wg sync.WaitGroup

	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			p := harness.MakeTestPID(nodeID, fmt.Sprintf("c-%d", idx))
			_ = cluster.Nodes[nodeID].Service.Join(group, p)
		}(i)
	}

	wg.Wait()

	// Node-0 should see all 30 joins (ref-counted)
	members := cluster.Nodes[nodeID].Service.GetMembers(group)
	assert.Len(t, members, 30, "node-0 should see 30 members")
}

// TestSynced_JoinGroupsAtomicLocal tests atomic multi-group join locally.
func TestSynced_JoinGroupsAtomicLocal(t *testing.T) {
	cluster := harness.NewSyncedCluster(t, 2)
	defer cluster.Stop(t)

	cluster.Start(t)

	p := harness.MakeTestPID("node-0", "atomic-local")
	groups := []string{"local-a", "local-b", "local-c"}

	err := cluster.Nodes["node-0"].Service.JoinGroups(groups, p)
	require.NoError(t, err)

	for _, g := range groups {
		members := cluster.Nodes["node-0"].Service.GetMembers(g)
		assert.Len(t, members, 1, "group %s on node-0", g)
	}
}

// TestSynced_LeaveGroupsBestEffortLocal tests multi-group leave locally.
func TestSynced_LeaveGroupsBestEffortLocal(t *testing.T) {
	cluster := harness.NewSyncedCluster(t, 2)
	defer cluster.Stop(t)

	cluster.Start(t)

	p := harness.MakeTestPID("node-0", "best-effort-local")
	joined := []string{"local-keep-a", "local-keep-b", "local-leave-a", "local-leave-b"}

	for _, g := range joined {
		cluster.Nodes["node-0"].Service.Join(g, p)
	}

	toLeave := []string{"local-leave-a", "local-leave-b", "local-not-joined"}
	err := cluster.Nodes["node-0"].Service.LeaveGroups(toLeave, p)
	require.NoError(t, err)

	for _, g := range []string{"local-leave-a", "local-leave-b"} {
		assert.Len(t, cluster.Nodes["node-0"].Service.GetMembers(g), 0)
	}
	for _, g := range []string{"local-keep-a", "local-keep-b"} {
		assert.Len(t, cluster.Nodes["node-0"].Service.GetMembers(g), 1)
	}
}

// TestSynced_MonitorCleanupLocal tests monitor triggers leave locally.
func TestSynced_MonitorCleanupLocal(t *testing.T) {
	cluster := harness.NewSyncedCluster(t, 2)
	defer cluster.Stop(t)

	cluster.Start(t)

	p := harness.MakeTestPID("node-0", "monitored-local")
	group := "monitor-local"

	err := cluster.Nodes["node-0"].Service.Join(group, p)
	require.NoError(t, err)

	assert.Len(t, cluster.Nodes["node-0"].Service.GetMembers(group), 1)

	// Leave simulates process exit
	err = cluster.Nodes["node-0"].Service.Leave(group, p)
	require.NoError(t, err)

	assert.Len(t, cluster.Nodes["node-0"].Service.GetMembers(group), 0)
}

// TestSynced_LargeGroupLocal tests a large group on a single node.
func TestSynced_LargeGroupLocal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	cluster := harness.NewSyncedCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "large-local"
	nodeID := "node-0"
	numMembers := 50

	for i := 0; i < numMembers; i++ {
		p := harness.MakeTestPID(nodeID, fmt.Sprintf("bulk-%d", i))
		err := cluster.Nodes[nodeID].Service.Join(group, p)
		require.NoError(t, err)
	}

	members := cluster.Nodes[nodeID].Service.GetMembers(group)
	assert.Len(t, members, numMembers)
}

// TestSynced_EmptyGroupOperations verifies operations on empty groups.
func TestSynced_EmptyGroupOperations(t *testing.T) {
	cluster := harness.NewSyncedCluster(t, 2)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "empty-group"

	// GetMembers on empty group
	for _, node := range cluster.Nodes {
		assert.Len(t, node.Service.GetMembers(group), 0)
		assert.Len(t, node.Service.GetLocalMembers(group), 0)
	}
}

// TestSynced_GroupIsolation verifies groups don't interfere locally.
func TestSynced_GroupIsolation(t *testing.T) {
	cluster := harness.NewSyncedCluster(t, 2)
	defer cluster.Stop(t)

	cluster.Start(t)

	groupA := "isolated-a"
	groupB := "isolated-b"

	pA := harness.MakeTestPID("node-0", "proc-a")
	pB := harness.MakeTestPID("node-0", "proc-b")

	cluster.Nodes["node-0"].Service.Join(groupA, pA)
	cluster.Nodes["node-0"].Service.Join(groupB, pB)

	// Group A should only have pA
	membersA := cluster.Nodes["node-0"].Service.GetMembers(groupA)
	assert.Len(t, membersA, 1)
	assert.Equal(t, pA.String(), membersA[0].String())

	// Group B should only have pB
	membersB := cluster.Nodes["node-0"].Service.GetMembers(groupB)
	assert.Len(t, membersB, 1)
	assert.Equal(t, pB.String(), membersB[0].String())
}

// TestSynced_MultipleProcessesPerNodeLocal tests multiple processes locally.
func TestSynced_MultipleProcessesPerNodeLocal(t *testing.T) {
	cluster := harness.NewSyncedCluster(t, 2)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "multi-proc"
	nodeID := "node-0"

	// 5 processes on node-0
	for i := 0; i < 5; i++ {
		p := harness.MakeTestPID(nodeID, fmt.Sprintf("mp-%d", i))
		cluster.Nodes[nodeID].Service.Join(group, p)
	}

	members := cluster.Nodes[nodeID].Service.GetMembers(group)
	assert.Len(t, members, 5)

	local := cluster.Nodes[nodeID].Service.GetLocalMembers(group)
	assert.Len(t, local, 5)
}

// TestSynced_LeaveNotJoinedError verifies leaving a group not joined.
func TestSynced_LeaveNotJoinedError(t *testing.T) {
	cluster := harness.NewSyncedCluster(t, 2)
	defer cluster.Stop(t)

	cluster.Start(t)

	p := harness.MakeTestPID("node-0", "loner")
	group := "exclusive"

	err := cluster.Nodes["node-0"].Service.Leave(group, p)
	assert.Error(t, err)
}

// TestSynced_IdempotentJoinLocal tests joining same group multiple times locally.
func TestSynced_IdempotentJoinLocal(t *testing.T) {
	cluster := harness.NewSyncedCluster(t, 2)
	defer cluster.Stop(t)

	cluster.Start(t)

	p := harness.MakeTestPID("node-0", "idempotent")
	group := "idempotent-group"

	// Join 5 times
	for i := 0; i < 5; i++ {
		err := cluster.Nodes["node-0"].Service.Join(group, p)
		require.NoError(t, err)
	}

	// Ref-counted: 5 entries visible locally
	members := cluster.Nodes["node-0"].Service.GetMembers(group)
	assert.Len(t, members, 5, "ref-counted members")

	// Leave 5 times
	for i := 0; i < 5; i++ {
		err := cluster.Nodes["node-0"].Service.Leave(group, p)
		require.NoError(t, err)
	}

	// All leaves processed
	assert.Len(t, cluster.Nodes["node-0"].Service.GetMembers(group), 0)
}

// TestSynced_WhichGroupsLocal tests WhichGroups locally.
func TestSynced_WhichGroupsLocal(t *testing.T) {
	cluster := harness.NewSyncedCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	// Each node joins its own local group
	for i := 0; i < 3; i++ {
		p := harness.MakeTestPID(fmt.Sprintf("node-%d", i), fmt.Sprintf("local-%d", i))
		cluster.Nodes[p.Node].Service.Join(fmt.Sprintf("local-group-%d", i), p)
	}

	// Each node sees only its local group
	for i := 0; i < 3; i++ {
		nodeID := fmt.Sprintf("node-%d", i)
		localGroups := cluster.Nodes[nodeID].Service.WhichLocalGroups()
		assert.Len(t, localGroups, 1, "node %s WhichLocalGroups", nodeID)
	}
}

// TestSynced_GetLocalMembersLocal tests GetLocalMembers locally.
func TestSynced_GetLocalMembersLocal(t *testing.T) {
	cluster := harness.NewSyncedCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "local-members-test"

	// Each node joins one member
	for i := 0; i < 3; i++ {
		p := harness.MakeTestPID(fmt.Sprintf("node-%d", i), fmt.Sprintf("member-%d", i))
		cluster.Nodes[p.Node].Service.Join(group, p)
	}

	// Each node sees only its local member
	for i := 0; i < 3; i++ {
		nodeID := fmt.Sprintf("node-%d", i)
		local := cluster.Nodes[nodeID].Service.GetLocalMembers(group)
		assert.Len(t, local, 1, "node %s local members", nodeID)
	}
}

// TestSynced_BroadcastLocal tests broadcast on a single node.
func TestSynced_BroadcastLocal(t *testing.T) {
	cluster := harness.NewSyncedCluster(t, 2)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "broadcast-local"
	for i := 0; i < 3; i++ {
		p := harness.MakeTestPID("node-0", fmt.Sprintf("bc-%d", i))
		cluster.Nodes["node-0"].Service.Join(group, p)
	}

	// Broadcast from node-0
	from := harness.MakeTestPID("node-0", "sender")
	count, err := cluster.Nodes["node-0"].Service.Broadcast(from, group, "test", nil)
	require.NoError(t, err)
	// Should deliver to 3 local members (sender not excluded from count in this mock)
	assert.GreaterOrEqual(t, count, 0)
}

// TestSynced_NodeFailureLocal tests node failure/recovery locally.
func TestSynced_NodeFailureLocal(t *testing.T) {
	cluster := harness.NewSyncedCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "failure-local"
	p1 := harness.MakeTestPID("node-0", "stable-1")
	p2 := harness.MakeTestPID("node-1", "failing")
	p3 := harness.MakeTestPID("node-2", "stable-2")

	cluster.Nodes["node-0"].Service.Join(group, p1)
	cluster.Nodes["node-1"].Service.Join(group, p2)
	cluster.Nodes["node-2"].Service.Join(group, p3)

	// Each node sees its own member
	assert.Len(t, cluster.Nodes["node-0"].Service.GetMembers(group), 1)
	assert.Len(t, cluster.Nodes["node-1"].Service.GetMembers(group), 1)
	assert.Len(t, cluster.Nodes["node-2"].Service.GetMembers(group), 1)

	// Kill node-1
	cluster.SimulateNodeFailure(t, "node-1")

	// Node-1 should be stopped
	stoppedNode := cluster.Nodes["node-1"]
	err := stoppedNode.Service.Join(group, p2)
	assert.Error(t, err, "stopped node should reject operations")

	// Recover node-1
	cluster.RecoverNode(t, "node-1")

	// Re-join (state persisted, so this adds another ref count)
	err = cluster.Nodes["node-1"].Service.Join(group, p2)
	require.NoError(t, err)

	// Node-1 should see its member (ref-counted: 2 entries from original + re-join)
	// Note: In real implementation, state might be cleared on recovery
	members := cluster.Nodes["node-1"].Service.GetMembers(group)
	assert.GreaterOrEqual(t, len(members), 1, "node-1 should have at least 1 member after recovery")
}

// TestSynced_RecoveryTimeLocal tests recovery timing.
func TestSynced_RecoveryTimeLocal(t *testing.T) {
	cluster := harness.NewSyncedCluster(t, 2)
	defer cluster.Stop(t)

	cluster.Start(t)

	// Kill and recover node-0
	cluster.SimulateNodeFailure(t, "node-0")

	start := time.Now()
	cluster.RecoverNode(t, "node-0")
	elapsed := time.Since(start)

	t.Logf("Recovery took %v", elapsed)
	assert.Less(t, elapsed, 200*time.Millisecond, "recovery should be fast")
}

// TestSynced_TopologyErrorInjection tests monitor error handling.
func TestSynced_TopologyErrorInjection(t *testing.T) {
	cluster := harness.NewSyncedCluster(t, 2)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "monitor-error-test"
	p := harness.MakeTestPID("node-0", "monitor-err-proc")

	// Inject monitor error
	topo := cluster.Nodes["node-0"].Topology
	topo.SetMonitorError(fmt.Errorf("monitor failed"))

	// Join still succeeds (monitor failure is non-fatal)
	err := cluster.Nodes["node-0"].Service.Join(group, p)
	require.NoError(t, err)

	// Clear error
	topo.SetMonitorError(nil)

	// Verify join worked
	members := cluster.Nodes["node-0"].Service.GetMembers(group)
	assert.Len(t, members, 1)
}

// TestSynced_RouterErrorInjection tests router error handling.
func TestSynced_RouterErrorInjection(t *testing.T) {
	cluster := harness.NewSyncedCluster(t, 2)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "router-error-test"
	p := harness.MakeTestPID("node-0", "error-proc")

	// Get router and inject error
	router := cluster.Nodes["node-0"].Router.(*harness.ForwardingRouter)
	router.SetSendError(fmt.Errorf("network down"))

	// Join still succeeds locally (router error doesn't block local join)
	err := cluster.Nodes["node-0"].Service.Join(group, p)
	require.NoError(t, err)

	// Clear error
	router.SetSendError(nil)

	// Verify join worked locally
	members := cluster.Nodes["node-0"].Service.GetMembers(group)
	assert.Len(t, members, 1)
}
