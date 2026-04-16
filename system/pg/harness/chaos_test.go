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

// TestChaos_NodeFailure tests recovery from node failure.
func TestChaos_NodeFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping chaos test in short mode")
	}

	cluster := harness.NewTestCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "resilient-group"

	// Add processes on all nodes
	for i := 0; i < 9; i++ {
		nodeID := fmt.Sprintf("node-%d", i%3)
		p := harness.MakeTestPID(nodeID, fmt.Sprintf("proc-%d", i))
		cluster.Nodes[nodeID].Service.Join(group, p)
	}

	// Verify total joined (local view on each node)
	totalJoined := 0
	for _, node := range cluster.Nodes {
		totalJoined += len(node.Service.GetLocalMembers(group))
	}
	assert.Equal(t, 9, totalJoined)

	// Simulate node-1 failure
	cluster.SimulateNodeFailure(t, "node-1")

	// In a real distributed system, other nodes would detect the failure
	// and clean up the failed node's processes. With our test harness,
	// we verify the node is stopped and remaining nodes are still functional.

	// Verify remaining nodes still work
	p := harness.MakeTestPID("node-0", "post-failure")
	err := cluster.Nodes["node-0"].Service.Join(group, p)
	require.NoError(t, err)
}

// TestChaos_RapidNodeFailures tests rapid succession of failures.
func TestChaos_RapidNodeFailures(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping chaos test in short mode")
	}

	cluster := harness.NewTestCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "rapid-fail-group"

	// Add processes
	for i := 0; i < 6; i++ {
		nodeID := fmt.Sprintf("node-%d", i%3)
		p := harness.MakeTestPID(nodeID, fmt.Sprintf("proc-%d", i))
		cluster.Nodes[nodeID].Service.Join(group, p)
	}

	// Rapid failure and recovery
	for cycle := 0; cycle < 3; cycle++ {
		// Fail node-1
		cluster.SimulateNodeFailure(t, "node-1")

		// Small delay
		time.Sleep(50 * time.Millisecond)

		// Recover
		cluster.RecoverNode(t, "node-1")

		// Verify operations still work
		p := harness.MakeTestPID("node-0", fmt.Sprintf("cycle-%d", cycle))
		err := cluster.Nodes["node-0"].Service.Join(group, p)
		require.NoError(t, err)
	}
}

// TestChaos_PartialPartition tests partial network partition.
func TestChaos_PartialPartition(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping chaos test in short mode")
	}

	cluster := harness.NewTestCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "partition-group"

	// Add processes on all nodes
	for i := 0; i < 6; i++ {
		nodeID := fmt.Sprintf("node-%d", i%3)
		p := harness.MakeTestPID(nodeID, fmt.Sprintf("proc-%d", i))
		cluster.Nodes[nodeID].Service.Join(group, p)
	}

	// Simulate partition by isolating node-1
	cluster.SimulateNodeFailure(t, "node-1")

	// Verify remaining connected nodes can still communicate
	p := harness.MakeTestPID("node-0", "partition-test")
	err := cluster.Nodes["node-0"].Service.Join(group, p)
	require.NoError(t, err)

	// Recover
	cluster.RecoverNode(t, "node-1")
}

// TestChaos_ConcurrentFailures tests concurrent node failures.
func TestChaos_ConcurrentFailures(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping chaos test in short mode")
	}

	cluster := harness.NewTestCluster(t, 5)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "concurrent-fail-group"

	// Add processes
	for i := 0; i < 10; i++ {
		nodeID := fmt.Sprintf("node-%d", i%5)
		p := harness.MakeTestPID(nodeID, fmt.Sprintf("proc-%d", i))
		cluster.Nodes[nodeID].Service.Join(group, p)
	}

	// Concurrent failures
	var wg sync.WaitGroup
	for _, nodeID := range []string{"node-1", "node-2", "node-3"} {
		wg.Add(1)
		go func(nid string) {
			defer wg.Done()
			cluster.SimulateNodeFailure(t, nid)
		}(nodeID)
	}
	wg.Wait()

	// Verify remaining nodes still work
	p := harness.MakeTestPID("node-0", "survivor")
	err := cluster.Nodes["node-0"].Service.Join(group, p)
	require.NoError(t, err)
}

// TestChaos_CascadeFailure tests cascade of failures.
func TestChaos_CascadeFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping chaos test in short mode")
	}

	cluster := harness.NewTestCluster(t, 4)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "cascade-group"

	// Add processes
	for i := 0; i < 8; i++ {
		nodeID := fmt.Sprintf("node-%d", i%4)
		p := harness.MakeTestPID(nodeID, fmt.Sprintf("proc-%d", i))
		cluster.Nodes[nodeID].Service.Join(group, p)
	}

	// Cascade failures
	for i := 1; i < 4; i++ {
		nodeID := fmt.Sprintf("node-%d", i)
		cluster.SimulateNodeFailure(t, nodeID)
		time.Sleep(100 * time.Millisecond)
	}

	// Last node should still be functional
	p := harness.MakeTestPID("node-0", "last-standing")
	err := cluster.Nodes["node-0"].Service.Join(group, p)
	require.NoError(t, err)
}

// TestChaos_JoinDuringFailure tests joining during node failure.
func TestChaos_JoinDuringFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping chaos test in short mode")
	}

	cluster := harness.NewTestCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "during-fail-group"

	// Start join operations
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			nodeID := fmt.Sprintf("node-%d", idx%3)
			p := harness.MakeTestPID(nodeID, fmt.Sprintf("concurrent-%d", idx))
			cluster.Nodes[nodeID].Service.Join(group, p)
		}(i)
	}

	// Fail a node during joins
	time.Sleep(10 * time.Millisecond)
	cluster.SimulateNodeFailure(t, "node-1")

	wg.Wait()

	// Verify some joins succeeded on remaining nodes
	node0Count := len(cluster.Nodes["node-0"].Service.GetLocalMembers(group))
	assert.Greater(t, node0Count, 0, "some joins should have succeeded on node-0")
}

// TestChaos_LeaveDuringFailure tests leaving during node failure.
func TestChaos_LeaveDuringFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping chaos test in short mode")
	}

	cluster := harness.NewTestCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "leave-fail-group"

	// Pre-populate
	for i := 0; i < 10; i++ {
		nodeID := fmt.Sprintf("node-%d", i%3)
		p := harness.MakeTestPID(nodeID, fmt.Sprintf("proc-%d", i))
		cluster.Nodes[nodeID].Service.Join(group, p)
	}

	// Start leave operations
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			nodeID := fmt.Sprintf("node-%d", idx%3)
			p := harness.MakeTestPID(nodeID, fmt.Sprintf("proc-%d", idx))
			cluster.Nodes[nodeID].Service.Leave(group, p)
		}(i)
	}

	// Fail a node during leaves
	time.Sleep(10 * time.Millisecond)
	cluster.SimulateNodeFailure(t, "node-1")

	wg.Wait()
}

// TestChaos_RecoveryTime tests recovery time after failure.
func TestChaos_RecoveryTime(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping chaos test in short mode")
	}

	cluster := harness.NewTestCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "recovery-time-group"

	// Add processes
	for i := 0; i < 6; i++ {
		nodeID := fmt.Sprintf("node-%d", i%3)
		p := harness.MakeTestPID(nodeID, fmt.Sprintf("proc-%d", i))
		cluster.Nodes[nodeID].Service.Join(group, p)
	}

	// Measure recovery time
	start := time.Now()

	cluster.SimulateNodeFailure(t, "node-1")
	time.Sleep(100 * time.Millisecond)
	cluster.RecoverNode(t, "node-1")

	// Perform operation after recovery
	p := harness.MakeTestPID("node-0", "post-recovery")
	err := cluster.Nodes["node-0"].Service.Join(group, p)
	require.NoError(t, err)

	elapsed := time.Since(start)
	t.Logf("Recovery took %v", elapsed)
}

// TestChaos_MembershipDuringInstability tests membership during instability.
func TestChaos_MembershipDuringInstability(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping chaos test in short mode")
	}

	cluster := harness.NewTestCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "instability-group"

	// Add initial members
	for i := 0; i < 6; i++ {
		nodeID := fmt.Sprintf("node-%d", i%3)
		p := harness.MakeTestPID(nodeID, fmt.Sprintf("initial-%d", i))
		cluster.Nodes[nodeID].Service.Join(group, p)
	}

	// Count initial members on stable nodes
	initialNode0Members := len(cluster.Nodes["node-0"].Service.GetLocalMembers(group))
	initialNode2Members := len(cluster.Nodes["node-2"].Service.GetLocalMembers(group))

	// Cause instability
	for i := 0; i < 5; i++ {
		cluster.SimulateNodeFailure(t, "node-1")
		time.Sleep(50 * time.Millisecond)
		cluster.RecoverNode(t, "node-1")
		time.Sleep(50 * time.Millisecond)
	}

	// Verify membership queries still work on stable nodes
	node0Members := len(cluster.Nodes["node-0"].Service.GetLocalMembers(group))
	node2Members := len(cluster.Nodes["node-2"].Service.GetLocalMembers(group))

	// Stable nodes should retain their members
	assert.Equal(t, initialNode0Members, node0Members, "node-0 should retain its members")
	assert.Equal(t, initialNode2Members, node2Members, "node-2 should retain its members")
}

// TestChaos_BroadcastDuringFailure tests broadcasting during failure.
func TestChaos_BroadcastDuringFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping chaos test in short mode")
	}

	cluster := harness.NewTestCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "broadcast-fail-group"

	// Add members
	for i := 0; i < 6; i++ {
		nodeID := fmt.Sprintf("node-%d", i%3)
		p := harness.MakeTestPID(nodeID, fmt.Sprintf("member-%d", i))
		cluster.Nodes[nodeID].Service.Join(group, p)
	}

	// Start broadcasts
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			from := harness.MakeTestPID("node-0", fmt.Sprintf("sender-%d", idx))
			cluster.Nodes["node-0"].Service.Broadcast(from, group, "test", nil)
		}(i)
	}

	// Fail node during broadcasts
	time.Sleep(10 * time.Millisecond)
	cluster.SimulateNodeFailure(t, "node-1")

	wg.Wait()
}
