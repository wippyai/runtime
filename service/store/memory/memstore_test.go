// SPDX-License-Identifier: MPL-2.0

package memory_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	memcfg "github.com/wippyai/runtime/api/service/store/memory"
	"github.com/wippyai/runtime/api/store"
	servicestore "github.com/wippyai/runtime/service/store"
	memorystore "github.com/wippyai/runtime/service/store/memory"
	systemresource "github.com/wippyai/runtime/system/resource"
	"go.uber.org/zap"
)

// createTestStore is a helper function that creates a memory store with default test configuration
func createTestStore(_ *testing.T) *memorystore.Store {
	logger := zap.NewNop()
	config := &memcfg.Config{
		MaxSize:         100,
		CleanupInterval: 50 * time.Millisecond,
	}
	return memorystore.NewStore(registry.NewID("test", "store"), config, logger)
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
	ctx := ctxapi.NewRootContext()

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
	assert.Equal(t, servicestore.ErrStoreClosed, err)
}

// TestMemoryStore_Set tests the Set functionality
func TestMemoryStore_Set(t *testing.T) {
	ms := createTestStore(t)
	ctx := ctxapi.NewRootContext()

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
	config := &memcfg.Config{
		MaxSize:         2,
		CleanupInterval: 50 * time.Millisecond,
	}
	limitedStore := memorystore.NewStore(registry.NewID("test", "limited"), config, logger)

	// Fill the store to capacity
	err = limitedStore.Set(ctx, createTestEntry("test:key1", "value1"))
	require.NoError(t, err)
	err = limitedStore.Set(ctx, createTestEntry("test:key2", "value2"))
	require.NoError(t, err)

	// Try to add another entry (should fail with ErrStoreFull)
	err = limitedStore.Set(ctx, createTestEntry("test:key3", "value3"))
	assert.Equal(t, servicestore.ErrStoreFull, err)

	// Test with closed store
	err = ms.Stop(ctx)
	require.NoError(t, err)

	err = ms.Set(ctx, createTestEntry("test:key2", "value"))
	assert.Equal(t, servicestore.ErrStoreClosed, err)
}

func TestMemoryStore_InfoEntryListPut(t *testing.T) {
	ms := createTestStore(t)
	ctx := ctxapi.NewRootContext()

	info := ms.StoreInfo(ctx)
	assert.Equal(t, registry.NewID("test", "store"), info.ID)
	assert.Equal(t, store.BackendMemory, info.Backend)
	assert.Equal(t, store.ConsistencyLocal, info.Consistency)
	assert.False(t, info.Durable)
	assert.True(t, info.List)
	assert.True(t, info.Versioned)
	assert.True(t, info.ConditionalPut)
	assert.True(t, info.TTL)

	first, err := ms.Put(ctx, registry.ParseID("test:item-b"), payload.New("b"), store.PutOptions{})
	require.NoError(t, err)
	assert.Equal(t, "test:item-b", first.Key.String())
	assert.NotZero(t, first.Version)

	_, err = ms.Put(ctx, registry.ParseID("test:bad-ttl"), payload.New("bad"), store.PutOptions{TTL: -time.Second})
	assert.ErrorIs(t, err, store.ErrInvalidOptions)

	_, err = ms.Put(ctx, registry.ParseID("test:item-b"), payload.New("dupe"), store.PutOptions{OnlyIfAbsent: true})
	assert.ErrorIs(t, err, store.ErrKeyExists)

	_, err = ms.Put(ctx, registry.ParseID("test:item-b"), payload.New("bad"), store.PutOptions{HasVersion: true, Version: first.Version + 100})
	assert.ErrorIs(t, err, store.ErrVersionMismatch)

	second, err := ms.Put(ctx, registry.ParseID("test:item-b"), payload.New("updated"), store.PutOptions{HasVersion: true, Version: first.Version})
	require.NoError(t, err)
	assert.Greater(t, second.Version, first.Version)

	entry, err := ms.Entry(ctx, registry.ParseID("test:item-b"))
	require.NoError(t, err)
	assert.Equal(t, second.Version, entry.Version)
	assert.Equal(t, "updated", entry.Value.Data())

	_, err = ms.Put(ctx, registry.ParseID("test:item-a"), payload.New("a"), store.PutOptions{})
	require.NoError(t, err)
	_, err = ms.Put(ctx, registry.ParseID("other:item"), payload.New("other"), store.PutOptions{})
	require.NoError(t, err)

	page, err := ms.List(ctx, store.ListOptions{Prefix: "test:item-", Limit: 1})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "test:item-a", page.Items[0].Key.String())
	assert.Equal(t, "test:item-a", page.Cursor)
	assert.True(t, page.HasMore)

	next, err := ms.List(ctx, store.ListOptions{Prefix: "test:item-", After: page.Cursor, Limit: 10})
	require.NoError(t, err)
	require.Len(t, next.Items, 1)
	assert.Equal(t, "test:item-b", next.Items[0].Key.String())
	assert.False(t, next.HasMore)
}

// TestMemoryStore_Delete tests the Delete functionality
func TestMemoryStore_Delete(t *testing.T) {
	ms := createTestStore(t)
	ctx := ctxapi.NewRootContext()

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
	assert.Equal(t, servicestore.ErrStoreClosed, err)
}

// TestMemoryStore_Has tests the Has functionality
func TestMemoryStore_Has(t *testing.T) {
	ms := createTestStore(t)
	ctx := ctxapi.NewRootContext()

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
	assert.Equal(t, servicestore.ErrStoreClosed, err)
}

// TestMemoryStore_Acquire tests the resource.Provider interface implementation
func TestMemoryStore_Acquire(t *testing.T) {
	ms := createTestStore(t)
	ctx := ctxapi.NewRootContext()

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
	assert.Equal(t, resource.ErrReleased, err)

	// Try exclusive mode (should fail since it's not supported)
	_, err = ms.Acquire(ctx, registry.ParseID("test:resource"), resource.ModeExclusive)
	assert.Equal(t, systemresource.ErrLocked, err)

	// Test with closed store
	err = ms.Stop(ctx)
	require.NoError(t, err)

	_, err = ms.Acquire(ctx, registry.ParseID("test:resource"), resource.ModeNormal)
	assert.Equal(t, resource.ErrReleased, err)
}

// TestMemoryStore_Start tests the Start functionality and cleanup routine
func TestMemoryStore_Start(t *testing.T) {
	ms := createTestStore(t)
	ctx := ctxapi.NewRootContext()

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
	ctx := ctxapi.NewRootContext()

	// Start the service
	_, err := ms.Start(ctx)
	require.NoError(t, err)

	// Stop the service
	err = ms.Stop(ctx)
	require.NoError(t, err)

	// Verify store operations now fail with ErrStoreClosed
	_, err = ms.Get(ctx, registry.ParseID("test:anything"))
	assert.Equal(t, servicestore.ErrStoreClosed, err)

	// Stopping again should be a no-op
	err = ms.Stop(ctx)
	require.NoError(t, err)
}

// TestMemoryStore_StopWithTimeout tests the Stop functionality with timeout
func TestMemoryStore_StopWithTimeout(t *testing.T) {
	ms := createTestStore(t)
	ctx := ctxapi.NewRootContext()

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
	config := &memcfg.Config{
		MaxSize:         1000, // Increased to handle concurrent operations
		CleanupInterval: 50 * time.Millisecond,
	}
	ms := memorystore.NewStore(registry.NewID("test", "store"), config, logger)
	ctx := ctxapi.NewRootContext()

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
				if err != nil && !errors.Is(err, servicestore.ErrStoreFull) {
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

// TestMemoryStore_ConcurrentReadWrite tests concurrent read/write stress
func TestMemoryStore_ConcurrentReadWrite(t *testing.T) {
	logger := zap.NewNop()
	config := &memcfg.Config{
		MaxSize:         10000,
		CleanupInterval: 100 * time.Millisecond,
	}
	ms := memorystore.NewStore(registry.NewID("test", "stress"), config, logger)
	ctx := ctxapi.NewRootContext()

	_, err := ms.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = ms.Stop(ctx) }()

	const (
		numWriters = 10
		numReaders = 20
		numOps     = 500
		keySpace   = 100
	)

	var wg sync.WaitGroup
	errChan := make(chan error, (numWriters+numReaders)*numOps)

	// Writers
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for i := 0; i < numOps; i++ {
				keyIdx := (writerID*numOps + i) % keySpace
				key := fmt.Sprintf("test:key%d", keyIdx)
				value := fmt.Sprintf("value-%d-%d", writerID, i)

				if err := ms.Set(ctx, createTestEntry(key, value)); err != nil {
					if !errors.Is(err, servicestore.ErrStoreFull) {
						errChan <- fmt.Errorf("set error: %w", err)
					}
				}

				if i%7 == 0 {
					_ = ms.Delete(ctx, registry.ParseID(key))
				}
			}
		}(w)
	}

	// Readers
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			for i := 0; i < numOps; i++ {
				keyIdx := (readerID*numOps + i) % keySpace
				key := registry.ParseID(fmt.Sprintf("test:key%d", keyIdx))

				_, err := ms.Get(ctx, key)
				if err != nil && !errors.Is(err, store.ErrKeyNotFound) {
					errChan <- fmt.Errorf("get error: %w", err)
				}

				_, err = ms.Has(ctx, key)
				if err != nil {
					errChan <- fmt.Errorf("has error: %w", err)
				}
			}
		}(r)
	}

	wg.Wait()
	close(errChan)

	var errs []error
	for err := range errChan {
		errs = append(errs, err)
	}
	require.Empty(t, errs, "unexpected errors during stress test: %v", errs)
}

// TestMemoryStore_TTLStress tests TTL expiration under concurrent load
func TestMemoryStore_TTLStress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}
	logger := zap.NewNop()
	config := &memcfg.Config{
		MaxSize:         5000,
		CleanupInterval: 10 * time.Millisecond,
	}
	ms := memorystore.NewStore(registry.NewID("test", "ttl-stress"), config, logger)
	ctx := ctxapi.NewRootContext()

	_, err := ms.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = ms.Stop(ctx) }()

	const numKeys = 200
	var wg sync.WaitGroup

	// Set keys with varying TTLs
	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("test:ttl%d", i)
		ttl := time.Duration(20+i%50) * time.Millisecond

		err := ms.Set(ctx, createTestEntryWithTTL(key, i, ttl))
		require.NoError(t, err)
	}

	// Concurrent reads while TTLs expire
	for r := 0; r < 5; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				for i := 0; i < numKeys; i++ {
					key := registry.ParseID(fmt.Sprintf("test:ttl%d", i))
					_, _ = ms.Get(ctx, key)
					_, _ = ms.Has(ctx, key)
				}
				time.Sleep(5 * time.Millisecond)
			}
		}()
	}

	wg.Wait()

	// After enough time, all TTL keys should be gone
	time.Sleep(100 * time.Millisecond)

	for i := 0; i < numKeys; i++ {
		key := registry.ParseID(fmt.Sprintf("test:ttl%d", i))
		exists, err := ms.Has(ctx, key)
		require.NoError(t, err)
		assert.False(t, exists, "key %d should have expired", i)
	}
}

// TestMemoryStore_RapidStartStop tests rapid start/stop cycles
func TestMemoryStore_RapidStartStop(t *testing.T) {
	for i := 0; i < 10; i++ {
		logger := zap.NewNop()
		config := &memcfg.Config{
			MaxSize:         100,
			CleanupInterval: 10 * time.Millisecond,
		}
		ms := memorystore.NewStore(registry.ID{NS: "test", Name: fmt.Sprintf("rapid-%d", i)}, config, logger)
		ctx := ctxapi.NewRootContext()

		_, err := ms.Start(ctx)
		require.NoError(t, err)

		// Do some operations
		for j := 0; j < 10; j++ {
			key := fmt.Sprintf("test:key%d", j)
			_ = ms.Set(ctx, createTestEntry(key, j))
		}

		err = ms.Stop(ctx)
		require.NoError(t, err)

		// Verify store is properly closed
		_, err = ms.Get(ctx, registry.ParseID("test:key0"))
		assert.Equal(t, servicestore.ErrStoreClosed, err)
	}
}

// TestMemoryStore_CapacityBoundary tests behavior at capacity boundaries
func TestMemoryStore_CapacityBoundary(t *testing.T) {
	logger := zap.NewNop()
	config := &memcfg.Config{
		MaxSize:         10,
		CleanupInterval: 50 * time.Millisecond,
	}
	ms := memorystore.NewStore(registry.NewID("test", "capacity"), config, logger)
	ctx := ctxapi.NewRootContext()

	_, err := ms.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = ms.Stop(ctx) }()

	// Fill to capacity
	for i := 0; i < 10; i++ {
		err := ms.Set(ctx, createTestEntry(fmt.Sprintf("test:key%d", i), i))
		require.NoError(t, err)
	}

	// Next set should fail
	err = ms.Set(ctx, createTestEntry("test:key10", 10))
	assert.Equal(t, servicestore.ErrStoreFull, err)

	// Update existing should succeed
	err = ms.Set(ctx, createTestEntry("test:key5", "updated"))
	require.NoError(t, err)

	val, err := ms.Get(ctx, registry.ParseID("test:key5"))
	require.NoError(t, err)
	assert.Equal(t, "updated", val.Data())

	// Delete one, then new set should succeed
	err = ms.Delete(ctx, registry.ParseID("test:key0"))
	require.NoError(t, err)

	err = ms.Set(ctx, createTestEntry("test:key10", 10))
	require.NoError(t, err)
}

func TestMemoryStore_SetPurgesExpiredBeforeCapacity(t *testing.T) {
	logger := zap.NewNop()
	config := &memcfg.Config{MaxSize: 1}
	ms := memorystore.NewStore(registry.NewID("test", "capacity-ttl"), config, logger)
	ctx := ctxapi.NewRootContext()

	err := ms.Set(ctx, createTestEntryWithTTL("test:expired", "old", 20*time.Millisecond))
	require.NoError(t, err)
	time.Sleep(40 * time.Millisecond)

	err = ms.Set(ctx, createTestEntry("test:fresh", "new"))
	require.NoError(t, err)

	_, err = ms.Get(ctx, registry.ParseID("test:expired"))
	assert.ErrorIs(t, err, store.ErrKeyNotFound)
	got, err := ms.Get(ctx, registry.ParseID("test:fresh"))
	require.NoError(t, err)
	assert.Equal(t, "new", got.Data())
}

// TestMemoryStore_CleanupBehavior tests the cleanup routine's behavior
func TestMemoryStore_CleanupBehavior(t *testing.T) {
	logger := zap.NewNop()
	config := &memcfg.Config{
		MaxSize:         100,
		CleanupInterval: 50 * time.Millisecond, // Short interval for testing
	}
	ms := memorystore.NewStore(registry.NewID("test", "cleanup-test"), config, logger)
	ctx := ctxapi.NewRootContext()

	// Start the service
	_, err := ms.Start(ctx)
	require.NoError(t, err)

	// Set multiple entries with different TTLs.
	// We intentionally keep wide TTL gaps because GitHub CI can have timer jitter.
	keys := []registry.ID{
		registry.ParseID("test:expire-200ms"),
		registry.ParseID("test:expire-500ms"),
		registry.ParseID("test:expire-900ms"),
		registry.ParseID("test:no-expire"),
	}

	err = ms.Set(ctx, createTestEntryWithTTL("test:expire-200ms", "200ms", 200*time.Millisecond))
	require.NoError(t, err)

	err = ms.Set(ctx, createTestEntryWithTTL("test:expire-500ms", "500ms", 500*time.Millisecond))
	require.NoError(t, err)

	err = ms.Set(ctx, createTestEntryWithTTL("test:expire-900ms", "900ms", 900*time.Millisecond))
	require.NoError(t, err)

	err = ms.Set(ctx, createTestEntry("test:no-expire", "forever"))
	require.NoError(t, err)

	// Verify all entries exist initially
	for _, key := range keys {
		exists, err := ms.Has(ctx, key)
		require.NoError(t, err)
		assert.True(t, exists, "Key %s should exist initially", key)
	}

	// In GitHub CI, fixed sleeps near TTL boundaries are flaky.
	// Use eventual/never checks with margins instead.
	require.Never(t, func() bool {
		exists, err := ms.Has(ctx, keys[1]) // 500ms key
		require.NoError(t, err)
		return !exists
	}, 150*time.Millisecond, 10*time.Millisecond, "500ms key should not expire early")

	require.Eventually(t, func() bool {
		exists, err := ms.Has(ctx, keys[0]) // 200ms key
		require.NoError(t, err)
		return !exists
	}, 900*time.Millisecond, 10*time.Millisecond, "200ms key should eventually expire")

	require.Eventually(t, func() bool {
		exists, err := ms.Has(ctx, keys[1]) // 500ms key
		require.NoError(t, err)
		return !exists
	}, 1200*time.Millisecond, 10*time.Millisecond, "500ms key should eventually expire")

	require.Eventually(t, func() bool {
		exists, err := ms.Has(ctx, keys[2]) // 900ms key
		require.NoError(t, err)
		return !exists
	}, 1800*time.Millisecond, 10*time.Millisecond, "900ms key should eventually expire")

	// The non-expiring key should still be present.
	exists, err := ms.Has(ctx, keys[3])
	require.NoError(t, err)
	assert.True(t, exists, "Non-expiring key should still exist")

	// Clean up
	err = ms.Stop(ctx)
	require.NoError(t, err)
}

// Benchmarks

func createBenchStore(_ *testing.B) (*memorystore.Store, context.Context) {
	logger := zap.NewNop()
	config := &memcfg.Config{
		MaxSize:         1000000,
		CleanupInterval: time.Hour,
	}
	ms := memorystore.NewStore(registry.NewID("bench", "store"), config, logger)
	ctx := ctxapi.NewRootContext()
	_, _ = ms.Start(ctx)
	return ms, ctx
}

func BenchmarkMemoryStore_Set(b *testing.B) {
	ms, ctx := createBenchStore(b)
	defer func() { _ = ms.Stop(ctx) }()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("test:key%d", i)
		_ = ms.Set(ctx, createTestEntry(key, i))
	}
}

func BenchmarkMemoryStore_Get(b *testing.B) {
	ms, ctx := createBenchStore(b)
	defer func() { _ = ms.Stop(ctx) }()

	// Pre-populate
	for i := 0; i < 10000; i++ {
		key := fmt.Sprintf("test:key%d", i)
		_ = ms.Set(ctx, createTestEntry(key, i))
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		key := registry.ParseID(fmt.Sprintf("test:key%d", i%10000))
		_, _ = ms.Get(ctx, key)
	}
}

func BenchmarkMemoryStore_Has(b *testing.B) {
	ms, ctx := createBenchStore(b)
	defer func() { _ = ms.Stop(ctx) }()

	// Pre-populate
	for i := 0; i < 10000; i++ {
		key := fmt.Sprintf("test:key%d", i)
		_ = ms.Set(ctx, createTestEntry(key, i))
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		key := registry.ParseID(fmt.Sprintf("test:key%d", i%10000))
		_, _ = ms.Has(ctx, key)
	}
}

func BenchmarkMemoryStore_Delete(b *testing.B) {
	ms, ctx := createBenchStore(b)
	defer func() { _ = ms.Stop(ctx) }()

	// Pre-populate
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("test:key%d", i)
		_ = ms.Set(ctx, createTestEntry(key, i))
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		key := registry.ParseID(fmt.Sprintf("test:key%d", i))
		_ = ms.Delete(ctx, key)
	}
}

func BenchmarkMemoryStore_SetWithTTL(b *testing.B) {
	ms, ctx := createBenchStore(b)
	defer func() { _ = ms.Stop(ctx) }()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("test:key%d", i)
		_ = ms.Set(ctx, createTestEntryWithTTL(key, i, time.Hour))
	}
}

func BenchmarkMemoryStore_ConcurrentGet(b *testing.B) {
	ms, ctx := createBenchStore(b)
	defer func() { _ = ms.Stop(ctx) }()

	// Pre-populate
	for i := 0; i < 10000; i++ {
		key := fmt.Sprintf("test:key%d", i)
		_ = ms.Set(ctx, createTestEntry(key, i))
	}

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := registry.ParseID(fmt.Sprintf("test:key%d", i%10000))
			_, _ = ms.Get(ctx, key)
			i++
		}
	})
}

func BenchmarkMemoryStore_ConcurrentSet(b *testing.B) {
	ms, ctx := createBenchStore(b)
	defer func() { _ = ms.Stop(ctx) }()

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("test:key%d", i)
			_ = ms.Set(ctx, createTestEntry(key, i))
			i++
		}
	})
}

func BenchmarkMemoryStore_ConcurrentMixed(b *testing.B) {
	ms, ctx := createBenchStore(b)
	defer func() { _ = ms.Stop(ctx) }()

	// Pre-populate
	for i := 0; i < 10000; i++ {
		key := fmt.Sprintf("test:key%d", i)
		_ = ms.Set(ctx, createTestEntry(key, i))
	}

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("test:key%d", i%10000)
			keyID := registry.ParseID(key)

			switch i % 4 {
			case 0:
				_ = ms.Set(ctx, createTestEntry(key, i))
			case 1, 2:
				_, _ = ms.Get(ctx, keyID)
			case 3:
				_, _ = ms.Has(ctx, keyID)
			}
			i++
		}
	})
}
