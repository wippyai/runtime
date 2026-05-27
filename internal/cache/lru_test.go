// SPDX-License-Identifier: MPL-2.0

package lru

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBasicOperations(t *testing.T) {
	t.Run("empty cache", func(t *testing.T) {
		cache := New[string, int]()
		defer cache.Close()

		_, exists := cache.Get("nonexistent")
		if exists {
			t.Error("expected Get on empty cache to return false")
		}
	})

	t.Run("set and get", func(t *testing.T) {
		cache := New[string, int]()
		defer cache.Close()

		_ = cache.Set("key1", 100)

		value, exists := cache.Get("key1")
		if !exists {
			t.Error("expected key to exist")
		}
		if value != 100 {
			t.Errorf("expected value 100, got %d", value)
		}
	})

	t.Run("delete", func(t *testing.T) {
		cache := New[string, int]()
		defer cache.Close()

		_ = cache.Set("key1", 100)
		cache.Delete("key1")

		_, exists := cache.Get("key1")
		if exists {
			t.Error("expected key to be deleted")
		}
	})

	t.Run("len", func(t *testing.T) {
		cache := New[string, int]()
		defer cache.Close()

		if cache.Len() != 0 {
			t.Error("expected empty cache to have length 0")
		}

		_ = cache.Set("key1", 100)
		if cache.Len() != 1 {
			t.Error("expected cache to have length 1")
		}
	})
}

func TestLRUEviction(t *testing.T) {
	t.Run("capacity eviction", func(t *testing.T) {
		cache := New[string, int](WithCapacity(2))
		defer cache.Close()

		_ = cache.Set("key1", 1)
		_ = cache.Set("key2", 2)
		_ = cache.Set("key3", 3) // should evict key1

		_, exists := cache.Get("key1")
		if exists {
			t.Error("expected key1 to be evicted")
		}

		if _, exists := cache.Get("key2"); !exists {
			t.Error("expected key2 to exist")
		}
		if _, exists := cache.Get("key3"); !exists {
			t.Error("expected key3 to exist")
		}
	})

	t.Run("lru order", func(t *testing.T) {
		cache := New[string, int](WithCapacity(2))
		defer cache.Close()

		_ = cache.Set("key1", 1)
		_ = cache.Set("key2", 2)
		cache.Get("key1")        // moves key1 to front
		_ = cache.Set("key3", 3) // should evict key2

		if _, exists := cache.Get("key2"); exists {
			t.Error("expected key2 to be evicted")
		}
		if _, exists := cache.Get("key1"); !exists {
			t.Error("expected key1 to exist")
		}
	})
}

func TestTTL(t *testing.T) {
	t.Run("expiration", func(t *testing.T) {
		cache := New[string, int](WithTTL(50 * time.Millisecond))
		defer cache.Close()

		_ = cache.Set("key1", 100)
		time.Sleep(100 * time.Millisecond)

		_, exists := cache.Get("key1")
		if exists {
			t.Error("expected key to be expired")
		}
	})

	t.Run("no ttl", func(t *testing.T) {
		cache := New[string, int]()
		defer cache.Close()

		_ = cache.Set("key1", 100)
		time.Sleep(100 * time.Millisecond)

		_, exists := cache.Get("key1")
		if !exists {
			t.Error("expected key to not expire without TTL")
		}
	})
}

func TestConcurrency(t *testing.T) {
	t.Run("concurrent reads", func(_ *testing.T) {
		cache := New[string, int]()
		defer cache.Close()

		_ = cache.Set("key1", 100)

		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = cache.Get("key1")
			}()
		}
		wg.Wait()
	})

	t.Run("concurrent writes", func(t *testing.T) {
		cache := New[string, int]()
		defer cache.Close()

		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)

			go func() {
				defer wg.Done()
				_ = cache.Set("key", i)
			}()
		}
		wg.Wait()

		if cache.Len() != 1 {
			t.Error("expected cache to have length 1")
		}
	})

	t.Run("mixed operations", func(_ *testing.T) {
		cache := New[string, int]()
		defer cache.Close()

		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(2)

			go func() {
				defer wg.Done()
				_ = cache.Set(string(rune(i)), i)
			}()
			go func() {
				defer wg.Done()
				_, _ = cache.Get(string(rune(i)))
			}()
		}
		wg.Wait()
	})
}

func TestConfiguration(t *testing.T) {
	t.Run("default capacity", func(t *testing.T) {
		cache := New[string, int]()
		defer cache.Close()

		// Fill beyond default capacity (1000)
		for i := 0; i < 1001; i++ {
			_ = cache.Set(string(rune(i)), i)
		}

		if cache.Len() > 1000 {
			t.Error("cache exceeded default capacity")
		}
	})

	t.Run("custom capacity", func(t *testing.T) {
		cache := New[string, int](WithCapacity(5))
		defer cache.Close()

		for i := 0; i < 10; i++ {
			_ = cache.Set(string(rune(i)), i)
		}

		if cache.Len() > 5 {
			t.Error("cache exceeded custom capacity")
		}
	})

	t.Run("custom ttl", func(t *testing.T) {
		cache := New[string, int](WithTTL(50 * time.Millisecond))
		defer cache.Close()

		_ = cache.Set("key1", 100)
		time.Sleep(25 * time.Millisecond)

		if _, exists := cache.Get("key1"); !exists {
			t.Error("key should not be expired yet")
		}

		time.Sleep(50 * time.Millisecond)
		if _, exists := cache.Get("key1"); exists {
			t.Error("key should be expired")
		}
	})
}

func TestCleanup(t *testing.T) {
	t.Run("automatic cleanup", func(t *testing.T) {
		// Create cache with a short TTL and cleanup interval
		cache := New[string, int](
			WithTTL(100*time.Millisecond),
			WithGCInterval(50*time.Millisecond),
		)
		defer cache.Close()

		// Add some items
		_ = cache.Set("key1", 1)
		_ = cache.Set("key2", 2)
		_ = cache.Set("key3", 3)

		// Verify items exist
		if cache.Len() != 3 {
			t.Errorf("expected cache to have 3 items, got %d", cache.Len())
		}

		// Wait for TTL + cleanup interval to ensure cleanup runs
		time.Sleep(200 * time.Millisecond)

		// Verify items were cleaned up
		if cache.Len() != 0 {
			t.Errorf("expected cache to be empty after cleanup, got %d items", cache.Len())
		}
	})

	t.Run("cache without TTL", func(t *testing.T) {
		// Create cache with no TTL but with cleanup interval
		cache := New[string, int](
			WithGCInterval(50 * time.Millisecond),
		)
		defer cache.Close()

		// Add some items
		_ = cache.Set("key1", 1)
		_ = cache.Set("key2", 2)

		// Wait for cleanup interval
		time.Sleep(100 * time.Millisecond)

		// Items should still be there (no TTL)
		if cache.Len() != 2 {
			t.Errorf("expected cache to have 2 items, got %d", cache.Len())
		}
	})

	t.Run("mixed expiration", func(t *testing.T) {
		// Create custom cache with TTL
		cache := New[string, int](
			WithTTL(150*time.Millisecond),
			WithGCInterval(50*time.Millisecond),
		)
		defer cache.Close()

		// Add items at different times
		_ = cache.Set("expire-first", 1)
		_ = cache.Set("expire-last", 2)

		// Wait for first cleanup
		time.Sleep(75 * time.Millisecond)

		// Update expire-last to reset its TTL
		_ = cache.Set("expire-last", 2)

		// Wait for first item to expire and be cleaned
		time.Sleep(100 * time.Millisecond)

		// First should be gone, second should remain
		if _, exists := cache.Get("expire-first"); exists {
			t.Error("expected first item to be cleaned up")
		}

		if _, exists := cache.Get("expire-last"); !exists {
			t.Error("expected last item to still exist")
		}

		// Wait for second item to expire
		time.Sleep(200 * time.Millisecond)

		// Both should be gone now
		if cache.Len() != 0 {
			t.Errorf("expected cache to be empty, got %d items", cache.Len())
		}
	})

	t.Run("close stops cleanup", func(_ *testing.T) {
		cache := New[string, int](
			WithTTL(1*time.Hour),
			WithGCInterval(10*time.Millisecond),
		)

		// Add an item with long TTL
		_ = cache.Set("test-key", 1)

		// close the cache (should stop the cleanup goroutine)
		cache.Close()

		// This should not panic
		cache.Close() // Test double-close safety
	})

	t.Run("operations after close", func(t *testing.T) {
		cache := New[string, int]()

		_ = cache.Set("key1", 100)
		cache.Close()

		// Get should return false after close
		_, exists := cache.Get("key1")
		if exists {
			t.Error("expected Get to return false after Close")
		}

		// Set should return error after close
		err := cache.Set("key2", 200)
		if !errors.Is(err, ErrCacheClosed) {
			t.Error("expected Set to return ErrCacheClosed after Close")
		}

		// Len should return 0 after close
		if cache.Len() != 0 {
			t.Error("expected Len to return 0 after Close")
		}
	})
}

// evictionRecorder captures (key, value) pairs delivered through the
// OnEvict callback so tests can assert the full eviction trace without
// racing against the callback's call site.
type evictionRecorder[K comparable, V any] struct {
	calls []evictionCall[K, V]
	mu    sync.Mutex
}

type evictionCall[K comparable, V any] struct {
	key   K
	value V
}

func (r *evictionRecorder[K, V]) record(k K, v V) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, evictionCall[K, V]{key: k, value: v})
}

func (r *evictionRecorder[K, V]) snapshot() []evictionCall[K, V] {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]evictionCall[K, V], len(r.calls))
	copy(out, r.calls)
	return out
}

func (r *evictionRecorder[K, V]) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func TestOnEvict(t *testing.T) {
	t.Run("capacity eviction fires callback", func(t *testing.T) {
		rec := &evictionRecorder[string, int]{}
		cache := New[string, int](
			WithCapacity(2),
			WithOnEvict(rec.record),
		)
		defer cache.Close()

		_ = cache.Set("a", 1)
		_ = cache.Set("b", 2)
		_ = cache.Set("c", 3) // evicts "a"

		calls := rec.snapshot()
		if len(calls) != 1 {
			t.Fatalf("expected 1 eviction, got %d", len(calls))
		}
		if calls[0].key != "a" || calls[0].value != 1 {
			t.Errorf("expected evict(a,1), got evict(%q,%d)", calls[0].key, calls[0].value)
		}
	})

	t.Run("delete fires callback", func(t *testing.T) {
		rec := &evictionRecorder[string, int]{}
		cache := New[string, int](WithOnEvict(rec.record))
		defer cache.Close()

		_ = cache.Set("k", 42)
		cache.Delete("k")

		calls := rec.snapshot()
		if len(calls) != 1 {
			t.Fatalf("expected 1 eviction, got %d", len(calls))
		}
		if calls[0].key != "k" || calls[0].value != 42 {
			t.Errorf("expected evict(k,42), got evict(%q,%d)", calls[0].key, calls[0].value)
		}
	})

	t.Run("delete of missing key does not fire", func(t *testing.T) {
		rec := &evictionRecorder[string, int]{}
		cache := New[string, int](WithOnEvict(rec.record))
		defer cache.Close()

		cache.Delete("nope")
		if n := rec.len(); n != 0 {
			t.Errorf("expected 0 evictions, got %d", n)
		}
	})

	t.Run("ttl expiry via Get fires callback", func(t *testing.T) {
		rec := &evictionRecorder[string, int]{}
		cache := New[string, int](
			WithTTL(20*time.Millisecond),
			WithOnEvict(rec.record),
		)
		defer cache.Close()

		_ = cache.Set("k", 1)
		time.Sleep(50 * time.Millisecond)

		// Get triggers the on-access expiry path.
		if _, ok := cache.Get("k"); ok {
			t.Fatal("expected Get to report expired key as missing")
		}

		calls := rec.snapshot()
		if len(calls) != 1 || calls[0].key != "k" {
			t.Errorf("expected eviction of k via Get, got %+v", calls)
		}
	})

	t.Run("gc cleanup fires callback", func(t *testing.T) {
		rec := &evictionRecorder[string, int]{}
		cache := New[string, int](
			WithTTL(30*time.Millisecond),
			WithGCInterval(20*time.Millisecond),
			WithOnEvict(rec.record),
		)
		defer cache.Close()

		_ = cache.Set("a", 1)
		_ = cache.Set("b", 2)

		// Wait long enough for TTL + at least one GC tick to process both.
		time.Sleep(150 * time.Millisecond)

		if rec.len() != 2 {
			t.Errorf("expected 2 evictions from GC, got %d", rec.len())
		}
		if cache.Len() != 0 {
			t.Errorf("expected cache empty, got %d", cache.Len())
		}
	})

	t.Run("update does not fire callback", func(t *testing.T) {
		rec := &evictionRecorder[string, int]{}
		cache := New[string, int](WithOnEvict(rec.record))
		defer cache.Close()

		_ = cache.Set("k", 1)
		_ = cache.Set("k", 2) // update, not eviction
		_ = cache.Set("k", 3) // update, not eviction

		if n := rec.len(); n != 0 {
			t.Errorf("expected 0 evictions for updates, got %d", n)
		}

		v, ok := cache.Get("k")
		if !ok || v != 3 {
			t.Errorf("expected current value 3, got (%d, %v)", v, ok)
		}
	})

	t.Run("lru order evicts least recently used", func(t *testing.T) {
		rec := &evictionRecorder[string, int]{}
		cache := New[string, int](
			WithCapacity(2),
			WithOnEvict(rec.record),
		)
		defer cache.Close()

		_ = cache.Set("a", 1)
		_ = cache.Set("b", 2)
		cache.Get("a")        // bump a to MRU
		_ = cache.Set("c", 3) // evicts b
		_ = cache.Set("d", 4) // evicts a

		calls := rec.snapshot()
		if len(calls) != 2 {
			t.Fatalf("expected 2 evictions, got %d", len(calls))
		}
		if calls[0].key != "b" {
			t.Errorf("expected first eviction b, got %q", calls[0].key)
		}
		if calls[1].key != "a" {
			t.Errorf("expected second eviction a, got %q", calls[1].key)
		}
	})

	t.Run("concurrent inserts account every eviction", func(t *testing.T) {
		const capacity = 10
		const writers = 1000

		var evicted atomic.Int64
		cache := New[int, int](
			WithCapacity(capacity),
			WithOnEvict(func(_, _ int) {
				evicted.Add(1)
			}),
		)
		defer cache.Close()

		var wg sync.WaitGroup
		wg.Add(writers)
		for i := 0; i < writers; i++ {
			go func(i int) {
				defer wg.Done()
				_ = cache.Set(i, i)
			}(i)
		}
		wg.Wait()

		// Conservation: inserts = evictions + current size.
		if got := int64(writers) - evicted.Load(); got != int64(cache.Len()) {
			t.Errorf("conservation violated: inserts=%d evictions=%d len=%d",
				writers, evicted.Load(), cache.Len())
		}
		if cache.Len() != capacity {
			t.Errorf("expected cache full at %d, got %d", capacity, cache.Len())
		}
	})

	t.Run("callback type mismatch panics at New", func(t *testing.T) {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("expected panic for type-mismatched OnEvict callback")
			}
		}()

		// K=string, V=int cache receiving a callback typed for (int, string).
		_ = New[string, int](WithOnEvict(func(_ int, _ string) {}))
	})

	t.Run("no callback default is a no-op", func(t *testing.T) {
		cache := New[string, int](WithCapacity(1))
		defer cache.Close()

		_ = cache.Set("a", 1)
		_ = cache.Set("b", 2) // evicts "a" with no callback
		cache.Delete("b")

		if cache.Len() != 0 {
			t.Errorf("expected empty cache, got %d", cache.Len())
		}
	})
}
