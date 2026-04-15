// SPDX-License-Identifier: MPL-2.0

package harness_test

import (
	"fmt"
	"testing"

	"github.com/wippyai/runtime/system/pg/harness"
)

// BenchmarkJoin benchmarks group join operations.
func BenchmarkJoin(b *testing.B) {
	cluster := harness.NewTestCluster(b, 1)
	defer cluster.Stop(b)
	cluster.Start(b)

	node := cluster.Nodes["node-0"]
	group := "bench-join-group"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := harness.MakeTestPID("node-0", fmt.Sprintf("bench-%d", i))
		node.Service.Join(group, p)
	}
}

// BenchmarkLeave benchmarks group leave operations.
func BenchmarkLeave(b *testing.B) {
	cluster := harness.NewTestCluster(b, 1)
	defer cluster.Stop(b)
	cluster.Start(b)

	node := cluster.Nodes["node-0"]
	group := "bench-leave-group"

	// Pre-populate
	pids := make([]string, b.N)
	for i := 0; i < b.N; i++ {
		p := harness.MakeTestPID("node-0", fmt.Sprintf("bench-%d", i))
		pids[i] = p.String()
		node.Service.Join(group, p)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := harness.MakeTestPID("node-0", fmt.Sprintf("bench-%d", i))
		node.Service.Leave(group, p)
	}
}

// BenchmarkGetMembers benchmarks member retrieval.
func BenchmarkGetMembers(b *testing.B) {
	cluster := harness.NewTestCluster(b, 1)
	defer cluster.Stop(b)
	cluster.Start(b)

	node := cluster.Nodes["node-0"]
	group := "bench-get-group"

	// Add members
	for i := 0; i < 100; i++ {
		p := harness.MakeTestPID("node-0", fmt.Sprintf("member-%d", i))
		node.Service.Join(group, p)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = node.Service.GetMembers(group)
	}
}

// BenchmarkGetMembersLarge benchmarks member retrieval with many members.
func BenchmarkGetMembersLarge(b *testing.B) {
	cluster := harness.NewTestCluster(b, 3)
	defer cluster.Stop(b)
	cluster.Start(b)

	node := cluster.Nodes["node-0"]
	group := "bench-get-large"

	// Add many members
	for i := 0; i < 1000; i++ {
		nodeID := fmt.Sprintf("node-%d", i%3)
		p := harness.MakeTestPID(nodeID, fmt.Sprintf("member-%d", i))
		cluster.Nodes[nodeID].Service.Join(group, p)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = node.Service.GetMembers(group)
	}
}

// BenchmarkJoinParallel benchmarks concurrent joins.
func BenchmarkJoinParallel(b *testing.B) {
	cluster := harness.NewTestCluster(b, 3)
	defer cluster.Stop(b)
	cluster.Start(b)

	group := "bench-parallel-group"

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			nodeID := fmt.Sprintf("node-%d", i%3)
			p := harness.MakeTestPID(nodeID, fmt.Sprintf("parallel-%d", i))
			cluster.Nodes[nodeID].Service.Join(group, p)
			i++
		}
	})
}

// BenchmarkWhichGroups benchmarks group listing.
func BenchmarkWhichGroups(b *testing.B) {
	cluster := harness.NewTestCluster(b, 1)
	defer cluster.Stop(b)
	cluster.Start(b)

	node := cluster.Nodes["node-0"]
	p := harness.MakeTestPID("node-0", "list-proc")

	// Create many groups
	for i := 0; i < 100; i++ {
		group := fmt.Sprintf("group-%d", i)
		node.Service.Join(group, p)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = node.Service.WhichGroups()
	}
}

// BenchmarkJoinGroupsAtomic benchmarks atomic multi-group join.
func BenchmarkJoinGroupsAtomic(b *testing.B) {
	cluster := harness.NewTestCluster(b, 1)
	defer cluster.Stop(b)
	cluster.Start(b)

	node := cluster.Nodes["node-0"]
	groups := []string{"g1", "g2", "g3", "g4", "g5"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := harness.MakeTestPID("node-0", fmt.Sprintf("atomic-%d", i))
		node.Service.JoinGroups(groups, p)
	}
}

// BenchmarkBroadcast benchmarks broadcast operations.
func BenchmarkBroadcast(b *testing.B) {
	cluster := harness.NewTestCluster(b, 3)
	defer cluster.Stop(b)
	cluster.Start(b)

	group := "bench-broadcast"
	from := harness.MakeTestPID("node-0", "sender")

	// Add members
	for i := 0; i < 100; i++ {
		nodeID := fmt.Sprintf("node-%d", i%3)
		p := harness.MakeTestPID(nodeID, fmt.Sprintf("member-%d", i))
		cluster.Nodes[nodeID].Service.Join(group, p)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cluster.Nodes["node-0"].Service.Broadcast(from, group, "test", nil)
	}
}
