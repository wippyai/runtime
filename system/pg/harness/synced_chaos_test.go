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

// --- SyncedCluster Chaos Tests ---

// TestSyncedChaos_RapidNodeFailure tests rapid failure/recovery cycles.
func TestSyncedChaos_RapidNodeFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	cluster := harness.NewSyncedCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "resilient"

	// Add some members
	for i := 0; i < 6; i++ {
		nodeID := fmt.Sprintf("node-%d", i%3)
		p := harness.MakeTestPID(nodeID, fmt.Sprintf("stable-%d", i))
		cluster.Nodes[nodeID].Service.Join(group, p)
	}

	// Rapid failure/recovery cycles on node-1
	for i := 0; i < 5; i++ {
		cluster.SimulateNodeFailure(t, "node-1")
		time.Sleep(10 * time.Millisecond)
		cluster.RecoverNode(t, "node-1")
		time.Sleep(10 * time.Millisecond)
	}

	// Verify other nodes still work
	for _, node := range cluster.Nodes {
		if node.ID != "node-1" {
			err := node.Service.Join(group, harness.MakeTestPID(node.ID, fmt.Sprintf("post-chaos-%s", node.ID)))
			assert.NoError(t, err)
		}
	}
}

// TestSyncedChaos_ConcurrentNodeOperations tests concurrent operations during chaos.
func TestSyncedChaos_ConcurrentNodeOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	cluster := harness.NewSyncedCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "chaos-concurrent"
	var wg sync.WaitGroup

	// First do concurrent joiners
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			nodeID := fmt.Sprintf("node-%d", idx%3)
			p := harness.MakeTestPID(nodeID, fmt.Sprintf("cc-%d", idx))
			_ = cluster.Nodes[nodeID].Service.Join(group, p)
		}(i)
	}

	wg.Wait()

	// Then do failure/recovery
	cluster.SimulateNodeFailure(t, "node-1")
	time.Sleep(50 * time.Millisecond)
	cluster.RecoverNode(t, "node-1")

	// Verify cluster still functional
	for _, node := range cluster.Nodes {
		err := node.Service.Join(group, harness.MakeTestPID(node.ID, "post-chaos"))
		assert.NoError(t, err)
	}
}

// TestSyncedChaos_PartialNodeFailure tests with only some nodes failing.
func TestSyncedChaos_PartialNodeFailure(t *testing.T) {
	cluster := harness.NewSyncedCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "partial-fail"

	// Add members to all nodes
	for i := 0; i < 6; i++ {
		nodeID := fmt.Sprintf("node-%d", i%3)
		p := harness.MakeTestPID(nodeID, fmt.Sprintf("member-%d", i))
		cluster.Nodes[nodeID].Service.Join(group, p)
	}

	// Kill only node-1
	cluster.SimulateNodeFailure(t, "node-1")

	// Continue operations on healthy nodes
	for _, node := range cluster.Nodes {
		if node.ID != "node-1" {
			err := node.Service.Join(group, harness.MakeTestPID(node.ID, "during-failure"))
			require.NoError(t, err)
		}
	}

	// Recover
	cluster.RecoverNode(t, "node-1")

	// Node-1 should work again
	err := cluster.Nodes["node-1"].Service.Join(group, harness.MakeTestPID("node-1", "after-recovery"))
	require.NoError(t, err)
}

// TestSyncedChaos_CascadeFailure tests cascading node failures.
func TestSyncedChaos_CascadeFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	cluster := harness.NewSyncedCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "cascade"

	// Add members
	for i := 0; i < 3; i++ {
		nodeID := fmt.Sprintf("node-%d", i)
		p := harness.MakeTestPID(nodeID, fmt.Sprintf("cascade-%d", i))
		cluster.Nodes[nodeID].Service.Join(group, p)
	}

	// Cascade: fail node-0, then node-1
	cluster.SimulateNodeFailure(t, "node-0")
	time.Sleep(50 * time.Millisecond)
	cluster.SimulateNodeFailure(t, "node-1")

	// Only node-2 should be operational
	err := cluster.Nodes["node-2"].Service.Join(group, harness.MakeTestPID("node-2", "lone-survivor"))
	require.NoError(t, err)

	// Recover in reverse order
	cluster.RecoverNode(t, "node-1")
	time.Sleep(50 * time.Millisecond)
	cluster.RecoverNode(t, "node-0")

	// All should work now
	for _, node := range cluster.Nodes {
		err := node.Service.Join(group, harness.MakeTestPID(node.ID, "post-cascade"))
		assert.NoError(t, err)
	}
}

// --- SyncedCluster Stress Tests ---

// TestSyncedStress_HighVolumeJoins tests high volume of joins.
func TestSyncedStress_HighVolumeJoins(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	cluster := harness.NewSyncedCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "high-volume"
	numJoins := 100

	start := time.Now()
	for i := 0; i < numJoins; i++ {
		nodeID := fmt.Sprintf("node-%d", i%3)
		p := harness.MakeTestPID(nodeID, fmt.Sprintf("hv-%d", i))
		err := cluster.Nodes[nodeID].Service.Join(group, p)
		require.NoError(t, err)
	}
	elapsed := time.Since(start)

	t.Logf("Joined %d processes in %v (%.0f joins/sec)", numJoins, elapsed, float64(numJoins)/elapsed.Seconds())

	// Verify all joined
	total := 0
	for _, node := range cluster.Nodes {
		total += len(node.Service.GetLocalMembers(group))
	}
	assert.Equal(t, numJoins, total)
}

// TestSyncedStress_SustainedLoad tests sustained load over time.
func TestSyncedStress_SustainedLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	cluster := harness.NewSyncedCluster(t, 2)
	defer cluster.Stop(t)

	cluster.Start(t)

	duration := 500 * time.Millisecond
	start := time.Now()
	iterations := 0

	for time.Since(start) < duration {
		nodeID := fmt.Sprintf("node-%d", iterations%2)
		group := fmt.Sprintf("sustained-%d", iterations%5)
		p := harness.MakeTestPID(nodeID, fmt.Sprintf("sus-%d", iterations))

		err := cluster.Nodes[nodeID].Service.Join(group, p)
		if err == nil {
			iterations++
		}

		// Every 10th, do a leave
		if iterations%10 == 0 && iterations > 0 {
			leaveIdx := iterations - 5
			leaveNodeID := fmt.Sprintf("node-%d", leaveIdx%2)
			leaveGroup := fmt.Sprintf("sustained-%d", leaveIdx%5)
			leaveP := harness.MakeTestPID(leaveNodeID, fmt.Sprintf("sus-%d", leaveIdx))
			_ = cluster.Nodes[leaveNodeID].Service.Leave(leaveGroup, leaveP)
		}
	}

	t.Logf("Completed %d operations in %v", iterations, time.Since(start))
	assert.Greater(t, iterations, 100, "should complete many operations")
}

// TestSyncedStress_MemoryPattern tests memory usage patterns.
func TestSyncedStress_MemoryPattern(t *testing.T) {
	cluster := harness.NewSyncedCluster(t, 2)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "memory-test"
	p := harness.MakeTestPID("node-0", "memory-proc")

	// Join and leave same process repeatedly
	for i := 0; i < 50; i++ {
		err := cluster.Nodes["node-0"].Service.Join(group, p)
		require.NoError(t, err)

		err = cluster.Nodes["node-0"].Service.Leave(group, p)
		require.NoError(t, err)
	}

	// Final state should be empty
	members := cluster.Nodes["node-0"].Service.GetMembers(group)
	assert.Len(t, members, 0, "final state should be empty after balanced join/leave")
}

// TestSyncedStress_ConcurrentGroups tests many groups simultaneously.
func TestSyncedStress_ConcurrentGroups(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	cluster := harness.NewSyncedCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	numGroups := 20
	var wg sync.WaitGroup

	// Concurrent group operations
	for g := 0; g < numGroups; g++ {
		wg.Add(1)
		go func(groupIdx int) {
			defer wg.Done()
			group := fmt.Sprintf("concurrent-group-%d", groupIdx)

			// Join from each node
			for n := 0; n < 3; n++ {
				nodeID := fmt.Sprintf("node-%d", n)
				p := harness.MakeTestPID(nodeID, fmt.Sprintf("g%d-n%d", groupIdx, n))
				err := cluster.Nodes[nodeID].Service.Join(group, p)
				assert.NoError(t, err)
			}
		}(g)
	}

	wg.Wait()

	// Verify all groups have members
	for g := 0; g < numGroups; g++ {
		group := fmt.Sprintf("concurrent-group-%d", g)
		totalMembers := 0
		for _, node := range cluster.Nodes {
			totalMembers += len(node.Service.GetLocalMembers(group))
		}
		assert.Equal(t, 3, totalMembers, "group %s should have 3 members", group)
	}
}

// TestSyncedStress_BurstOperations tests burst of operations.
func TestSyncedStress_BurstOperations(t *testing.T) {
	cluster := harness.NewSyncedCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "burst"

	// Burst join 50 processes
	start := time.Now()
	for i := 0; i < 50; i++ {
		nodeID := fmt.Sprintf("node-%d", i%3)
		p := harness.MakeTestPID(nodeID, fmt.Sprintf("burst-%d", i))
		cluster.Nodes[nodeID].Service.Join(group, p)
	}
	joinTime := time.Since(start)

	// Burst leave all
	start = time.Now()
	for i := 0; i < 50; i++ {
		nodeID := fmt.Sprintf("node-%d", i%3)
		p := harness.MakeTestPID(nodeID, fmt.Sprintf("burst-%d", i))
		_ = cluster.Nodes[nodeID].Service.Leave(group, p)
	}
	leaveTime := time.Since(start)

	t.Logf("Burst: 50 joins in %v, 50 leaves in %v", joinTime, leaveTime)

	// Verify empty
	total := 0
	for _, node := range cluster.Nodes {
		total += len(node.Service.GetLocalMembers(group))
	}
	assert.Equal(t, 0, total)
}
