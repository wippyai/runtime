// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

func startTestService(t *testing.T) *Service {
	t.Helper()
	bus := eventbus.NewBus()
	svc := NewService("test", bus, zap.NewNop())
	_, err := svc.Start(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = svc.Stop(context.Background())
		bus.Stop()
	})
	return svc
}

// --- Basic CRUD ---

func TestService_SetAndGet(t *testing.T) {
	svc := startTestService(t)

	ver, err := svc.Set("key1", []byte("value1"))
	require.NoError(t, err)
	assert.Equal(t, kvapi.Version(1), ver)

	entry, err := svc.Get("key1")
	require.NoError(t, err)
	assert.Equal(t, "key1", entry.Key)
	assert.Equal(t, []byte("value1"), entry.Value)
	assert.Equal(t, kvapi.Version(1), entry.Version)
}

func TestService_GetNotFound(t *testing.T) {
	svc := startTestService(t)

	_, err := svc.Get("nonexistent")
	assert.ErrorIs(t, err, kvapi.ErrKeyNotFound)
}

func TestService_Delete(t *testing.T) {
	svc := startTestService(t)

	_, err := svc.Set("key1", []byte("value1"))
	require.NoError(t, err)

	err = svc.Delete("key1")
	require.NoError(t, err)

	_, err = svc.Get("key1")
	assert.ErrorIs(t, err, kvapi.ErrKeyNotFound)
}

func TestService_DeleteNotFound(t *testing.T) {
	svc := startTestService(t)

	err := svc.Delete("nonexistent")
	assert.ErrorIs(t, err, kvapi.ErrKeyNotFound)
}

func TestService_SetOverwrite(t *testing.T) {
	svc := startTestService(t)

	v1, err := svc.Set("key1", []byte("value1"))
	require.NoError(t, err)

	v2, err := svc.Set("key1", []byte("value2"))
	require.NoError(t, err)
	assert.Greater(t, v2, v1)

	entry, err := svc.Get("key1")
	require.NoError(t, err)
	assert.Equal(t, []byte("value2"), entry.Value)
}

// --- CAS operations ---

func TestService_SetIfAbsent(t *testing.T) {
	svc := startTestService(t)

	ver, ok, err := svc.SetIfAbsent("key1", []byte("value1"))
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, kvapi.Version(1), ver)

	ver2, ok2, err := svc.SetIfAbsent("key1", []byte("value2"))
	require.NoError(t, err)
	assert.False(t, ok2)
	assert.Equal(t, kvapi.Version(1), ver2)

	entry, err := svc.Get("key1")
	require.NoError(t, err)
	assert.Equal(t, []byte("value1"), entry.Value)
}

func TestService_CompareAndSwap(t *testing.T) {
	svc := startTestService(t)

	v1, err := svc.Set("key1", []byte("value1"))
	require.NoError(t, err)

	// Correct version
	v2, ok, err := svc.CompareAndSwap("key1", v1, []byte("value2"))
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Greater(t, v2, v1)

	// Wrong version
	_, ok2, err := svc.CompareAndSwap("key1", v1, []byte("value3"))
	require.NoError(t, err)
	assert.False(t, ok2)

	entry, err := svc.Get("key1")
	require.NoError(t, err)
	assert.Equal(t, []byte("value2"), entry.Value)
}

func TestService_CompareAndSwap_CreateFromZero(t *testing.T) {
	svc := startTestService(t)

	// CAS with version 0 on nonexistent key creates it
	ver, ok, err := svc.CompareAndSwap("key1", 0, []byte("value1"))
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, kvapi.Version(1), ver)
}

// --- Scan ---

func TestService_Scan(t *testing.T) {
	svc := startTestService(t)

	_, _ = svc.Set("users/alice", []byte("alice"))
	_, _ = svc.Set("users/bob", []byte("bob"))
	_, _ = svc.Set("config/db", []byte("pg"))

	var users []string
	err := svc.Scan("users/", func(e kvapi.Entry) bool {
		users = append(users, e.Key)
		return true
	})
	require.NoError(t, err)
	sort.Strings(users)
	assert.Equal(t, []string{"users/alice", "users/bob"}, users)
}

func TestService_ScanEmpty(t *testing.T) {
	svc := startTestService(t)

	var count int
	err := svc.Scan("anything/", func(e kvapi.Entry) bool {
		count++
		return true
	})
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestService_ScanAll(t *testing.T) {
	svc := startTestService(t)

	_, _ = svc.Set("a", []byte("1"))
	_, _ = svc.Set("b", []byte("2"))

	var keys []string
	err := svc.Scan("", func(e kvapi.Entry) bool {
		keys = append(keys, e.Key)
		return true
	})
	require.NoError(t, err)
	assert.Len(t, keys, 2)
}

func TestService_ScanEarlyStop(t *testing.T) {
	svc := startTestService(t)

	for i := 0; i < 10; i++ {
		_, _ = svc.Set("key"+string(rune('0'+i)), []byte("val"))
	}

	var count int
	_ = svc.Scan("key", func(e kvapi.Entry) bool {
		count++
		return count < 3
	})
	assert.Equal(t, 3, count)
}

// --- Leases ---

func TestService_LeaseGrantAndExpire(t *testing.T) {
	svc := startTestService(t)

	lease, err := svc.GrantLease(context.Background(), 100*time.Millisecond)
	require.NoError(t, err)
	assert.NotEmpty(t, string(lease.ID()))

	_, err = svc.SetWithLease("session:alice", []byte("data"), lease.ID())
	require.NoError(t, err)

	// Key exists
	_, err = svc.Get("session:alice")
	require.NoError(t, err)

	// Wait for lease to expire
	select {
	case <-lease.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("lease did not expire in time")
	}

	// Key should be gone
	require.Eventually(t, func() bool {
		_, err := svc.Get("session:alice")
		return err != nil
	}, time.Second, 10*time.Millisecond)
}

func TestService_LeaseRevoke(t *testing.T) {
	svc := startTestService(t)

	lease, err := svc.GrantLease(context.Background(), 10*time.Second)
	require.NoError(t, err)

	_, err = svc.SetWithLease("key1", []byte("v1"), lease.ID())
	require.NoError(t, err)
	_, err = svc.SetWithLease("key2", []byte("v2"), lease.ID())
	require.NoError(t, err)

	err = lease.Revoke(context.Background())
	require.NoError(t, err)

	_, err = svc.Get("key1")
	assert.ErrorIs(t, err, kvapi.ErrKeyNotFound)
	_, err = svc.Get("key2")
	assert.ErrorIs(t, err, kvapi.ErrKeyNotFound)
}

func TestService_LeaseKeepAlive(t *testing.T) {
	svc := startTestService(t)

	lease, err := svc.GrantLease(context.Background(), 150*time.Millisecond)
	require.NoError(t, err)

	_, err = svc.SetWithLease("key1", []byte("v1"), lease.ID())
	require.NoError(t, err)

	// Renew before expiry
	time.Sleep(80 * time.Millisecond)
	err = lease.KeepAlive(context.Background())
	require.NoError(t, err)

	// Should still exist after original TTL would have expired
	time.Sleep(100 * time.Millisecond)
	_, err = svc.Get("key1")
	require.NoError(t, err)
}

func TestService_SetWithLease_InvalidLease(t *testing.T) {
	svc := startTestService(t)

	_, err := svc.SetWithLease("key1", []byte("v1"), "nonexistent-lease")
	assert.ErrorIs(t, err, kvapi.ErrLeaseNotFound)
}

func TestService_SetIfAbsentWithLease(t *testing.T) {
	svc := startTestService(t)

	lease, err := svc.GrantLease(context.Background(), 5*time.Second)
	require.NoError(t, err)

	ver, ok, err := svc.SetIfAbsentWithLease("key1", []byte("v1"), lease.ID())
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, kvapi.Version(1), ver)

	_, ok2, err := svc.SetIfAbsentWithLease("key1", []byte("v2"), lease.ID())
	require.NoError(t, err)
	assert.False(t, ok2)
}

// --- Watch ---

func TestService_Watch(t *testing.T) {
	svc := startTestService(t)

	w, err := svc.Watch(context.Background(), "users/")
	require.NoError(t, err)
	defer w.Close()

	// Give watcher time to subscribe
	time.Sleep(50 * time.Millisecond)

	_, _ = svc.Set("users/alice", []byte("data"))
	_, _ = svc.Set("config/db", []byte("pg")) // should not appear

	select {
	case evt := <-w.Events():
		assert.Equal(t, kvapi.WatchPut, evt.Type)
		assert.Equal(t, "users/alice", evt.Current.Key)
	case <-time.After(time.Second):
		t.Fatal("expected watch event")
	}

	// Should not receive the config key
	select {
	case evt := <-w.Events():
		t.Fatalf("unexpected event: %+v", evt)
	case <-time.After(100 * time.Millisecond):
		// expected
	}
}

func TestService_WatchDelete(t *testing.T) {
	svc := startTestService(t)

	_, _ = svc.Set("key1", []byte("value1"))

	w, err := svc.Watch(context.Background(), "key")
	require.NoError(t, err)
	defer w.Close()

	time.Sleep(50 * time.Millisecond)

	_ = svc.Delete("key1")

	select {
	case evt := <-w.Events():
		assert.Equal(t, kvapi.WatchDelete, evt.Type)
		assert.Equal(t, "key1", evt.Previous.Key)
	case <-time.After(time.Second):
		t.Fatal("expected watch delete event")
	}
}

func TestService_WatchAll(t *testing.T) {
	svc := startTestService(t)

	w, err := svc.Watch(context.Background(), "")
	require.NoError(t, err)
	defer w.Close()

	time.Sleep(50 * time.Millisecond)

	_, _ = svc.Set("a", []byte("1"))
	_, _ = svc.Set("b", []byte("2"))

	for i := 0; i < 2; i++ {
		select {
		case evt := <-w.Events():
			assert.Equal(t, kvapi.WatchPut, evt.Type)
		case <-time.After(time.Second):
			t.Fatal("expected watch event")
		}
	}
}

// --- Concurrency ---

func TestService_ConcurrentReads(t *testing.T) {
	svc := startTestService(t)

	for i := 0; i < 100; i++ {
		_, _ = svc.Set("key"+string(rune(i)), []byte("value"))
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				_, _ = svc.Get("key" + string(rune(j%100)))
				_ = svc.Scan("key", func(e kvapi.Entry) bool { return true })
			}
		}()
	}
	wg.Wait()
}

func TestService_ConcurrentWritesAndReads(t *testing.T) {
	svc := startTestService(t)

	var wg sync.WaitGroup
	// Writers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				key := "key" + string(rune(id*100+j))
				_, _ = svc.Set(key, []byte("value"))
			}
		}(i)
	}
	// Readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				_, _ = svc.Get("key" + string(rune(j%500)))
			}
		}()
	}
	wg.Wait()
}

// --- Version monotonicity ---

func TestService_VersionMonotonicallyIncreasing(t *testing.T) {
	svc := startTestService(t)

	var lastVersion kvapi.Version
	for i := 0; i < 50; i++ {
		ver, err := svc.Set("key", []byte("value"))
		require.NoError(t, err)
		assert.Greater(t, ver, lastVersion)
		lastVersion = ver
	}
}
