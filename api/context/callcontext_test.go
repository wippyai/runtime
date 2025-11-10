package context

import (
	"context"
	"sync"
	"testing"
)

func TestNewCallContext(t *testing.T) {
	cc := NewCallContext()
	if cc == nil {
		t.Fatal("NewCallContext() returned nil")
	}
	if cc.Parent() != nil {
		t.Error("NewCallContext() should have nil parent")
	}
}

func TestCallContext_SetAndGet(t *testing.T) {
	cc := NewCallContext()

	// Test with string key
	cc.Set("key1", "value1")
	if got := cc.Get("key1"); got != "value1" {
		t.Errorf("Get(key1) = %v, want value1", got)
	}

	// Test with *Key
	key := &Key{Name: "test.key"}
	cc.Set(key, 42)
	if got := cc.Get(key); got != 42 {
		t.Errorf("Get(key) = %v, want 42", got)
	}

	// Test with struct{} key
	type customKey struct{}
	cc.Set(customKey{}, "custom")
	if got := cc.Get(customKey{}); got != "custom" {
		t.Errorf("Get(customKey{}) = %v, want custom", got)
	}

	// Test non-existent key
	if got := cc.Get("nonexistent"); got != nil {
		t.Errorf("Get(nonexistent) = %v, want nil", got)
	}
}

func TestCallContext_Iterate(t *testing.T) {
	cc := NewCallContext()

	expected := map[string]any{
		"key1": "value1",
		"key2": 42,
		"key3": true,
	}

	for k, v := range expected {
		cc.Set(k, v)
	}

	collected := make(map[string]any)
	cc.Iterate(func(key any, value any) {
		if k, ok := key.(string); ok {
			collected[k] = value
		}
	})

	if len(collected) != len(expected) {
		t.Errorf("Iterate() collected %d items, want %d", len(collected), len(expected))
	}

	for k, v := range expected {
		if collected[k] != v {
			t.Errorf("Iterate() key %s = %v, want %v", k, collected[k], v)
		}
	}
}

func TestCallContext_IterateEmpty(t *testing.T) {
	cc := NewCallContext()

	count := 0
	cc.Iterate(func(key any, value any) {
		count++
	})

	if count != 0 {
		t.Errorf("Iterate() on empty context called fn %d times, want 0", count)
	}
}

func TestCallContext_Parent(t *testing.T) {
	parent := NewCallContext()
	parent.Set("parent_key", "parent_value")

	child := NewCallContext()
	child.Set("child_key", "child_value")

	// Set parent
	child = child.WithParent(parent)

	// Verify parent reference
	if child.Parent() == nil {
		t.Fatal("Parent() = nil, want parent")
	}

	// Verify parent has its values
	if got := child.Parent().Get("parent_key"); got != "parent_value" {
		t.Errorf("Parent().Get(parent_key) = %v, want parent_value", got)
	}

	// Verify child has its own values
	if got := child.Get("child_key"); got != "child_value" {
		t.Errorf("Get(child_key) = %v, want child_value", got)
	}
}

func TestCallContext_GetDoesNotWalkParent(t *testing.T) {
	parent := NewCallContext()
	parent.Set("key", "parent_value")

	child := NewCallContext()
	child = child.WithParent(parent)

	// Child Get() should NOT find parent's value
	if got := child.Get("key"); got != nil {
		t.Errorf("child.Get(key) = %v, want nil (should not walk parent)", got)
	}

	// Manual parent lookup should work
	if got := child.Parent().Get("key"); got != "parent_value" {
		t.Errorf("child.Parent().Get(key) = %v, want parent_value", got)
	}
}

func TestCallContext_WithParent(t *testing.T) {
	parent := NewCallContext()
	parent.Set("parent_key", "parent_value")

	original := NewCallContext()
	original.Set("key1", "value1")

	// WithParent returns new instance
	withParent := original.WithParent(parent)

	// Verify new instance has parent
	if withParent.Parent() != parent {
		t.Error("WithParent() did not set parent correctly")
	}

	// Verify new instance shares values map
	if got := withParent.Get("key1"); got != "value1" {
		t.Errorf("withParent.Get(key1) = %v, want value1", got)
	}

	// Modify original, should affect withParent (shared map)
	original.Set("key2", "value2")
	if got := withParent.Get("key2"); got != "value2" {
		t.Errorf("withParent.Get(key2) = %v, want value2 (shared map)", got)
	}
}

func TestCallContext_ConcurrentAccess(t *testing.T) {
	cc := NewCallContext()

	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			cc.Set(n, n*2)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			val := cc.Get(n)
			if val != nil && val != n*2 {
				t.Errorf("Get(%d) = %v, want %d", n, val, n*2)
			}
		}(i)
	}

	wg.Wait()
}

func TestWithCallContext(t *testing.T) {
	cc := NewCallContext()
	cc.Set("key1", "value1")

	ctx := context.Background()
	ctx = WithCallContext(ctx, cc)

	retrieved := CallFromContext(ctx)
	if retrieved == nil {
		t.Fatal("CallFromContext() returned nil")
	}

	if got := retrieved.Get("key1"); got != "value1" {
		t.Errorf("retrieved.Get(key1) = %v, want value1", got)
	}
}

func TestCallFromContext_NotPresent(t *testing.T) {
	ctx := context.Background()

	retrieved := CallFromContext(ctx)
	if retrieved == nil {
		t.Error("CallFromContext() returned nil, want empty CallContext")
	}

	// Should be empty
	if got := retrieved.Get("anything"); got != nil {
		t.Errorf("empty CallContext.Get() = %v, want nil", got)
	}
}

func TestCallFromContext_WrongType(t *testing.T) {
	ctx := context.Background()
	ctx = context.WithValue(ctx, callContextKey, "not a CallContext")

	retrieved := CallFromContext(ctx)
	if retrieved == nil {
		t.Error("CallFromContext() returned nil, want empty CallContext")
	}
}

func TestWithoutCallContext(t *testing.T) {
	cc := NewCallContext()
	cc.Set("key1", "value1")

	ctx := context.Background()
	ctx = WithCallContext(ctx, cc)

	// Verify it's there
	if got := CallFromContext(ctx).Get("key1"); got != "value1" {
		t.Errorf("Before WithoutCallContext: Get(key1) = %v, want value1", got)
	}

	// Remove CallContext
	ctx = WithoutCallContext(ctx)

	// Should return empty CallContext
	retrieved := CallFromContext(ctx)
	if got := retrieved.Get("key1"); got != nil {
		t.Errorf("After WithoutCallContext: Get(key1) = %v, want nil", got)
	}
}

func TestCopyCallContext(t *testing.T) {
	original := NewCallContext()
	original.Set("key1", "value1")
	original.Set("key2", 42)

	parent := NewCallContext()
	parent.Set("parent_key", "parent_value")
	original = original.WithParent(parent)

	// Copy it
	copied := CopyCallContext(original)

	// Verify copied has same values
	if got := copied.Get("key1"); got != "value1" {
		t.Errorf("copied.Get(key1) = %v, want value1", got)
	}
	if got := copied.Get("key2"); got != 42 {
		t.Errorf("copied.Get(key2) = %v, want 42", got)
	}

	// Verify parent is NOT copied
	if copied.Parent() != nil {
		t.Error("CopyCallContext() should not copy parent")
	}

	// Modify original
	original.Set("key3", "value3")

	// Copied should NOT have new value (independent)
	if got := copied.Get("key3"); got != nil {
		t.Errorf("copied.Get(key3) = %v, want nil (should be independent)", got)
	}
}

func TestCallContext_MultipleTypes(t *testing.T) {
	cc := NewCallContext()

	// Store different types
	cc.Set("string", "value")
	cc.Set("int", 42)
	cc.Set("bool", true)
	cc.Set("slice", []int{1, 2, 3})
	cc.Set("map", map[string]int{"a": 1})

	// Retrieve and verify
	if got := cc.Get("string").(string); got != "value" {
		t.Errorf("Get(string) = %v, want value", got)
	}
	if got := cc.Get("int").(int); got != 42 {
		t.Errorf("Get(int) = %v, want 42", got)
	}
	if got := cc.Get("bool").(bool); got != true {
		t.Errorf("Get(bool) = %v, want true", got)
	}
	if got := cc.Get("slice").([]int); len(got) != 3 {
		t.Errorf("Get(slice) length = %v, want 3", len(got))
	}
	if got := cc.Get("map").(map[string]int)["a"]; got != 1 {
		t.Errorf("Get(map)[a] = %v, want 1", got)
	}
}
