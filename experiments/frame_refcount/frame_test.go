package main

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// Frame Reference Counting Design
//
// This mechanism exists to safely pool frame contexts in a polyglot runtime
// where the execution path is unpredictable. Frames are central to application
// operation - every function call, process spawn, and async operation uses them.
// Pooling reduces memory allocation pressure significantly.
//
// The challenge: In a polyglot runtime (Lua, WebAssembly, Temporal workflows,
// nested function calls, processes spawning processes), the context propagation
// path is not deterministic. Context cancellation works but not always immediately,
// which complicates knowing when a frame can be safely returned to the pool.
//
// The solution: Reference counting with parent links and chain collapse.
// - Fork increments parent refcount synchronously before any async operation
// - Release decrements refcount and triggers parent release when hitting zero
// - Parent is only pooled when ALL descendants have released

func TestFrame_SimpleParentChild(t *testing.T) {
	parent := NewRootFrame("parent")
	parent.Set("actor", "user123")

	child := parent.Fork("child")

	// Child should inherit values
	if v, _ := child.Get("actor"); v != "user123" {
		t.Errorf("child should inherit actor, got %v", v)
	}

	// Parent refcount should be 2 (self + child)
	if parent.refcount.Load() != 2 {
		t.Errorf("parent refcount should be 2, got %d", parent.refcount.Load())
	}

	child.Release()

	// Parent refcount should be 1 now
	if parent.refcount.Load() != 1 {
		t.Errorf("parent refcount should be 1 after child release, got %d", parent.refcount.Load())
	}

	parent.Release()
}

func TestFrame_ParentReleasesFirst(t *testing.T) {
	parent := NewRootFrame("parent")
	parent.Set("actor", "user123")

	child := parent.Fork("child")

	// Parent releases first - but child holds reference
	parent.Release()

	// Parent should NOT be released yet (refcount was 2, now 1)
	if parent.refcount.Load() != 1 {
		t.Errorf("parent refcount should be 1, got %d", parent.refcount.Load())
	}

	// Child should still see inherited value
	if v, _ := child.Get("actor"); v != "user123" {
		t.Errorf("child should still see actor after parent.Release(), got %v", v)
	}

	// Child releases - triggers chain collapse
	child.Release()
}

func TestFrame_MultipleChildren(t *testing.T) {
	parent := NewRootFrame("parent")
	parent.Set("actor", "user123")

	child1 := parent.Fork("child1")
	child2 := parent.Fork("child2")
	child3 := parent.Fork("child3")

	// Parent refcount should be 4 (self + 3 children)
	if parent.refcount.Load() != 4 {
		t.Errorf("parent refcount should be 4, got %d", parent.refcount.Load())
	}

	parent.Release() // refcount -> 3
	child1.Release() // refcount -> 2
	child2.Release() // refcount -> 1
	child3.Release() // refcount -> 0, parent pooled
}

func TestFrame_ThreeLevelChain(t *testing.T) {
	grandparent := NewRootFrame("grandparent")
	grandparent.Set("actor", "admin")

	parent := grandparent.Fork("parent")
	parent.Set("scope", "read")

	child := parent.Fork("child")

	// Child should see both inherited values
	if v, _ := child.Get("actor"); v != "admin" {
		t.Errorf("child should inherit actor from grandparent, got %v", v)
	}
	if v, _ := child.Get("scope"); v != "read" {
		t.Errorf("child should inherit scope from parent, got %v", v)
	}

	// Release in any order - chain collapse handles it
	grandparent.Release()
	parent.Release()
	child.Release() // Triggers parent release, which triggers grandparent release
}

func TestFrame_AsyncForkBeforeGoroutine(t *testing.T) {
	// This is the CORRECT pattern for async operations:
	// Fork BEFORE starting the goroutine
	parent := NewRootFrame("parent")
	parent.Set("actor", "async_user")

	var wg sync.WaitGroup
	wg.Add(1)

	// Fork synchronously BEFORE goroutine
	child := parent.Fork("async_child")

	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)

		// Child should see inherited value even if parent already released
		if v, _ := child.Get("actor"); v != "async_user" {
			t.Errorf("async child should see actor, got %v", v)
		}

		child.Release()
	}()

	// Parent can release immediately - child holds reference
	time.Sleep(5 * time.Millisecond)
	parent.Release()

	wg.Wait()
}

func TestFrame_ConcurrentForks(t *testing.T) {
	// Stress test: many concurrent children
	parent := NewRootFrame("parent")
	parent.Set("actor", "stress_user")
	parent.Set("scope", "admin")

	const numChildren = 100
	var wg sync.WaitGroup
	wg.Add(numChildren)

	// Fork ALL children synchronously
	children := make([]*Frame, numChildren)
	for i := 0; i < numChildren; i++ {
		children[i] = parent.Fork(fmt.Sprintf("child_%d", i))
	}

	// Verify refcount
	expectedRefcount := int32(numChildren + 1)
	if parent.refcount.Load() != expectedRefcount {
		t.Errorf("parent refcount should be %d, got %d", expectedRefcount, parent.refcount.Load())
	}

	// Start goroutines
	for i := 0; i < numChildren; i++ {
		go func(idx int) {
			defer wg.Done()
			child := children[idx]

			time.Sleep(time.Duration(idx%10) * time.Millisecond)

			// Verify inherited values
			actor, _ := child.Get("actor")
			scope, _ := child.Get("scope")
			if actor != "stress_user" || scope != "admin" {
				t.Errorf("child_%d: bad inherited values: actor=%v scope=%v", idx, actor, scope)
			}

			child.Release()
		}(i)
	}

	// Parent releases immediately
	parent.Release()

	wg.Wait()
}

func TestFrame_ChildModificationDoesNotAffectParent(t *testing.T) {
	parent := NewRootFrame("parent")
	parent.Set("actor", "original")

	child := parent.Fork("child")

	// Child modifies its copy
	child.Set("actor", "modified")

	// Parent should still have original value
	if v, _ := parent.Get("actor"); v != "original" {
		t.Errorf("parent actor should be original, got %v", v)
	}

	// Child should have modified value
	if v, _ := child.Get("actor"); v != "modified" {
		t.Errorf("child actor should be modified, got %v", v)
	}

	child.Release()
	parent.Release()
}

func TestFrame_NestedAsyncOperations(t *testing.T) {
	// Simulates: Lua function -> spawns process -> process calls function -> function spawns async
	// This is the polyglot runtime scenario

	root := NewRootFrame("http_handler")
	root.Set("actor", "http_user")
	root.Set("request_id", "req-123")

	var wg sync.WaitGroup

	// Level 1: Process spawn
	process := root.Fork("process")
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(5 * time.Millisecond)

		// Level 2: Function call from process
		funcFrame := process.Fork("function")

		// Level 3: Async operation from function
		asyncOp := funcFrame.Fork("async_op")
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(10 * time.Millisecond)

			// Should see all inherited values
			if v, _ := asyncOp.Get("actor"); v != "http_user" {
				t.Errorf("async_op should see actor, got %v", v)
			}
			if v, _ := asyncOp.Get("request_id"); v != "req-123" {
				t.Errorf("async_op should see request_id, got %v", v)
			}

			asyncOp.Release()
		}()

		funcFrame.Release()
		process.Release()
	}()

	// HTTP handler completes quickly
	root.Release()

	wg.Wait()
}

func BenchmarkFrame_ForkAndRelease(b *testing.B) {
	parent := NewRootFrame("parent")
	parent.Set("actor", "bench_user")
	parent.Set("scope", "admin")
	parent.Set("request_id", "req-123")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		child := parent.Fork("child")
		child.Release()
	}

	parent.Release()
}

func BenchmarkFrame_ConcurrentForkRelease(b *testing.B) {
	parent := NewRootFrame("parent")
	parent.Set("actor", "bench_user")
	parent.Set("scope", "admin")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			child := parent.Fork("child")
			child.Release()
		}
	})

	parent.Release()
}
