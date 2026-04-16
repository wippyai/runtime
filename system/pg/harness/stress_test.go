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

// TestStress_HundredProcesses tests 100 processes across nodes (local view only).
func TestStress_HundredProcesses(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	cluster := harness.NewTestCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "hundred-group"
	numProcesses := 100

	// Join 100 processes
	var wg sync.WaitGroup
	errors := make(chan error, numProcesses)

	for i := 0; i < numProcesses; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			nodeID := fmt.Sprintf("node-%d", idx%3)
			p := harness.MakeTestPID(nodeID, fmt.Sprintf("stress-%03d", idx))
			if err := cluster.Nodes[nodeID].Service.Join(group, p); err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		require.NoError(t, err)
	}

	// Verify all joined (count local members on each node)
	totalMembers := 0
	for _, node := range cluster.Nodes {
		totalMembers += len(node.Service.GetLocalMembers(group))
	}
	assert.Equal(t, numProcesses, totalMembers)

	// Cleanup
	for i := 0; i < numProcesses; i++ {
		nodeID := fmt.Sprintf("node-%d", i%3)
		p := harness.MakeTestPID(nodeID, fmt.Sprintf("stress-%03d", i))
		cluster.Nodes[nodeID].Service.Leave(group, p)
	}

	// Verify all left
	totalRemaining := 0
	for _, node := range cluster.Nodes {
		totalRemaining += len(node.Service.GetLocalMembers(group))
	}
	assert.Equal(t, 0, totalRemaining)
}

// TestStress_MultipleGroups tests many groups simultaneously.
func TestStress_MultipleGroups(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	cluster := harness.NewTestCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	numGroups := 100
	processesPerGroup := 10

	// Create many groups
	var wg sync.WaitGroup
	for g := 0; g < numGroups; g++ {
		groupName := fmt.Sprintf("stress-group-%03d", g)
		for p := 0; p < processesPerGroup; p++ {
			wg.Add(1)
			go func(groupIdx, procIdx int, gName string) {
				defer wg.Done()
				nodeID := fmt.Sprintf("node-%d", (groupIdx+procIdx)%3)
				p := harness.MakeTestPID(nodeID, fmt.Sprintf("g%d-p%d", groupIdx, procIdx))
				cluster.Nodes[nodeID].Service.Join(gName, p)
			}(g, p, groupName)
		}
	}

	wg.Wait()

	// Verify all groups have correct size (locally on each node)
	for g := 0; g < numGroups; g++ {
		group := fmt.Sprintf("stress-group-%03d", g)
		totalMembers := 0
		for _, node := range cluster.Nodes {
			totalMembers += len(node.Service.GetLocalMembers(group))
		}
		assert.Equal(t, processesPerGroup, totalMembers, "group %s", group)
	}
}

// TestStress_RapidOperations tests rapid concurrent operations.
func TestStress_RapidOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	cluster := harness.NewTestCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "rapid-group"
	numOperations := 500

	var wg sync.WaitGroup
	for i := 0; i < numOperations; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			nodeID := fmt.Sprintf("node-%d", idx%3)
			p := harness.MakeTestPID(nodeID, fmt.Sprintf("rapid-%d", idx))

			// Join
			err := cluster.Nodes[nodeID].Service.Join(group, p)
			require.NoError(t, err)

			// Small delay
			time.Sleep(time.Millisecond)

			// Leave
			err = cluster.Nodes[nodeID].Service.Leave(group, p)
			require.NoError(t, err)
		}(i)
	}

	wg.Wait()

	// Group should be empty (or have few members due to timing)
	totalMembers := 0
	for _, node := range cluster.Nodes {
		totalMembers += len(node.Service.GetLocalMembers(group))
	}
	assert.Less(t, totalMembers, 10)
}

// TestStress_LongRunningGroup tests a long-running group with many changes.
func TestStress_LongRunningGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	cluster := harness.NewTestCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "long-running"
	cycles := 50
	processesPerCycle := 20

	for cycle := 0; cycle < cycles; cycle++ {
		var wg sync.WaitGroup

		// Join phase
		for i := 0; i < processesPerCycle; i++ {
			wg.Add(1)
			go func(c, p int) {
				defer wg.Done()
				nodeID := fmt.Sprintf("node-%d", (c+p)%3)
				pid := harness.MakeTestPID(nodeID, fmt.Sprintf("cycle%d-proc%d", c, p))
				cluster.Nodes[nodeID].Service.Join(group, pid)
			}(cycle, i)
		}
		wg.Wait()

		// Verify (count on all nodes)
		totalSize := 0
		for _, node := range cluster.Nodes {
			totalSize += len(node.Service.GetLocalMembers(group))
		}
		require.Equal(t, processesPerCycle, totalSize, "cycle %d", cycle)

		// Leave phase
		for i := 0; i < processesPerCycle; i++ {
			wg.Add(1)
			go func(c, p int) {
				defer wg.Done()
				nodeID := fmt.Sprintf("node-%d", (c+p)%3)
				pid := harness.MakeTestPID(nodeID, fmt.Sprintf("cycle%d-proc%d", c, p))
				cluster.Nodes[nodeID].Service.Leave(group, pid)
			}(cycle, i)
		}
		wg.Wait()
	}

	// Final verify
	totalRemaining := 0
	for _, node := range cluster.Nodes {
		totalRemaining += len(node.Service.GetLocalMembers(group))
	}
	assert.Equal(t, 0, totalRemaining)
}

// TestStress_MixedOperations tests mixed join/leave concurrently.
func TestStress_MixedOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	cluster := harness.NewTestCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	groups := []string{"mix-a", "mix-b", "mix-c"}
	numOperations := 300

	var wg sync.WaitGroup
	for i := 0; i < numOperations; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			nodeID := fmt.Sprintf("node-%d", idx%3)
			group := groups[idx%3]
			p := harness.MakeTestPID(nodeID, fmt.Sprintf("mix-%d", idx))

			// Random operation
			if idx%2 == 0 {
				cluster.Nodes[nodeID].Service.Join(group, p)
			} else {
				// Best effort leave
				_ = cluster.Nodes[nodeID].Service.Leave(group, p)
			}
		}(i)
	}

	wg.Wait()

	// Wait for event loop to settle
	time.Sleep(100 * time.Millisecond)

	// Verify each group has consistent local view
	for _, group := range groups {
		totalMembers := 0
		for _, node := range cluster.Nodes {
			totalMembers += len(node.Service.GetLocalMembers(group))
		}
		// Should have approximately half of operations (joins minus leaves)
		assert.Greater(t, totalMembers, 0, "group %s should have members", group)
	}
}

// TestStress_MemoryStability tests memory stability over many operations.
func TestStress_MemoryStability(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	cluster := harness.NewTestCluster(t, 2)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "memory-test"
	iterations := 1000

	// Repeated join/leave cycles
	for i := 0; i < iterations; i++ {
		p := harness.MakeTestPID("node-0", fmt.Sprintf("mem-%d", i))

		err := cluster.Nodes["node-0"].Service.Join(group, p)
		require.NoError(t, err)

		err = cluster.Nodes["node-0"].Service.Leave(group, p)
		require.NoError(t, err)
	}

	// Should be empty
	members := cluster.Nodes["node-0"].Service.GetMembers(group)
	assert.Len(t, members, 0)
}

// TestStress_BurstTraffic tests burst of traffic.
func TestStress_BurstTraffic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	cluster := harness.NewTestCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	group := "burst-group"
	numProcesses := 50

	// Join many processes
	for i := 0; i < numProcesses; i++ {
		nodeID := fmt.Sprintf("node-%d", i%3)
		p := harness.MakeTestPID(nodeID, fmt.Sprintf("burst-%d", i))
		cluster.Nodes[nodeID].Service.Join(group, p)
	}

	// Rapid broadcast burst
	from := harness.MakeTestPID("node-0", "sender")
	for i := 0; i < 100; i++ {
		_, err := cluster.Nodes["node-0"].Service.Broadcast(from, group, "burst", nil)
		require.NoError(t, err)
	}
}

// TestStress_ConcurrentGroupCreation tests concurrent group creation.
func TestStress_ConcurrentGroupCreation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	cluster := harness.NewTestCluster(t, 3)
	defer cluster.Stop(t)

	cluster.Start(t)

	numGroups := 200
	var wg sync.WaitGroup

	for i := 0; i < numGroups; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			group := fmt.Sprintf("concurrent-%d", idx)
			nodeID := fmt.Sprintf("node-%d", idx%3)
			p := harness.MakeTestPID(nodeID, fmt.Sprintf("creator-%d", idx))
			cluster.Nodes[nodeID].Service.Join(group, p)
		}(i)
	}

	wg.Wait()

	// Wait for event loop to settle
	time.Sleep(100 * time.Millisecond)

	// Verify all groups exist (count across all nodes)
	for i := 0; i < numGroups; i++ {
		group := fmt.Sprintf("concurrent-%d", i)
		totalMembers := 0
		for _, node := range cluster.Nodes {
			totalMembers += len(node.Service.GetLocalMembers(group))
		}
		assert.Equal(t, 1, totalMembers, "group %s", group)
	}
}
