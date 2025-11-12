package context

import (
	"context"
	"sync"
	"testing"
)

func TestNewAppContext(t *testing.T) {
	ac := NewAppContext()
	if ac == nil {
		t.Fatal("NewAppContext() returned nil")
	}
}

func TestAppContext_WithAndGet(t *testing.T) {
	ac := NewAppContext()

	// Test with string key
	ac = ac.With("key1", "value1")
	if got := ac.Get("key1"); got != "value1" {
		t.Errorf("Get(key1) = %v, want value1", got)
	}

	// Test with *Key
	key := &Key{Name: "test.key"}
	ac = ac.With(key, 42)
	if got := ac.Get(key); got != 42 {
		t.Errorf("Get(key) = %v, want 42", got)
	}

	// Test with struct{} key
	type customKey struct{}
	ac = ac.With(customKey{}, "custom")
	if got := ac.Get(customKey{}); got != "custom" {
		t.Errorf("Get(customKey{}) = %v, want custom", got)
	}

	// Test non-existent key
	if got := ac.Get("nonexistent"); got != nil {
		t.Errorf("Get(nonexistent) = %v, want nil", got)
	}
}

func TestAppContext_WriteOnce(t *testing.T) {
	ac := NewAppContext()

	// Set value once
	ac = ac.With("key1", "value1")

	// Should be able to read
	if got := ac.Get("key1"); got != "value1" {
		t.Errorf("Get(key1) = %v, want value1", got)
	}

	// Setting same key again should panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("With() for existing key should panic")
		} else if r != "cannot overwrite AppContext key: key already set" {
			t.Errorf("panic message = %v, want 'cannot overwrite AppContext key: key already set'", r)
		}
	}()

	ac.With("key1", "value2")
}

func TestAppContext_ConcurrentAccess(t *testing.T) {
	ac := NewAppContext()

	var wg sync.WaitGroup

	// Concurrent writes to different keys
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ac.With(n, n*2)
		}(i)
	}

	wg.Wait()

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			val := ac.Get(n)
			if val != nil && val != n*2 {
				t.Errorf("Get(%d) = %v, want %d", n, val, n*2)
			}
		}(i)
	}

	wg.Wait()
}

func TestWithAppContext(t *testing.T) {
	ac := NewAppContext()
	ac = ac.With("key1", "value1")

	ctx := context.Background()
	ctx = WithAppContext(ctx, ac)

	retrieved := AppFromContext(ctx)
	if retrieved == nil {
		t.Fatal("AppFromContext() returned nil")
	}

	if got := retrieved.Get("key1"); got != "value1" {
		t.Errorf("retrieved.Get(key1) = %v, want value1", got)
	}
}

func TestAppFromContext_NotPresent(t *testing.T) {
	ctx := context.Background()

	retrieved := AppFromContext(ctx)
	if retrieved != nil {
		t.Errorf("AppFromContext() = %v, want nil when not present", retrieved)
	}
}

func TestAppFromContext_WrongType(t *testing.T) {
	ctx := context.Background()
	ctx = context.WithValue(ctx, appContextKey, "not an AppContext")

	retrieved := AppFromContext(ctx)
	if retrieved != nil {
		t.Errorf("AppFromContext() = %v, want nil when wrong type", retrieved)
	}
}

func TestAppContext_MultipleTypes(t *testing.T) {
	ac := NewAppContext()

	// Store different types using chaining
	ac = ac.With("string", "value").
		With("int", 42).
		With("bool", true).
		With("slice", []int{1, 2, 3}).
		With("map", map[string]int{"a": 1})

	// Retrieve and verify
	if got := ac.Get("string").(string); got != "value" {
		t.Errorf("Get(string) = %v, want value", got)
	}
	if got := ac.Get("int").(int); got != 42 {
		t.Errorf("Get(int) = %v, want 42", got)
	}
	if got := ac.Get("bool").(bool); got != true {
		t.Errorf("Get(bool) = %v, want true", got)
	}
	if got := ac.Get("slice").([]int); len(got) != 3 {
		t.Errorf("Get(slice) length = %v, want 3", len(got))
	}
	if got := ac.Get("map").(map[string]int)["a"]; got != 1 {
		t.Errorf("Get(map)[a] = %v, want 1", got)
	}
}
