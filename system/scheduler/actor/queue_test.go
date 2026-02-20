package actor

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

// Basic functionality tests

func TestQueueNewCapacity(t *testing.T) {
	q := NewQueue(8)
	if cap(q.items) != 16 { // minimum is 16
		t.Fatalf("expected capacity >= 16, got %d", cap(q.items))
	}

	q = NewQueue(100)
	if cap(q.items) != 100 {
		t.Fatalf("expected capacity 100, got %d", cap(q.items))
	}
}

func TestQueuePushPop(t *testing.T) {
	q := NewQueue(16)

	// Push 5 items
	for i := 0; i < 5; i++ {
		q.Push(&Processor{id: uint64(i)})
	}

	if q.Len() != 5 {
		t.Fatalf("expected len 5, got %d", q.Len())
	}

	// Pop returns FIFO order
	for i := 0; i < 5; i++ {
		p := q.Pop()
		if p == nil {
			t.Fatalf("unexpected nil at position %d", i)
			return
		}
		if p.id != uint64(i) {
			t.Fatalf("expected ID %d, got %d", i, p.id)
		}
	}

	if q.Len() != 0 {
		t.Fatalf("expected len 0, got %d", q.Len())
	}

	// Pop from empty returns nil
	if p := q.Pop(); p != nil {
		t.Fatalf("expected nil from empty queue, got %v", p)
	}
}

func TestQueuePopN(t *testing.T) {
	q := NewQueue(32)

	for i := 0; i < 10; i++ {
		q.Push(&Processor{id: uint64(i)})
	}

	buf := make([]*Processor, 4)
	n := q.PopN(buf)

	if n != 4 {
		t.Fatalf("expected 4, got %d", n)
	}

	// Should be FIFO
	for i := 0; i < n; i++ {
		if buf[i].id != uint64(i) {
			t.Fatalf("buf[%d] expected ID %d, got %d", i, i, buf[i].id)
		}
	}

	if q.Len() != 6 {
		t.Fatalf("expected len 6, got %d", q.Len())
	}
}

func TestQueuePopNMoreThanAvailable(t *testing.T) {
	q := NewQueue(16)

	for i := 0; i < 3; i++ {
		q.Push(&Processor{id: uint64(i)})
	}

	buf := make([]*Processor, 10)
	n := q.PopN(buf)

	if n != 3 {
		t.Fatalf("expected 3, got %d", n)
	}

	if q.Len() != 0 {
		t.Fatalf("expected len 0, got %d", q.Len())
	}
}

func TestQueueGrow(t *testing.T) {
	q := NewQueue(16)

	// Push more than initial capacity
	for i := 0; i < 50; i++ {
		q.Push(&Processor{id: uint64(i)})
	}

	if q.Len() != 50 {
		t.Fatalf("expected len 50, got %d", q.Len())
	}

	// Verify all items in FIFO order
	for i := 0; i < 50; i++ {
		p := q.Pop()
		if p == nil || p.id != uint64(i) {
			t.Fatalf("expected ID %d, got %v", i, p)
		}
	}
}

func TestQueueWrapAround(t *testing.T) {
	q := NewQueue(16)

	// Push and pop to move head/tail
	for round := 0; round < 5; round++ {
		for i := 0; i < 10; i++ {
			q.Push(&Processor{id: uint64(round*10 + i)})
		}
		for i := 0; i < 10; i++ {
			p := q.Pop()
			if p == nil || p.id != uint64(round*10+i) {
				t.Fatalf("round %d, expected ID %d, got %v", round, round*10+i, p)
			}
		}
	}
}

func TestQueueIsEmpty(t *testing.T) {
	q := NewQueue(16)

	if !q.IsEmpty() {
		t.Fatal("new queue should be empty")
	}

	q.Push(&Processor{id: 1})
	if q.IsEmpty() {
		t.Fatal("queue with item should not be empty")
	}

	q.Pop()
	if !q.IsEmpty() {
		t.Fatal("queue after pop should be empty")
	}
}

// Concurrency tests

func TestQueueConcurrentPushPop(t *testing.T) {
	q := NewQueue(64)

	const numOps = 10000
	var pushed, popped atomic.Int64
	var wg sync.WaitGroup

	// Producers
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for j := 0; j < numOps/4; j++ {
				q.Push(&Processor{id: uint64(base*numOps + j)})
				pushed.Add(1)
			}
		}(i)
	}

	// Consumers
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOps/4; j++ {
				for {
					if q.Pop() != nil {
						popped.Add(1)
						break
					}
					runtime.Gosched()
				}
			}
		}()
	}

	wg.Wait()

	// Drain any remaining
	for q.Pop() != nil {
		popped.Add(1)
	}

	if pushed.Load() != popped.Load() {
		t.Fatalf("mismatch: pushed=%d, popped=%d", pushed.Load(), popped.Load())
	}
}

func TestQueueConcurrentPopN(t *testing.T) {
	q := NewQueue(256)

	const numItems = 1000
	for i := 0; i < numItems; i++ {
		q.Push(&Processor{id: uint64(i)})
	}

	var popped atomic.Int64
	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]*Processor, 16)
			for {
				n := q.PopN(buf)
				if n > 0 {
					popped.Add(int64(n))
				} else if q.IsEmpty() {
					return
				}
				runtime.Gosched()
			}
		}()
	}

	wg.Wait()

	if popped.Load() != numItems {
		t.Fatalf("expected %d popped, got %d", numItems, popped.Load())
	}
}

func TestQueueNoDataRace(_ *testing.T) {
	q := NewQueue(64)
	done := make(chan struct{})

	// Writer
	go func() {
		for i := 0; i < 10000; i++ {
			q.Push(&Processor{id: uint64(i)})
		}
		close(done)
	}()

	// Readers
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					// Drain remaining
					for q.Pop() != nil { //nolint:revive // intentional empty drain loop
					}
					return
				default:
					q.Pop()
					_ = q.Len()
					_ = q.IsEmpty()
				}
			}
		}()
	}

	wg.Wait()
}

// Allocation tests

func TestQueueZeroAllocPushPop(t *testing.T) {
	q := NewQueue(64)

	// Pre-warm
	for i := 0; i < 32; i++ {
		q.Push(&Processor{id: uint64(i)})
	}
	for i := 0; i < 32; i++ {
		q.Pop()
	}

	p := &Processor{id: 999}
	allocs := testing.AllocsPerRun(1000, func() {
		q.Push(p)
		q.Pop()
	})

	if allocs > 0 {
		t.Fatalf("expected 0 allocs, got %f", allocs)
	}
}

func TestQueueZeroAllocPopN(t *testing.T) {
	q := NewQueue(64)
	buf := make([]*Processor, 8)
	p := &Processor{id: 1}

	// Pre-fill
	for i := 0; i < 32; i++ {
		q.Push(p)
	}

	allocs := testing.AllocsPerRun(100, func() {
		q.PopN(buf)
		// Refill
		for i := 0; i < 8; i++ {
			q.Push(p)
		}
	})

	if allocs > 0 {
		t.Fatalf("expected 0 allocs, got %f", allocs)
	}
}

// Benchmarks

func BenchmarkQueuePush(b *testing.B) {
	q := NewQueue(1024)
	p := &Processor{id: 1}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Push(p)
	}
}

func BenchmarkQueuePop(b *testing.B) {
	q := NewQueue(1024)
	p := &Processor{id: 1}

	// Fill
	for i := 0; i < 512; i++ {
		q.Push(p)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if q.Pop() == nil {
			// Refill
			for j := 0; j < 512; j++ {
				q.Push(p)
			}
		}
	}
}

func BenchmarkQueuePushPop(b *testing.B) {
	q := NewQueue(64)
	p := &Processor{id: 1}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Push(p)
		q.Pop()
	}
}

func BenchmarkQueuePopN(b *testing.B) {
	q := NewQueue(256)
	p := &Processor{id: 1}
	buf := make([]*Processor, 16)

	// Fill
	for i := 0; i < 128; i++ {
		q.Push(p)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		n := q.PopN(buf)
		// Refill
		for j := 0; j < n; j++ {
			q.Push(p)
		}
	}
}

func BenchmarkQueueConcurrentPushPop(b *testing.B) {
	q := NewQueue(1024)
	p := &Processor{id: 1}

	// Pre-fill
	for i := 0; i < 512; i++ {
		q.Push(p)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			q.Push(p)
			q.Pop()
		}
	})
}
