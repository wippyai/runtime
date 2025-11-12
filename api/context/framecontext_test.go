package context

import (
	"context"
	"sync"
	"testing"
)

func TestNewFrameContext(t *testing.T) {
	ctx, cc := newFrameContext(context.Background())
	if cc == nil {
		t.Fatal("newFrameContext() returned nil FrameContext")
	}
	if ctx == nil {
		t.Fatal("newFrameContext() returned nil context")
	}
	if cc.Parent() != nil {
		t.Error("newFrameContext() should have nil parent")
	}
}

func TestFrameContext_SetAndGet(t *testing.T) {
	_, cc := newFrameContext(context.Background())

	key1 := &Key{Name: "test.key1"}
	key2 := &Key{Name: "test.key2"}

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
	nonExistentKey := &Key{Name: "nonexistent"}
	if got, ok := cc.Get(nonExistentKey); ok || got != nil {
		t.Errorf("Get(nonexistent) = %v, %v, want nil, false", got, ok)
	}
}

func TestFrameContext_Has(t *testing.T) {
	_, cc := newFrameContext(context.Background())

	key1 := &Key{Name: "test.key1"}
	key2 := &Key{Name: "test.key2"}

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

func TestFrameContext_Iterate(t *testing.T) {
	_, cc := newFrameContext(context.Background())

	key1 := &Key{Name: "test.key1"}
	key2 := &Key{Name: "test.key2"}
	key3 := &Key{Name: "test.key3"}

	expected := map[*Key]any{
		key1: "value1",
		key2: 42,
		key3: true,
	}

	for k, v := range expected {
		cc.Set(k, v)
	}

	collected := make(map[*Key]any)
	cc.Iterate(func(key any, value any) {
		collected[key.(*Key)] = value
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

func TestFrameContext_IterateEmpty(t *testing.T) {
	_, cc := newFrameContext(context.Background())

	count := 0
	cc.Iterate(func(key any, value any) {
		count++
	})

	if count != 0 {
		t.Errorf("Iterate() on empty context called fn %d times, want 0", count)
	}
}

func TestFrameContext_ScopeInheritance(t *testing.T) {
	// Test inheritance via OpenFrameContext with sealed parent
	parentCtx, parent := newFrameContext(context.Background())

	nonInheritKey := &Key{Name: "test.noninherit", Inherit: false}
	inheritKey := &Key{Name: "test.inherit", Inherit: true}

	parent.Set(nonInheritKey, "noninherit_value")
	parent.Set(inheritKey, "inherit_value")

	// Seal parent to trigger inheritance
	parent.Seal()

	// Use OpenFrameContext - should auto-inherit keys with Inherit=true
	_, child := OpenFrameContext(parentCtx)

	// Non-inherit key should NOT be inherited
	if child.Has(nonInheritKey) {
		t.Error("child.Has(nonInheritKey) = true, want false (not inheritable)")
	}
	if got, ok := child.Get(nonInheritKey); ok {
		t.Errorf("child.Get(nonInheritKey) = %v, %v, want nil, false", got, ok)
	}

	// Inherit key SHOULD be inherited
	if !child.Has(inheritKey) {
		t.Error("child.Has(inheritKey) = false, want true (inheritable)")
	}
	if got, ok := child.Get(inheritKey); !ok || got != "inherit_value" {
		t.Errorf("child.Get(inheritKey) = %v, %v, want inherit_value, true", got, ok)
	}

	// Verify parent reference
	if child.Parent() != parent {
		t.Error("child.Parent() != parent")
	}
}

func TestFrameContext_Parent(t *testing.T) {
	parentCtx, parent := newFrameContext(context.Background())
	parentKey := &Key{Name: "parent.key"}
	parent.Set(parentKey, "parent_value")

	_, child := newFrameContext(parentCtx)
	childKey := &Key{Name: "child.key"}
	child.Set(childKey, "child_value")

	// Verify parent reference
	if child.Parent() == nil {
		t.Fatal("child.Parent() = nil, want parent")
	}

	// Verify parent has its values
	if got, ok := child.Parent().Get(parentKey); !ok || got != "parent_value" {
		t.Errorf("child.Parent().Get(parentKey) = %v, %v, want parent_value, true", got, ok)
	}

	// Child should NOT have parent's value (no auto-inheritance via newFrameContext)
	if got, ok := child.Get(parentKey); ok {
		t.Errorf("child.Get(parentKey) = %v, %v, want nil, false (not inherited)", got, ok)
	}

	// Verify child has its own values
	if got, ok := child.Get(childKey); !ok || got != "child_value" {
		t.Errorf("child.Get(childKey) = %v, %v, want child_value, true", got, ok)
	}
}

func TestFrameContext_ConcurrentAccess(t *testing.T) {
	_, cc := newFrameContext(context.Background())

	var wg sync.WaitGroup

	// Concurrent writes to different keys
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := &Key{Name: "test.key" + string(rune(n))}
			cc.Set(key, n*2)
		}(i)
	}

	wg.Wait()

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := &Key{Name: "test.key" + string(rune(n))}
			val, ok := cc.Get(key)
			if ok && val != n*2 {
				t.Errorf("Get(key%d) = %v, want %d", n, val, n*2)
			}
		}(i)
	}

	wg.Wait()
}

func TestWithFrameContext(t *testing.T) {
	_, cc := newFrameContext(context.Background())
	key := &Key{Name: "test.key"}
	cc.Set(key, "value1")

	ctx := context.Background()
	ctx = WithFrameContext(ctx, cc)

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
	ctx = context.WithValue(ctx, frameContextKey, "not a FrameContext")

	retrieved := CallFromContext(ctx)
	if retrieved != nil {
		t.Error("CallFromContext() should return nil for wrong type")
	}
}

func TestFrameContext_MultipleTypes(t *testing.T) {
	_, cc := newFrameContext(context.Background())

	stringKey := &Key{Name: "test.string"}
	intKey := &Key{Name: "test.int"}
	boolKey := &Key{Name: "test.bool"}
	sliceKey := &Key{Name: "test.slice"}
	mapKey := &Key{Name: "test.map"}

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

func TestFrameContext_InheritanceDoesNotAffectParent(t *testing.T) {
	parentCtx, parent := newFrameContext(context.Background())
	testKey := &Key{Name: "test.key"}
	parent.Set(testKey, "original")

	_, child := newFrameContext(parentCtx)

	// Child does NOT inherit by default (no auto-inheritance)
	if got, ok := child.Get(testKey); ok {
		t.Errorf("child.Get(testKey) = %v, %v, want nil, false (not inherited)", got, ok)
	}

	// Child can set its own value
	if err := child.Set(testKey, "child_value"); err != nil {
		t.Errorf("child.Set(testKey) error = %v, want nil", err)
	}

	// Parent should still have original value
	if got, _ := parent.Get(testKey); got != "original" {
		t.Errorf("parent.Get(testKey) = %v, want original", got)
	}

	// Child should have its own value
	if got, _ := child.Get(testKey); got != "child_value" {
		t.Errorf("child.Get(testKey) = %v, want child_value", got)
	}
}
