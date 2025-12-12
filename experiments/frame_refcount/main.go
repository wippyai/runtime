package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Frame represents a frame context with reference counting and parent link
type Frame struct {
	id       string
	parent   *Frame         // Link to parent frame (for chain collapse)
	refcount atomic.Int32   // Reference count
	values   map[string]any // Frame data
	mu       sync.RWMutex   // Protects values AND release operations
	released bool
}

var verbose = false

var pool = sync.Pool{
	New: func() any {
		return &Frame{values: make(map[string]any, 8)}
	},
}

// Fork creates a new child frame from this parent.
// MUST be called synchronously before spawning goroutine.
// Increments parent refcount to keep parent alive until child releases.
func (parent *Frame) Fork(id string) *Frame {
	// Increment parent refcount FIRST (atomic, safe)
	parent.IncRef()

	// Get new frame from pool
	f := pool.Get().(*Frame)
	f.id = id
	f.parent = parent
	f.refcount.Store(1)
	f.released = false

	// Copy values under parent's lock (safe - we hold a ref)
	parent.mu.RLock()
	for k, v := range parent.values {
		f.values[k] = v
	}
	parent.mu.RUnlock()

	if verbose {
		fmt.Printf("[%s] forked from [%s], parent refcount now %d\n",
			id, parent.id, parent.refcount.Load())
	}

	return f
}

// NewRootFrame creates a new root frame (no parent)
func NewRootFrame(id string) *Frame {
	f := pool.Get().(*Frame)
	f.id = id
	f.parent = nil
	f.refcount.Store(1)
	f.released = false
	return f
}

func (f *Frame) IncRef() {
	f.refcount.Add(1)
}

func (f *Frame) Set(key string, value any) {
	f.mu.Lock()
	f.values[key] = value
	f.mu.Unlock()
}

func (f *Frame) Get(key string) (any, bool) {
	f.mu.RLock()
	v, ok := f.values[key]
	f.mu.RUnlock()
	return v, ok
}

// Release decrements refcount and triggers chain collapse when zero
func (f *Frame) Release() {
	newCount := f.refcount.Add(-1)
	if verbose {
		fmt.Printf("[%s] Release called, refcount now %d\n", f.id, newCount)
	}

	if newCount > 0 {
		return // Still has references
	}

	if newCount < 0 {
		panic(fmt.Sprintf("[%s] refcount went negative!", f.id))
	}

	// Refcount is 0 - safe to release
	f.mu.Lock()
	parent := f.parent
	f.parent = nil
	clear(f.values)
	f.released = true
	f.mu.Unlock()

	// Release parent AFTER we've cleared our state (chain collapse)
	if parent != nil {
		if verbose {
			fmt.Printf("[%s] triggering parent [%s] release (chain collapse)\n", f.id, parent.id)
		}
		parent.Release()
	}

	if verbose {
		fmt.Printf("[%s] returned to pool\n", f.id)
	}
	pool.Put(f)
}

// Legacy NewFrame for comparison (broken for async)
func NewFrame(id string, parent *Frame) *Frame {
	if parent == nil {
		return NewRootFrame(id)
	}
	return parent.Fork(id)
}

func main() {
	fmt.Println("=== Test 1: Simple parent-child, child releases first ===")
	test1()

	fmt.Println("\n=== Test 2: Parent releases before child ===")
	test2()

	fmt.Println("\n=== Test 3: Multiple children from same parent ===")
	test3()

	fmt.Println("\n=== Test 4: Three-level chain (grandparent) ===")
	test4()

	fmt.Println("\n=== Test 5: Async - parent finishes before child starts (BROKEN) ===")
	test5()

	fmt.Println("\n=== Test 6: Async - fork before goroutine (CORRECT) ===")
	test6()

	fmt.Println("\n=== Test 7: Stress test - many concurrent forks ===")
	test7()
}

func test1() {
	parent := NewFrame("parent", nil)
	parent.Set("actor", "user123")

	child := NewFrame("child", parent)
	fmt.Printf("child inherited actor: %v\n", mustGet(child, "actor"))

	// Child releases first
	child.Release()
	// Parent releases second
	parent.Release()
}

func test2() {
	parent := NewFrame("parent", nil)
	parent.Set("actor", "user123")

	child := NewFrame("child", parent)
	fmt.Printf("child inherited actor: %v\n", mustGet(child, "actor"))

	// Parent releases first - but child holds reference, so parent stays alive
	parent.Release()
	fmt.Println("parent.Release() returned, but parent not pooled yet (child holds ref)")

	// Child releases - triggers chain collapse
	child.Release()
}

func test3() {
	parent := NewFrame("parent", nil)
	parent.Set("actor", "user123")

	child1 := NewFrame("child1", parent)
	child2 := NewFrame("child2", parent)
	child3 := NewFrame("child3", parent)

	fmt.Printf("parent refcount after 3 children: %d\n", parent.refcount.Load())

	// Parent releases
	parent.Release()
	fmt.Printf("parent refcount after parent.Release(): %d\n", parent.refcount.Load())

	// Children release one by one
	child1.Release()
	fmt.Printf("parent refcount after child1.Release(): %d\n", parent.refcount.Load())

	child2.Release()
	fmt.Printf("parent refcount after child2.Release(): %d\n", parent.refcount.Load())

	child3.Release() // This triggers parent to pool
}

func test4() {
	grandparent := NewFrame("grandparent", nil)
	grandparent.Set("actor", "admin")

	parent := NewFrame("parent", grandparent)
	parent.Set("scope", "read")

	child := NewFrame("child", parent)
	fmt.Printf("child sees actor=%v scope=%v\n", mustGet(child, "actor"), mustGet(child, "scope"))

	// Release in reverse order - each triggers chain
	grandparent.Release()
	parent.Release()
	child.Release() // Triggers parent release, which triggers grandparent release
}

func test5() {
	parent := NewFrame("parent", nil)
	parent.Set("actor", "async_user")

	var wg sync.WaitGroup
	wg.Add(1)

	// WRONG: Child forks inside goroutine - parent may be released before fork
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		child := NewFrame("async_child", parent)
		fmt.Printf("async_child inherited actor: %v\n", mustGet(child, "actor"))
		time.Sleep(50 * time.Millisecond)
		child.Release()
	}()

	time.Sleep(5 * time.Millisecond)
	parent.Release()
	fmt.Println("parent.Release() returned")

	wg.Wait()
	fmt.Println("async test complete (BROKEN)")
}

func test6() {
	parent := NewFrame("parent", nil)
	parent.Set("actor", "async_user")

	var wg sync.WaitGroup
	wg.Add(1)

	// CORRECT: Fork BEFORE starting goroutine - parent refcount incremented synchronously
	child := NewFrame("async_child", parent)

	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		fmt.Printf("async_child inherited actor: %v\n", mustGet(child, "actor"))
		time.Sleep(50 * time.Millisecond)
		child.Release()
	}()

	time.Sleep(5 * time.Millisecond)
	parent.Release()
	fmt.Println("parent.Release() returned (but parent not pooled - child holds ref)")

	wg.Wait()
	fmt.Println("async test complete (CORRECT)")
}

func test7() {
	// Stress test: parent spawns many children concurrently
	// All children must see inherited values, no races
	parent := NewRootFrame("parent")
	parent.Set("actor", "stress_user")
	parent.Set("scope", "admin")

	const numChildren = 100
	var wg sync.WaitGroup
	wg.Add(numChildren)

	// Fork ALL children synchronously BEFORE starting goroutines
	children := make([]*Frame, numChildren)
	for i := 0; i < numChildren; i++ {
		children[i] = parent.Fork(fmt.Sprintf("child_%d", i))
	}

	fmt.Printf("parent refcount after %d forks: %d\n", numChildren, parent.refcount.Load())

	// Now start goroutines - parent can release anytime
	for i := 0; i < numChildren; i++ {
		go func(idx int) {
			defer wg.Done()
			child := children[idx]

			// Random delay to simulate work
			time.Sleep(time.Duration(idx%10) * time.Millisecond)

			// Verify inherited values
			actor, _ := child.Get("actor")
			scope, _ := child.Get("scope")
			if actor != "stress_user" || scope != "admin" {
				panic(fmt.Sprintf("child_%d: bad inherited values: actor=%v scope=%v", idx, actor, scope))
			}

			child.Release()
		}(i)
	}

	// Parent releases immediately
	parent.Release()
	fmt.Println("parent.Release() returned, waiting for children...")

	wg.Wait()
	fmt.Println("stress test complete - all children saw correct values")
}

func mustGet(f *Frame, key string) any {
	v, ok := f.Get(key)
	if !ok {
		return "<not found>"
	}
	return v
}
