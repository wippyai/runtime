package lru

import (
	"sync"
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

		cache.Set("key1", 100)

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

		cache.Set("key1", 100)
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

		cache.Set("key1", 100)
		if cache.Len() != 1 {
			t.Error("expected cache to have length 1")
		}
	})
}

func TestLRUEviction(t *testing.T) {
	t.Run("capacity eviction", func(t *testing.T) {
		cache := New[string, int](WithCapacity(2))
		defer cache.Close()

		cache.Set("key1", 1)
		cache.Set("key2", 2)
		cache.Set("key3", 3) // should evict key1

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

		cache.Set("key1", 1)
		cache.Set("key2", 2)
		cache.Get("key1")    // moves key1 to front
		cache.Set("key3", 3) // should evict key2

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

		cache.Set("key1", 100)
		time.Sleep(100 * time.Millisecond)

		_, exists := cache.Get("key1")
		if exists {
			t.Error("expected key to be expired")
		}
	})

	t.Run("no ttl", func(t *testing.T) {
		cache := New[string, int]()
		defer cache.Close()

		cache.Set("key1", 100)
		time.Sleep(100 * time.Millisecond)

		_, exists := cache.Get("key1")
		if !exists {
			t.Error("expected key to not expire without TTL")
		}
	})
}

func TestConcurrency(t *testing.T) {
	t.Run("concurrent reads", func(t *testing.T) {
		cache := New[string, int]()
		defer cache.Close()

		cache.Set("key1", 100)

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
			i := i
			go func() {
				defer wg.Done()
				cache.Set("key", i)
			}()
		}
		wg.Wait()

		if cache.Len() != 1 {
			t.Error("expected cache to have length 1")
		}
	})

	t.Run("mixed operations", func(t *testing.T) {
		cache := New[string, int]()
		defer cache.Close()

		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(2)
			i := i
			go func() {
				defer wg.Done()
				cache.Set(string(rune(i)), i)
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
			cache.Set(string(rune(i)), i)
		}

		if cache.Len() > 1000 {
			t.Error("cache exceeded default capacity")
		}
	})

	t.Run("custom capacity", func(t *testing.T) {
		cache := New[string, int](WithCapacity(5))
		defer cache.Close()

		for i := 0; i < 10; i++ {
			cache.Set(string(rune(i)), i)
		}

		if cache.Len() > 5 {
			t.Error("cache exceeded custom capacity")
		}
	})

	t.Run("custom ttl", func(t *testing.T) {
		cache := New[string, int](WithTTL(50 * time.Millisecond))
		defer cache.Close()

		cache.Set("key1", 100)
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
		cache.Set("key1", 1)
		cache.Set("key2", 2)
		cache.Set("key3", 3)

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
		cache.Set("key1", 1)
		cache.Set("key2", 2)

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
		cache.Set("expire-first", 1)
		cache.Set("expire-last", 2)

		// Wait for first cleanup
		time.Sleep(75 * time.Millisecond)

		// Update expire-last to reset its TTL
		cache.Set("expire-last", 2)

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

	t.Run("close stops cleanup", func(t *testing.T) {
		cache := New[string, int](
			WithTTL(1*time.Hour),
			WithGCInterval(10*time.Millisecond),
		)

		// Add an item with long TTL
		cache.Set("test-key", 1)

		// close the cache (should stop the cleanup goroutine)
		cache.Close()

		// This should not panic
		cache.Close() // Test double-close safety
	})
}
