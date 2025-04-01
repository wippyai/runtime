package memstore_test

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/service/memstore"
	"sync"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	memcfg "github.com/ponyruntime/pony/api/service/memstore"
	"github.com/ponyruntime/pony/api/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// createTestStore is a helper function that creates a memory store with default test configuration
func createTestStore(t *testing.T) *memstore.MemoryStore {
	logger := zap.NewNop()
	config := &memcfg.MemoryConfig{
		MaxSize:         100,
		CleanupInterval: 50 * time.Millisecond,
	}
	return memstore.NewMemoryStore(registry.ID{NS: "test", Name: "store"}, config, logger)
}

// createTestEntry is a helper function to create a store entry with the given key and value
func createTestEntry(key string, value any) store.Entry {
	return store.Entry{
		Key:   registry.ParseID(key),
		Value: payload.New(value),
	}
}

// createTestEntryWithTTL is a helper function to create a store entry with TTL
func createTestEntryWithTTL(key string, value any, ttl time.Duration) store.Entry {
	return store.Entry{
		Key:   registry.ParseID(key),
		Value: payload.New(value),
		TTL:   ttl,
	}
}

// TestMemoryStore_Get tests the Get functionality
func TestMemoryStore_Get(t *testing.T) {
	ms := createTestStore(t)
	ctx := context.Background()

	// Set a test value
	testKey := registry.ParseID("test:key1")
	testValue := "test value"
	err := ms.Set(ctx, createTestEntry("test:key1", testValue))
	require.NoError(t, err)

	// Test retrieving the value
	result, err := ms.Get(ctx, testKey)
	require.NoError(t, err)
	assert.Equal(t, testValue, result.Data())

	// Test retrieving a non-existent value
	_, err = ms.Get(ctx, registry.ParseID("test:nonexistent"))
	assert.Equal(t, store.ErrKeyNotFound, err)

	// Test expiration
	shortTTLKey := registry.ParseID("test:expiring")
	err = ms.Set(ctx, createTestEntryWithTTL("test:expiring", "will expire", 50*time.Millisecond))
	require.NoError(t, err)

	// Initially the value should exist
	result, err = ms.Get(ctx, shortTTLKey)
	require.NoError(t, err)
	assert.Equal(t, "will expire", result.Data())

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// After expiration the value should not exist
	_, err = ms.Get(ctx, shortTTLKey)
	assert.Equal(t, store.ErrKeyNotFound, err)

	// Test closed store
	err = ms.Stop(ctx)
	require.NoError(t, err)

	_, err = ms.Get(ctx, testKey)
	assert.Equal(t, store.ErrStoreClosed, err)
}

// TestMemoryStore_Set tests the Set functionality
func TestMemoryStore_Set(t *testing.T) {
	ms := createTestStore(t)
	ctx := context.Background()

	// Test setting a new value
	testKey := registry.ParseID("test:key1")
	testValue := "test value"
	err := ms.Set(ctx, createTestEntry("test:key1", testValue))
	require.NoError(t, err)

	// Verify it was set
	exists, err := ms.Has(ctx, testKey)
	require.NoError(t, err)
	assert.True(t, exists)

	// Test updating an existing value
	updatedValue := "updated value"
	err = ms.Set(ctx, createTestEntry("test:key1", updatedValue))
	require.NoError(t, err)

	// Verify it was updated
	result, err := ms.Get(ctx, testKey)
	require.NoError(t, err)
	assert.Equal(t, updatedValue, result.Data())

	// Test store full behavior
	logger, _ := zap.NewDevelopment()
	config := &memcfg.MemoryConfig{
		MaxSize:         2,
		CleanupInterval: 50 * time.Millisecond,
	}
	limitedStore := memstore.NewMemoryStore(registry.ID{NS: "test", Name: "limited"}, config, logger)

	// Fill the store to capacity
	err = limitedStore.Set(ctx, createTestEntry("test:key1", "value1"))
	require.NoError(t, err)
	err = limitedStore.Set(ctx, createTestEntry("test:key2", "value2"))
	require.NoError(t, err)

	// Try to add another entry (should fail with ErrStoreFull)
	err = limitedStore.Set(ctx, createTestEntry("test:key3", "value3"))
	assert.Equal(t, store.ErrStoreFull, err)

	// Test with closed store
	err = ms.Stop(ctx)
	require.NoError(t, err)

	err = ms.Set(ctx, createTestEntry("test:key2", "value"))
	assert.Equal(t, store.ErrStoreClosed, err)
}

// TestMemoryStore_Delete tests the Delete functionality
func TestMemoryStore_Delete(t *testing.T) {
	ms := createTestStore(t)
	ctx := context.Background()

	// Set a test value
	testKey := registry.ParseID("test:key1")
	err := ms.Set(ctx, createTestEntry("test:key1", "test value"))
	require.NoError(t, err)

	// Delete the value
	err = ms.Delete(ctx, testKey)
	require.NoError(t, err)

	// Verify it's gone
	exists, err := ms.Has(ctx, testKey)
	require.NoError(t, err)
	assert.False(t, exists)

	// Test deleting a non-existent key
	err = ms.Delete(ctx, registry.ParseID("test:nonexistent"))
	assert.Equal(t, store.ErrKeyNotFound, err)

	// Test with closed store
	err = ms.Stop(ctx)
	require.NoError(t, err)

	err = ms.Delete(ctx, testKey)
	assert.Equal(t, store.ErrStoreClosed, err)
}

// TestMemoryStore_Has tests the Has functionality
func TestMemoryStore_Has(t *testing.T) {
	ms := createTestStore(t)
	ctx := context.Background()

	// Test checking a non-existent key
	testKey := registry.ParseID("test:key1")
	exists, err := ms.Has(ctx, testKey)
	require.NoError(t, err)
	assert.False(t, exists)

	// Set a value and check again
	err = ms.Set(ctx, createTestEntry("test:key1", "test value"))
	require.NoError(t, err)

	exists, err = ms.Has(ctx, testKey)
	require.NoError(t, err)
	assert.True(t, exists)

	// Test with TTL and expiration
	expiringKey := registry.ParseID("test:expiring")
	err = ms.Set(ctx, createTestEntryWithTTL("test:expiring", "will expire", 50*time.Millisecond))
	require.NoError(t, err)

	// Initially exists
	exists, err = ms.Has(ctx, expiringKey)
	require.NoError(t, err)
	assert.True(t, exists)

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Should no longer exist after expiration
	exists, err = ms.Has(ctx, expiringKey)
	require.NoError(t, err)
	assert.False(t, exists)

	// Test with closed store
	err = ms.Stop(ctx)
	require.NoError(t, err)

	_, err = ms.Has(ctx, testKey)
	assert.Equal(t, store.ErrStoreClosed, err)
}

// TestMemoryStore_Acquire tests the resource.Provider interface implementation
func TestMemoryStore_Acquire(t *testing.T) {
	ms := createTestStore(t)
	ctx := context.Background()

	// Acquire the store resource in normal mode
	res, err := ms.Acquire(ctx, registry.ParseID("test:resource"), resource.ModeNormal)
	require.NoError(t, err)

	// Get the store from the resource
	storeInterface, err := res.Get()
	require.NoError(t, err)

	// Verify it's a store.Store
	storeImpl, ok := storeInterface.(store.Store)
	assert.True(t, ok)
	assert.NotNil(t, storeImpl)

	// Release the resource
	res.Release()

	// Try to get after release (should fail)
	_, err = res.Get()
	assert.Equal(t, resource.ErrResourceReleased, err)

	// Try exclusive mode (should fail since it's not supported)
	_, err = ms.Acquire(ctx, registry.ParseID("test:resource"), resource.ModeExclusive)
	assert.Equal(t, resource.ErrResourceLocked, err)

	// Test with closed store
	err = ms.Stop(ctx)
	require.NoError(t, err)

	_, err = ms.Acquire(ctx, registry.ParseID("test:resource"), resource.ModeNormal)
	assert.Equal(t, resource.ErrResourceReleased, err)
}

// TestMemoryStore_Start tests the Start functionality and cleanup routine
func TestMemoryStore_Start(t *testing.T) {
	ms := createTestStore(t)
	ctx := context.Background()

	// Start the service
	statusChan, err := ms.Start(ctx)
	require.NoError(t, err)
	assert.NotNil(t, statusChan)

	// Set a value with TTL
	expiringKey := registry.ParseID("test:expiring")
	err = ms.Set(ctx, createTestEntryWithTTL("test:expiring", "will expire", 50*time.Millisecond))
	require.NoError(t, err)

	// Initially exists
	exists, err := ms.Has(ctx, expiringKey)
	require.NoError(t, err)
	assert.True(t, exists)

	// Wait for cleanup cycle (longer than the CleanupInterval and TTL)
	time.Sleep(200 * time.Millisecond)

	// Should be cleaned up by the cleanup routine
	exists, err = ms.Has(ctx, expiringKey)
	require.NoError(t, err)
	assert.False(t, exists)

	// Check we can start again (should be idempotent)
	newStatusChan, err := ms.Start(ctx)
	require.NoError(t, err)
	assert.NotNil(t, newStatusChan)

	// Clean up
	err = ms.Stop(ctx)
	require.NoError(t, err)
}

// TestMemoryStore_Stop tests the Stop functionality
func TestMemoryStore_Stop(t *testing.T) {
	ms := createTestStore(t)
	ctx := context.Background()

	// Start the service
	_, err := ms.Start(ctx)
	require.NoError(t, err)

	// Stop the service
	err = ms.Stop(ctx)
	require.NoError(t, err)

	// Verify store operations now fail with ErrStoreClosed
	_, err = ms.Get(ctx, registry.ParseID("test:anything"))
	assert.Equal(t, store.ErrStoreClosed, err)

	// Stopping again should be a no-op
	err = ms.Stop(ctx)
	require.NoError(t, err)
}

// TestMemoryStore_StopWithTimeout tests the Stop functionality with timeout
func TestMemoryStore_StopWithTimeout(t *testing.T) {
	ms := createTestStore(t)
	ctx := context.Background()

	// Start the service
	_, err := ms.Start(ctx)
	require.NoError(t, err)

	// Create a short timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, 1*time.Millisecond)
	defer cancel()

	// Stop should succeed despite the short timeout
	// since there's no long-running operation in our test store
	err = ms.Stop(timeoutCtx)
	require.NoError(t, err)
}

// TestMemoryStore_ConcurrentAccess tests concurrent access to the store
func TestMemoryStore_ConcurrentAccess(t *testing.T) {
	// Create a store with a larger capacity for concurrent test
	logger := zap.NewNop()
	config := &memcfg.MemoryConfig{
		MaxSize:         1000, // Increased to handle concurrent operations
		CleanupInterval: 50 * time.Millisecond,
	}
	ms := memstore.NewMemoryStore(registry.ID{NS: "test", Name: "store"}, config, logger)
	ctx := context.Background()

	// Start the service
	_, err := ms.Start(ctx)
	require.NoError(t, err)

	// Number of concurrent goroutines
	const numGoroutines = 10
	// Number of operations per goroutine
	const numOps = 100

	// Wait group to synchronize goroutines
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Start concurrent goroutines
	for i := 0; i < numGoroutines; i++ {
		go func(routineID int) {
			defer wg.Done()

			// Perform operations
			for j := 0; j < numOps; j++ {
				key := registry.ParseID(fmt.Sprintf("test:key%d-%d", routineID, j))
				value := fmt.Sprintf("value-%d-%d", routineID, j)

				// Set
				err := ms.Set(ctx, createTestEntry(key.String(), value))
				// Either no error or store full is acceptable during concurrent test
				if err != nil && err != store.ErrStoreFull {
					assert.Fail(t, "Unexpected error on Set: %v", err)
				}

				// Get
				result, err := ms.Get(ctx, key)
				if err == nil {
					assert.Equal(t, value, result.Data())
				} else {
					// Could be another goroutine deleted it or it wasn't set due to capacity
					assert.Contains(t, []error{store.ErrKeyNotFound}, err)
				}

				// Has
				_, err = ms.Has(ctx, key)
				// Has should just work or return key not found
				assert.Contains(t, []error{nil, store.ErrKeyNotFound}, err)

				// Delete (occasionally)
				if j%3 == 0 {
					_ = ms.Delete(ctx, key)
				}
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Verify we can still use the store
	testKey := registry.ParseID("test:final")
	err = ms.Set(ctx, createTestEntry("test:final", "final value"))
	require.NoError(t, err)

	result, err := ms.Get(ctx, testKey)
	require.NoError(t, err)
	assert.Equal(t, "final value", result.Data())

	// Clean up
	err = ms.Stop(ctx)
	require.NoError(t, err)
}

// TestMemoryStore_CleanupBehavior tests the cleanup routine's behavior
func TestMemoryStore_CleanupBehavior(t *testing.T) {
	logger := zap.NewNop()
	config := &memcfg.MemoryConfig{
		MaxSize:         100,
		CleanupInterval: 50 * time.Millisecond, // Short interval for testing
	}
	ms := memstore.NewMemoryStore(registry.ID{NS: "test", Name: "cleanup-test"}, config, logger)
	ctx := context.Background()

	// Start the service
	_, err := ms.Start(ctx)
	require.NoError(t, err)

	// Set multiple entries with different TTLs
	keys := []registry.ID{
		registry.ParseID("test:expire-50ms"),
		registry.ParseID("test:expire-100ms"),
		registry.ParseID("test:expire-150ms"),
		registry.ParseID("test:no-expire"),
	}

	err = ms.Set(ctx, createTestEntryWithTTL("test:expire-50ms", "50ms", 50*time.Millisecond))
	require.NoError(t, err)

	err = ms.Set(ctx, createTestEntryWithTTL("test:expire-100ms", "100ms", 100*time.Millisecond))
	require.NoError(t, err)

	err = ms.Set(ctx, createTestEntryWithTTL("test:expire-150ms", "150ms", 150*time.Millisecond))
	require.NoError(t, err)

	err = ms.Set(ctx, createTestEntry("test:no-expire", "forever"))
	require.NoError(t, err)

	// Verify all entries exist initially
	for _, key := range keys {
		exists, err := ms.Has(ctx, key)
		require.NoError(t, err)
		assert.True(t, exists, "Key %s should exist initially", key)
	}

	// Wait for the first cleanup cycle
	time.Sleep(75 * time.Millisecond)

	// After 75ms: 50ms key should be gone, others should remain
	exists, err := ms.Has(ctx, keys[0]) // 50ms key
	require.NoError(t, err)
	assert.False(t, exists, "50ms key should be expired")

	for i := 1; i < len(keys); i++ {
		exists, err = ms.Has(ctx, keys[i])
		require.NoError(t, err)
		assert.True(t, exists, "Key %s should still exist", keys[i])
	}

	// Wait for the second cleanup cycle
	time.Sleep(50 * time.Millisecond) // Now at ~125ms total

	// After 125ms: 50ms and 100ms keys should be gone, others should remain
	for i := 0; i < 2; i++ {
		exists, err = ms.Has(ctx, keys[i])
		require.NoError(t, err)
		assert.False(t, exists, "Key %s should be expired", keys[i])
	}

	for i := 2; i < len(keys); i++ {
		exists, err = ms.Has(ctx, keys[i])
		require.NoError(t, err)
		assert.True(t, exists, "Key %s should still exist", keys[i])
	}

	// Wait for the third cleanup cycle
	time.Sleep(50 * time.Millisecond) // Now at ~175ms total

	// After 175ms: All except the non-expiring key should be gone
	for i := 0; i < 3; i++ {
		exists, err = ms.Has(ctx, keys[i])
		require.NoError(t, err)
		assert.False(t, exists, "Key %s should be expired", keys[i])
	}

	// The non-expiring key should still be there
	exists, err = ms.Has(ctx, keys[3])
	require.NoError(t, err)
	assert.True(t, exists, "Non-expiring key should still exist")

	// Clean up
	err = ms.Stop(ctx)
	require.NoError(t, err)
}
