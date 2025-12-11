// Package context provides application-level context management utilities.
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

	_ = cc.Set(key1, "value1")

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
		_ = cc.Set(k, v)
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
	cc.Iterate(func(_ any, _ any) {
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

	_ = parent.Set(nonInheritKey, "noninherit_value")
	_ = parent.Set(inheritKey, "inherit_value")

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
			_ = cc.Set(key, n*2)
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
	_ = cc.Set(key, "value1")

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
	_ = cc.Set(stringKey, "value")
	_ = cc.Set(intKey, 42)
	_ = cc.Set(boolKey, true)
	_ = cc.Set(sliceKey, []int{1, 2, 3})
	_ = cc.Set(mapKey, map[string]int{"a": 1})

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
	_ = parent.Set(testKey, "original")

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

func TestFrameContext_Seal(t *testing.T) {
	_, fc := newFrameContext(context.Background())
	key := &Key{Name: "test.key"}

	if fc.IsSealed() {
		t.Error("new frame should not be sealed")
	}

	_ = fc.Set(key, "value1")
	fc.Seal()

	if !fc.IsSealed() {
		t.Error("frame should be sealed after Seal()")
	}

	err := fc.Set(key, "value2")
	if err == nil {
		t.Error("Set() on sealed frame should return error")
	}
}

func TestFrameContext_SetMultiple(t *testing.T) {
	_, fc := newFrameContext(context.Background())

	key1 := &Key{Name: "key1"}
	key2 := &Key{Name: "key2"}
	key3 := &Key{Name: "key3"}

	pairs := []Pair{
		{Key: key1, Value: "value1"},
		{Key: key2, Value: 42},
		{Key: key3, Value: true},
	}

	err := fc.SetMultiple(pairs...)
	if err != nil {
		t.Errorf("SetMultiple() error = %v, want nil", err)
	}

	if val, ok := fc.Get(key1); !ok || val != "value1" {
		t.Errorf("Get(key1) = %v, %v, want value1, true", val, ok)
	}
	if val, ok := fc.Get(key2); !ok || val != 42 {
		t.Errorf("Get(key2) = %v, %v, want 42, true", val, ok)
	}
	if val, ok := fc.Get(key3); !ok || val != true {
		t.Errorf("Get(key3) = %v, %v, want true, true", val, ok)
	}
}

func TestFrameContext_SetMultipleSealed(t *testing.T) {
	_, fc := newFrameContext(context.Background())
	fc.Seal()

	key1 := &Key{Name: "key1"}
	pairs := []Pair{{Key: key1, Value: "value1"}}

	err := fc.SetMultiple(pairs...)
	if err == nil {
		t.Error("SetMultiple() on sealed frame should return error")
	}
}

func TestOpenFrameContext_Unsealed(t *testing.T) {
	ctx, fc := newFrameContext(context.Background())
	key := &Key{Name: "test.key"}
	_ = fc.Set(key, "value1")

	newCtx, newFC := OpenFrameContext(ctx)
	if newFC != fc {
		t.Error("OpenFrameContext should return existing unsealed frame")
	}
	if newCtx != ctx {
		t.Error("OpenFrameContext should return same context for unsealed frame")
	}

	if val, ok := newFC.Get(key); !ok || val != "value1" {
		t.Errorf("OpenFrameContext returned frame should have existing values")
	}
}

func TestOpenFrameContext_NilFrame(t *testing.T) {
	ctx := context.Background()
	newCtx, fc := OpenFrameContext(ctx)

	if fc == nil {
		t.Fatal("OpenFrameContext should create new frame when none exists")
	}
	if newCtx == ctx {
		t.Error("OpenFrameContext should return new context when creating frame")
	}
}

func TestOpenFrameContext_InheritWithCloner(t *testing.T) {
	parentCtx, parent := newFrameContext(context.Background())

	type testCloner struct {
		value string
	}

	cloner := &testCloner{value: "original"}

	inheritKey := &Key{Name: "test.cloner", Inherit: true}
	_ = parent.Set(inheritKey, cloner)
	parent.Seal()

	_, child := OpenFrameContext(parentCtx)

	val, ok := child.Get(inheritKey)
	if !ok {
		t.Fatal("child should inherit key")
	}

	inheritedVal, ok := val.(*testCloner)
	if !ok {
		t.Fatal("inherited value should be *testCloner")
	}

	if inheritedVal.value != "original" {
		t.Errorf("inherited value = %v, want original", inheritedVal.value)
	}
}

func TestOpenFrameContext_InheritWithValuesCloner(t *testing.T) {
	parentCtx, parent := newFrameContext(context.Background())

	values := NewValues()
	values.Set("key1", "value1")

	inheritKey := &Key{Name: "test.values", Inherit: true}
	_ = parent.Set(inheritKey, values)
	parent.Seal()

	_, child := OpenFrameContext(parentCtx)

	val, ok := child.Get(inheritKey)
	if !ok {
		t.Fatal("child should inherit Values")
	}

	clonedValues, ok := val.(Values)
	if !ok {
		t.Fatal("inherited value should be Values")
	}

	if got, _ := clonedValues.Get("key1"); got != "value1" {
		t.Errorf("cloned Values.Get(key1) = %v, want value1", got)
	}

	clonedValues.Set("key2", "value2")
	if got, ok := values.Get("key2"); ok {
		t.Errorf("parent Values should not have child's new values, got %v", got)
	}
}

// Reference counting tests for frame pooling safety in polyglot runtime.
// The mechanism exists because in polyglot runtimes (Lua, WebAssembly, Temporal workflows),
// the context propagation path is not deterministic and context cancellation doesn't
// always work immediately, which complicates knowing when frames can be safely pooled.

func TestFrameContext_RefCount_ParentReleasesFirst(t *testing.T) {
	parentCtx, parent := newFrameContext(context.Background())
	inheritKey := &Key{Name: "test.actor", Inherit: true}
	_ = parent.Set(inheritKey, "user123")
	parent.Seal()

	_, child := OpenFrameContext(parentCtx)

	parentFC := parent.(*frameContext)
	if parentFC.refcount.Load() != 2 {
		t.Errorf("parent refcount should be 2 (self + child), got %d", parentFC.refcount.Load())
	}

	ReleaseFrameContext(parent)

	if parentFC.refcount.Load() != 1 {
		t.Errorf("parent refcount after release should be 1 (child still holds ref), got %d", parentFC.refcount.Load())
	}

	val, ok := child.Get(inheritKey)
	if !ok || val != "user123" {
		t.Errorf("child should still see inherited value after parent release, got %v", val)
	}

	ReleaseFrameContext(child)
}

func TestFrameContext_RefCount_ChildReleasesFirst(t *testing.T) {
	parentCtx, parent := newFrameContext(context.Background())
	inheritKey := &Key{Name: "test.actor", Inherit: true}
	_ = parent.Set(inheritKey, "user123")
	parent.Seal()

	_, child := OpenFrameContext(parentCtx)
	parentFC := parent.(*frameContext)

	if parentFC.refcount.Load() != 2 {
		t.Errorf("parent refcount should be 2, got %d", parentFC.refcount.Load())
	}

	ReleaseFrameContext(child)

	if parentFC.refcount.Load() != 1 {
		t.Errorf("parent refcount after child release should be 1, got %d", parentFC.refcount.Load())
	}

	ReleaseFrameContext(parent)
}

func TestFrameContext_RefCount_MultipleChildren(t *testing.T) {
	parentCtx, parent := newFrameContext(context.Background())
	inheritKey := &Key{Name: "test.actor", Inherit: true}
	_ = parent.Set(inheritKey, "user123")
	parent.Seal()

	_, child1 := OpenFrameContext(parentCtx)
	_, child2 := OpenFrameContext(parentCtx)
	_, child3 := OpenFrameContext(parentCtx)

	parentFC := parent.(*frameContext)
	if parentFC.refcount.Load() != 4 {
		t.Errorf("parent refcount should be 4 (self + 3 children), got %d", parentFC.refcount.Load())
	}

	ReleaseFrameContext(parent)
	if parentFC.refcount.Load() != 3 {
		t.Errorf("parent refcount after parent.Release() should be 3, got %d", parentFC.refcount.Load())
	}

	ReleaseFrameContext(child1)
	if parentFC.refcount.Load() != 2 {
		t.Errorf("parent refcount after child1.Release() should be 2, got %d", parentFC.refcount.Load())
	}

	ReleaseFrameContext(child2)
	ReleaseFrameContext(child3)
}

func TestFrameContext_RefCount_ChainCollapse(t *testing.T) {
	grandparentCtx, grandparent := newFrameContext(context.Background())
	inheritKey := &Key{Name: "test.actor", Inherit: true}
	_ = grandparent.Set(inheritKey, "admin")
	grandparent.Seal()

	parentCtx, parent := OpenFrameContext(grandparentCtx)
	inheritScope := &Key{Name: "test.scope", Inherit: true}
	_ = parent.Set(inheritScope, "read")
	parent.Seal()

	_, child := OpenFrameContext(parentCtx)

	val, ok := child.Get(inheritKey)
	if !ok || val != "admin" {
		t.Errorf("child should inherit from grandparent, got %v", val)
	}
	val, ok = child.Get(inheritScope)
	if !ok || val != "read" {
		t.Errorf("child should inherit scope from parent, got %v", val)
	}

	ReleaseFrameContext(grandparent)
	ReleaseFrameContext(parent)
	ReleaseFrameContext(child)
}

func TestFrameContext_RefCount_AsyncForkBeforeGoroutine(t *testing.T) {
	parentCtx, parent := newFrameContext(context.Background())
	inheritKey := &Key{Name: "test.actor", Inherit: true}
	_ = parent.Set(inheritKey, "async_user")
	parent.Seal()

	var wg sync.WaitGroup
	wg.Add(1)

	_, child := OpenFrameContext(parentCtx)

	go func() {
		defer wg.Done()
		val, ok := child.Get(inheritKey)
		if !ok || val != "async_user" {
			t.Errorf("async child should see inherited value, got %v", val)
		}
		ReleaseFrameContext(child)
	}()

	ReleaseFrameContext(parent)

	wg.Wait()
}

func TestFrameContext_RefCount_ConcurrentForks(t *testing.T) {
	parentCtx, parent := newFrameContext(context.Background())
	inheritKey := &Key{Name: "test.actor", Inherit: true}
	scopeKey := &Key{Name: "test.scope", Inherit: true}
	_ = parent.Set(inheritKey, "stress_user")
	_ = parent.Set(scopeKey, "admin")
	parent.Seal()

	const numChildren = 50
	var wg sync.WaitGroup
	wg.Add(numChildren)

	children := make([]FrameContext, numChildren)
	for i := 0; i < numChildren; i++ {
		_, children[i] = OpenFrameContext(parentCtx)
	}

	parentFC := parent.(*frameContext)
	expectedRefcount := int32(numChildren + 1)
	if parentFC.refcount.Load() != expectedRefcount {
		t.Errorf("parent refcount should be %d, got %d", expectedRefcount, parentFC.refcount.Load())
	}

	for i := 0; i < numChildren; i++ {
		go func(idx int) {
			defer wg.Done()
			child := children[idx]

			actor, _ := child.Get(inheritKey)
			scope, _ := child.Get(scopeKey)
			if actor != "stress_user" || scope != "admin" {
				t.Errorf("child %d: bad inherited values: actor=%v scope=%v", idx, actor, scope)
			}

			ReleaseFrameContext(child)
		}(i)
	}

	ReleaseFrameContext(parent)

	wg.Wait()
}

func TestFrameContext_RefCount_NestedAsyncOperations(t *testing.T) {
	rootCtx, root := newFrameContext(context.Background())
	actorKey := &Key{Name: "test.actor", Inherit: true}
	requestKey := &Key{Name: "test.request_id", Inherit: true}
	_ = root.Set(actorKey, "http_user")
	_ = root.Set(requestKey, "req-123")
	root.Seal()

	var wg sync.WaitGroup

	processCtx, process := OpenFrameContext(rootCtx)
	process.Seal()

	wg.Add(1)
	go func() {
		defer wg.Done()

		funcCtx, funcFrame := OpenFrameContext(processCtx)
		funcFrame.Seal()

		asyncCtx, asyncOp := OpenFrameContext(funcCtx)
		_ = asyncCtx

		wg.Add(1)
		go func() {
			defer wg.Done()

			actor, _ := asyncOp.Get(actorKey)
			reqID, _ := asyncOp.Get(requestKey)
			if actor != "http_user" {
				t.Errorf("async_op should see actor, got %v", actor)
			}
			if reqID != "req-123" {
				t.Errorf("async_op should see request_id, got %v", reqID)
			}

			ReleaseFrameContext(asyncOp)
		}()

		ReleaseFrameContext(funcFrame)
		ReleaseFrameContext(process)
	}()

	ReleaseFrameContext(root)

	wg.Wait()
}

func BenchmarkFrameContext_ForkAndRelease(b *testing.B) {
	parentCtx, parent := newFrameContext(context.Background())
	actorKey := &Key{Name: "test.actor", Inherit: true}
	scopeKey := &Key{Name: "test.scope", Inherit: true}
	requestKey := &Key{Name: "test.request_id", Inherit: true}
	_ = parent.Set(actorKey, "bench_user")
	_ = parent.Set(scopeKey, "admin")
	_ = parent.Set(requestKey, "req-123")
	parent.Seal()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, child := OpenFrameContext(parentCtx)
		ReleaseFrameContext(child)
	}

	ReleaseFrameContext(parent)
}

func BenchmarkFrameContext_ConcurrentForkRelease(b *testing.B) {
	parentCtx, parent := newFrameContext(context.Background())
	actorKey := &Key{Name: "test.actor", Inherit: true}
	scopeKey := &Key{Name: "test.scope", Inherit: true}
	_ = parent.Set(actorKey, "bench_user")
	_ = parent.Set(scopeKey, "admin")
	parent.Seal()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, child := OpenFrameContext(parentCtx)
			ReleaseFrameContext(child)
		}
	})

	ReleaseFrameContext(parent)
}
