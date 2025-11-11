package context

import (
	"context"
	"sync"
	"testing"
)

func TestNewCallContext(t *testing.T) {
	ctx, cc := NewCallContext(context.Background())
	if cc == nil {
		t.Fatal("NewCallContext() returned nil CallContext")
	}
	if ctx == nil {
		t.Fatal("NewCallContext() returned nil context")
	}
	if cc.Parent() != nil {
		t.Error("NewCallContext() should have nil parent")
	}
}

func TestCallContext_SetAndGet(t *testing.T) {
	_, cc := NewCallContext(context.Background())

	key1 := &Key{Name: "test.key1", Scope: ScopeCall}
	key2 := &Key{Name: "test.key2", Scope: ScopeThread}

	// Set values
	if err := cc.Set(key1, "value1"); err != nil {
		t.Errorf("Set(key1) error = %v, want nil", err)
	}
	if err := cc.Set(key2, 42); err != nil {
		t.Errorf("Set(key2) error = %v, want nil", err)
	}

	// Get values
	if got, ok := cc.Get(key1); !ok || got != "value1" {
		t.Errorf("Get(key1) = %v, %v, want value1, true", got, ok)
	}
	if got, ok := cc.Get(key2); !ok || got != 42 {
		t.Errorf("Get(key2) = %v, %v, want 42, true", got, ok)
	}

	// Non-existent key
	nonExistentKey := &Key{Name: "nonexistent", Scope: ScopeCall}
	if got, ok := cc.Get(nonExistentKey); ok || got != nil {
		t.Errorf("Get(nonexistent) = %v, %v, want nil, false", got, ok)
	}
}

func TestCallContext_Has(t *testing.T) {
	_, cc := NewCallContext(context.Background())

	key1 := &Key{Name: "test.key1", Scope: ScopeCall}
	key2 := &Key{Name: "test.key2", Scope: ScopeThread}

	if cc.Has(key1) {
		t.Error("Has(key1) = true, want false before Set")
	}

	cc.Set(key1, "value1")

	if !cc.Has(key1) {
		t.Error("Has(key1) = false, want true after Set")
	}
	if cc.Has(key2) {
		t.Error("Has(key2) = true, want false (not set)")
	}
}

func TestCallContext_WriteOnce(t *testing.T) {
	_, cc := NewCallContext(context.Background())

	key := &Key{Name: "test.key", Scope: ScopeCall}

	// First set should succeed
	if err := cc.Set(key, "value1"); err != nil {
		t.Errorf("First Set() error = %v, want nil", err)
	}

	// Second set should fail
	if err := cc.Set(key, "value2"); err == nil {
		t.Error("Second Set() error = nil, want error")
	} else if keyErr, ok := err.(*KeyError); !ok {
		t.Errorf("Second Set() error type = %T, want *KeyError", err)
	} else if keyErr.Key != key {
		t.Errorf("KeyError.Key = %v, want %v", keyErr.Key, key)
	}

	// Value should still be original
	if got, _ := cc.Get(key); got != "value1" {
		t.Errorf("Get(key) = %v, want value1", got)
	}
}

func TestCallContext_Iterate(t *testing.T) {
	_, cc := NewCallContext(context.Background())

	key1 := &Key{Name: "test.key1", Scope: ScopeCall}
	key2 := &Key{Name: "test.key2", Scope: ScopeThread}
	key3 := &Key{Name: "test.key3", Scope: ScopeCall}

	expected := map[*Key]any{
		key1: "value1",
		key2: 42,
		key3: true,
	}

	for k, v := range expected {
		cc.Set(k, v)
	}

	collected := make(map[*Key]any)
	cc.Iterate(func(key *Key, value any) {
		collected[key] = value
	})

	if len(collected) != len(expected) {
		t.Errorf("Iterate() collected %d items, want %d", len(collected), len(expected))
	}

	for k, v := range expected {
		if collected[k] != v {
			t.Errorf("Iterate() key %s = %v, want %v", k.Name, collected[k], v)
		}
	}
}

func TestCallContext_IterateEmpty(t *testing.T) {
	_, cc := NewCallContext(context.Background())

	count := 0
	cc.Iterate(func(key *Key, value any) {
		count++
	})

	if count != 0 {
		t.Errorf("Iterate() on empty context called fn %d times, want 0", count)
	}
}

func TestCallContext_ScopeInheritance(t *testing.T) {
	// Create parent with both ScopeCall and ScopeThread keys
	parentCtx, parent := NewCallContext(context.Background())

	callKey := &Key{Name: "test.call", Scope: ScopeCall}
	threadKey := &Key{Name: "test.thread", Scope: ScopeThread}

	parent.Set(callKey, "call_value")
	parent.Set(threadKey, "thread_value")

	// Create child from parent
	_, child := NewCallContext(parentCtx)

	// ScopeCall key should NOT be inherited
	if child.Has(callKey) {
		t.Error("child.Has(callKey) = true, want false (ScopeCall not inherited)")
	}
	if got, ok := child.Get(callKey); ok {
		t.Errorf("child.Get(callKey) = %v, %v, want nil, false", got, ok)
	}

	// ScopeThread key SHOULD be inherited
	if !child.Has(threadKey) {
		t.Error("child.Has(threadKey) = false, want true (ScopeThread inherited)")
	}
	if got, ok := child.Get(threadKey); !ok || got != "thread_value" {
		t.Errorf("child.Get(threadKey) = %v, %v, want thread_value, true", got, ok)
	}

	// Verify parent reference
	if child.Parent() != parent {
		t.Error("child.Parent() != parent")
	}
}

func TestCallContext_Parent(t *testing.T) {
	parentCtx, parent := NewCallContext(context.Background())
	parentKey := &Key{Name: "parent.key", Scope: ScopeThread}
	parent.Set(parentKey, "parent_value")

	_, child := NewCallContext(parentCtx)
	childKey := &Key{Name: "child.key", Scope: ScopeThread}
	child.Set(childKey, "child_value")

	// Verify parent reference
	if child.Parent() == nil {
		t.Fatal("child.Parent() = nil, want parent")
	}

	// Verify parent has its values
	if got, ok := child.Parent().Get(parentKey); !ok || got != "parent_value" {
		t.Errorf("child.Parent().Get(parentKey) = %v, %v, want parent_value, true", got, ok)
	}

	// Child should have inherited parent's ScopeThread key
	if got, ok := child.Get(parentKey); !ok || got != "parent_value" {
		t.Errorf("child.Get(parentKey) = %v, %v, want parent_value, true (inherited)", got, ok)
	}

	// Verify child has its own values
	if got, ok := child.Get(childKey); !ok || got != "child_value" {
		t.Errorf("child.Get(childKey) = %v, %v, want child_value, true", got, ok)
	}
}

func TestCallContext_ConcurrentAccess(t *testing.T) {
	_, cc := NewCallContext(context.Background())

	var wg sync.WaitGroup

	// Concurrent writes to different keys
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := &Key{Name: "test.key" + string(rune(n)), Scope: ScopeCall}
			cc.Set(key, n*2)
		}(i)
	}

	wg.Wait()

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := &Key{Name: "test.key" + string(rune(n)), Scope: ScopeCall}
			val, ok := cc.Get(key)
			if ok && val != n*2 {
				t.Errorf("Get(key%d) = %v, want %d", n, val, n*2)
			}
		}(i)
	}

	wg.Wait()
}

func TestWithCallContext(t *testing.T) {
	_, cc := NewCallContext(context.Background())
	key := &Key{Name: "test.key", Scope: ScopeCall}
	cc.Set(key, "value1")

	ctx := context.Background()
	ctx = WithCallContext(ctx, cc)

	retrieved := CallFromContext(ctx)
	if retrieved == nil {
		t.Fatal("CallFromContext() returned nil")
	}

	if got, ok := retrieved.Get(key); !ok || got != "value1" {
		t.Errorf("retrieved.Get(key) = %v, %v, want value1, true", got, ok)
	}
}

func TestCallFromContext_NotPresent(t *testing.T) {
	ctx := context.Background()

	retrieved := CallFromContext(ctx)
	if retrieved != nil {
		t.Error("CallFromContext() should return nil when not present")
	}
}

func TestCallFromContext_WrongType(t *testing.T) {
	ctx := context.Background()
	ctx = context.WithValue(ctx, callContextKey, "not a CallContext")

	retrieved := CallFromContext(ctx)
	if retrieved != nil {
		t.Error("CallFromContext() should return nil for wrong type")
	}
}

func TestCallContext_MultipleTypes(t *testing.T) {
	_, cc := NewCallContext(context.Background())

	stringKey := &Key{Name: "test.string", Scope: ScopeCall}
	intKey := &Key{Name: "test.int", Scope: ScopeCall}
	boolKey := &Key{Name: "test.bool", Scope: ScopeCall}
	sliceKey := &Key{Name: "test.slice", Scope: ScopeCall}
	mapKey := &Key{Name: "test.map", Scope: ScopeCall}

	// Store different types
	cc.Set(stringKey, "value")
	cc.Set(intKey, 42)
	cc.Set(boolKey, true)
	cc.Set(sliceKey, []int{1, 2, 3})
	cc.Set(mapKey, map[string]int{"a": 1})

	// Retrieve and verify
	if got, _ := cc.Get(stringKey); got.(string) != "value" {
		t.Errorf("Get(string) = %v, want value", got)
	}
	if got, _ := cc.Get(intKey); got.(int) != 42 {
		t.Errorf("Get(int) = %v, want 42", got)
	}
	if got, _ := cc.Get(boolKey); got.(bool) != true {
		t.Errorf("Get(bool) = %v, want true", got)
	}
	if got, _ := cc.Get(sliceKey); len(got.([]int)) != 3 {
		t.Errorf("Get(slice) length = %v, want 3", len(got.([]int)))
	}
	if got, _ := cc.Get(mapKey); got.(map[string]int)["a"] != 1 {
		t.Errorf("Get(map)[a] = %v, want 1", got.(map[string]int)["a"])
	}
}

func TestCallContext_InheritanceDoesNotAffectParent(t *testing.T) {
	parentCtx, parent := NewCallContext(context.Background())
	threadKey := &Key{Name: "test.thread", Scope: ScopeThread}
	parent.Set(threadKey, "original")

	_, child := NewCallContext(parentCtx)

	// Child inherited the value
	if got, _ := child.Get(threadKey); got != "original" {
		t.Errorf("child.Get(threadKey) = %v, want original", got)
	}

	// Try to overwrite in child (should fail - write-once)
	if err := child.Set(threadKey, "modified"); err == nil {
		t.Error("child.Set(threadKey) should fail (already inherited)")
	}

	// Parent should still have original value
	if got, _ := parent.Get(threadKey); got != "original" {
		t.Errorf("parent.Get(threadKey) = %v, want original", got)
	}
}
