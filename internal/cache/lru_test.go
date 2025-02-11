package lru

import (
	"sync"
	"testing"
	"time"
)

func TestBasicOperations(t *testing.T) {
	t.Run("empty cache", func(t *testing.T) {
		cache := New[string, int]()
		_, exists := cache.Get("nonexistent")
		if exists {
			t.Error("expected Get on empty cache to return false")
		}
	})

	t.Run("set and get", func(t *testing.T) {
		cache := New[string, int]()
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
		cache.Set("key1", 100)
		cache.Delete("key1")

		_, exists := cache.Get("key1")
		if exists {
			t.Error("expected key to be deleted")
		}
	})

	t.Run("len", func(t *testing.T) {
		cache := New[string, int]()
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

		cache.Set("key1", 100)
		time.Sleep(100 * time.Millisecond)

		_, exists := cache.Get("key1")
		if exists {
			t.Error("expected key to be expired")
		}
	})

	t.Run("no ttl", func(t *testing.T) {
		cache := New[string, int]()

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

		for i := 0; i < 10; i++ {
			cache.Set(string(rune(i)), i)
		}

		if cache.Len() > 5 {
			t.Error("cache exceeded custom capacity")
		}
	})

	t.Run("custom ttl", func(t *testing.T) {
		cache := New[string, int](WithTTL(50 * time.Millisecond))

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
