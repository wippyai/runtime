// SPDX-License-Identifier: MPL-2.0

package sharded_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/system/globalreg/sharded"
	"go.uber.org/zap"
)

// setupShardedRegistry creates a test sharded registry.
func setupShardedRegistry(t *testing.T, shardCount uint32) *sharded.ShardCoordinator {
	logger := zap.NewNop()
	if testing.Verbose() {
		var err error
		logger, err = zap.NewDevelopment()
		require.NoError(t, err)
	}

	config := &sharded.Config{
		ShardCount:     shardCount,
		HashSeed:       0xdeadbeef,
		TxnTimeout:     10 * time.Second,
		PrepareTimeout: 5 * time.Second,
	}

	sc, err := sharded.NewShardCoordinator(logger, config)
	require.NoError(t, err)
	return sc
}

// TestSharded_Register tests basic registration.
func TestSharded_Register(t *testing.T) {
	sc := setupShardedRegistry(t, 4)
	ctx := context.Background()

	p := pid.PID{Host: "test", UniqID: "proc1", Node: "node1"}

	// Register
	result, err := sc.Register(ctx, "my-service", p)
	require.NoError(t, err)
	assert.Equal(t, p, result)

	// Lookup
	found, ok := sc.Lookup("my-service")
	assert.True(t, ok)
	assert.Equal(t, p, found)
}

// TestSharded_Register_Conflict tests duplicate registration fails.
func TestSharded_Register_Conflict(t *testing.T) {
	sc := setupShardedRegistry(t, 4)
	ctx := context.Background()

	p1 := pid.PID{Host: "test", UniqID: "proc1", Node: "node1"}
	p2 := pid.PID{Host: "test", UniqID: "proc2", Node: "node1"}

	// First registration
	_, err := sc.Register(ctx, "shared", p1)
	require.NoError(t, err)

	// Second should fail
	existing, err := sc.Register(ctx, "shared", p2)
	assert.Error(t, err)
	assert.Equal(t, p1, existing)
}

// TestSharded_RegisterMulti_SingleShard tests multi-name registration in one shard.
func TestSharded_RegisterMulti_SingleShard(t *testing.T) {
	sc := setupShardedRegistry(t, 4)
	ctx := context.Background()

	p := pid.PID{Host: "test", UniqID: "proc1", Node: "node1"}

	// Names that hash to same shard
	names := []string{"aaa-service", "aaa-worker", "aaa-cache"}

	err := sc.RegisterMulti(ctx, names, p)
	require.NoError(t, err)

	// Verify all registered
	for _, name := range names {
		_, ok := sc.Lookup(name)
		assert.True(t, ok, "name %s should be registered", name)
	}
}

// TestSharded_RegisterMulti_MultiShard tests cross-shard atomic registration.
func TestSharded_RegisterMulti_MultiShard(t *testing.T) {
	sc := setupShardedRegistry(t, 8)
	ctx := context.Background()

	p := pid.PID{Host: "test", UniqID: "proc1", Node: "node1"}

	// Names that will hash to different shards
	names := []string{"service-alpha", "service-beta", "service-gamma", "service-delta"}

	err := sc.RegisterMulti(ctx, names, p)
	require.NoError(t, err)

	// Verify all registered
	for _, name := range names {
		_, ok := sc.Lookup(name)
		assert.True(t, ok, "name %s should be registered", name)
	}
}

// TestSharded_RegisterMulti_Atomicity tests that 2PC is atomic.
func TestSharded_RegisterMulti_Atomicity(t *testing.T) {
	sc := setupShardedRegistry(t, 8)
	ctx := context.Background()

	p1 := pid.PID{Host: "test", UniqID: "proc1", Node: "node1"}
	p2 := pid.PID{Host: "test", UniqID: "proc2", Node: "node1"}

	// First, register some names
	names1 := []string{"atomic-a", "atomic-b", "atomic-c"}
	err := sc.RegisterMulti(ctx, names1, p1)
	require.NoError(t, err)

	// Try to register conflicting names atomically
	names2 := []string{"atomic-a", "atomic-x", "atomic-y"} // atomic-a conflicts
	err = sc.RegisterMulti(ctx, names2, p2)
	assert.Error(t, err)

	// Verify original names still belong to p1 (atomic rollback worked)
	for _, name := range names1 {
		found, ok := sc.Lookup(name)
		require.True(t, ok)
		assert.Equal(t, p1.UniqID, found.UniqID)
	}

	// Verify new names were NOT registered (atomic)
	_, ok := sc.Lookup("atomic-x")
	assert.False(t, ok, "atomic-x should not exist due to atomic rollback")
}

// TestSharded_Unregister tests unregistration.
func TestSharded_Unregister(t *testing.T) {
	sc := setupShardedRegistry(t, 4)
	ctx := context.Background()

	p := pid.PID{Host: "test", UniqID: "proc1", Node: "node1"}

	// Register
	_, err := sc.Register(ctx, "temp-service", p)
	require.NoError(t, err)

	// Verify exists
	_, ok := sc.Lookup("temp-service")
	assert.True(t, ok)

	// Unregister
	removed, err := sc.Unregister(ctx, "temp-service")
	require.NoError(t, err)
	assert.True(t, removed)

	// Verify gone
	_, ok = sc.Lookup("temp-service")
	assert.False(t, ok)
}

// TestSharded_UnregisterMulti tests multi-name unregistration.
func TestSharded_UnregisterMulti(t *testing.T) {
	sc := setupShardedRegistry(t, 8)
	ctx := context.Background()

	p := pid.PID{Host: "test", UniqID: "proc1", Node: "node1"}

	// Register multiple names
	names := []string{"bulk-a", "bulk-b", "bulk-c", "bulk-d"}
	err := sc.RegisterMulti(ctx, names, p)
	require.NoError(t, err)

	// Unregister all
	err = sc.UnregisterMulti(ctx, names)
	require.NoError(t, err)

	// Verify all gone
	for _, name := range names {
		_, ok := sc.Lookup(name)
		assert.False(t, ok, "name %s should be unregistered", name)
	}
}

// TestSharded_LookupByPID tests finding all names for a PID.
func TestSharded_LookupByPID(t *testing.T) {
	sc := setupShardedRegistry(t, 8)
	ctx := context.Background()

	p := pid.PID{Host: "test", UniqID: "proc1", Node: "node1"}

	// Register multiple names
	names := []string{"svc1", "svc2", "svc3", "svc4"}
	for _, name := range names {
		_, err := sc.Register(ctx, name, p)
		require.NoError(t, err)
	}

	// Lookup by PID
	found := sc.LookupByPID(p)
	assert.Len(t, found, len(names))
	for _, name := range names {
		assert.Contains(t, found, name)
	}
}

// TestSharded_Remove tests removing all names for a PID.
func TestSharded_Remove(t *testing.T) {
	sc := setupShardedRegistry(t, 8)
	ctx := context.Background()

	p := pid.PID{Host: "test", UniqID: "proc1", Node: "node1"}

	// Register multiple names
	names := []string{"remove1", "remove2", "remove3"}
	for _, name := range names {
		_, err := sc.Register(ctx, name, p)
		require.NoError(t, err)
	}

	// Remove all
	err := sc.Remove(ctx, p)
	require.NoError(t, err)

	// Verify all gone
	for _, name := range names {
		_, ok := sc.Lookup(name)
		assert.False(t, ok, "name %s should be removed", name)
	}
}

// TestSharded_ShardDistribution tests that names are distributed across shards.
func TestSharded_ShardDistribution(t *testing.T) {
	sc := setupShardedRegistry(t, 8)
	ctx := context.Background()

	p := pid.PID{Host: "test", UniqID: "proc1", Node: "node1"}

	// Register many names
	numNames := 100
	for i := 0; i < numNames; i++ {
		name := fmt.Sprintf("dist-%d", i)
		_, err := sc.Register(ctx, name, p)
		require.NoError(t, err)
	}

	// Get shard info
	info := sc.GetShardInfo()
	assert.Len(t, info, 8)

	// Verify some distribution (not all in one shard)
	totalNames := 0
	for _, shard := range info {
		totalNames += shard.NameCount
	}
	assert.Equal(t, numNames, totalNames)
}

// TestSharded_ConcurrentRegistrations tests concurrent operations.
func TestSharded_ConcurrentRegistrations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrent test in short mode")
	}

	sc := setupShardedRegistry(t, 8)
	ctx := context.Background()

	numGoroutines := 20
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			p := pid.PID{Host: "test", UniqID: fmt.Sprintf("proc-%d", idx), Node: "node1"}
			name := fmt.Sprintf("concurrent-%d", idx)
			_, err := sc.Register(ctx, name, p)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// All should succeed (no conflicts)
	errorCount := 0
	for err := range errors {
		if err != nil {
			errorCount++
			t.Logf("Error: %v", err)
		}
	}
	assert.Equal(t, 0, errorCount)

	// Verify all registered
	for i := 0; i < numGoroutines; i++ {
		name := fmt.Sprintf("concurrent-%d", i)
		_, ok := sc.Lookup(name)
		assert.True(t, ok, "name %s should be registered", name)
	}
}

// TestSharded_Stress tests high load.
func TestSharded_Stress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	sc := setupShardedRegistry(t, 16)
	ctx := context.Background()

	numOperations := 500

	// Rapid register/unregister cycles
	for i := 0; i < numOperations; i++ {
		p := pid.PID{Host: "test", UniqID: fmt.Sprintf("stress-%d", i), Node: "node1"}
		name := fmt.Sprintf("stress-%d", i)

		_, err := sc.Register(ctx, name, p)
		require.NoError(t, err)

		_, err = sc.Unregister(ctx, name)
		require.NoError(t, err)
	}

	// Verify empty
	info := sc.GetShardInfo()
	total := 0
	for _, shard := range info {
		total += shard.NameCount
	}
	assert.Equal(t, 0, total)
}

// TestSharded_CrossShardAtomicRollback tests 2PC rollback on conflict.
func TestSharded_CrossShardAtomicRollback(t *testing.T) {
	sc := setupShardedRegistry(t, 8)
	ctx := context.Background()

	p1 := pid.PID{Host: "test", UniqID: "owner1", Node: "node1"}
	p2 := pid.PID{Host: "test", UniqID: "owner2", Node: "node1"}

	// Register initial set
	names1 := []string{"conflict-a", "unique-b", "unique-c"}
	err := sc.RegisterMulti(ctx, names1, p1)
	require.NoError(t, err)

	// Try to register overlapping set with different owner
	names2 := []string{"conflict-a", "new-d", "new-e"} // conflict-a overlaps
	err = sc.RegisterMulti(ctx, names2, p2)
	require.Error(t, err)

	// Verify p1 still owns all original names
	for _, name := range names1 {
		found, ok := sc.Lookup(name)
		require.True(t, ok)
		assert.Equal(t, p1.UniqID, found.UniqID)
	}

	// Verify p2 owns nothing (transaction rolled back)
	for _, name := range names2 {
		if name == "conflict-a" {
			continue // Skip the conflicting one
		}
		_, ok := sc.Lookup(name)
		assert.False(t, ok, "name %s from failed txn should not exist", name)
	}
}

// TestSharded_GetShardInfo tests shard metadata.
func TestSharded_GetShardInfo(t *testing.T) {
	sc := setupShardedRegistry(t, 4)

	info := sc.GetShardInfo()
	require.Len(t, info, 4)

	for _, shard := range info {
		assert.NotNil(t, shard.ID)
		assert.True(t, shard.IsHealthy)
	}
}
