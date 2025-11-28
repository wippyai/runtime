package scheduler

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

// Basic functionality tests

func TestDequeNewCapacity(t *testing.T) {
	tests := []struct {
		input    int
		expected int
	}{
		{1, 1},
		{2, 2},
		{3, 4},   // rounds up to power of 2
		{5, 8},   // rounds up to power of 2
		{16, 16}, // already power of 2
		{17, 32}, // rounds up to power of 2
	}

	for _, tc := range tests {
		d := NewDeque(tc.input)
		buf := d.buffer.Load()
		if len(buf.items) != tc.expected {
			t.Errorf("NewDeque(%d): got capacity %d, want %d", tc.input, len(buf.items), tc.expected)
		}
	}
}

func TestDequePushPop(t *testing.T) {
	d := NewDeque(8)

	// Push 5 items
	for i := 0; i < 5; i++ {
		d.Push(&Processor{ID: uint64(i)})
	}

	if d.Len() != 5 {
		t.Fatalf("expected len 5, got %d", d.Len())
	}

	// Pop returns LIFO order
	for i := 4; i >= 0; i-- {
		p := d.Pop()
		if p == nil {
			t.Fatalf("unexpected nil at position %d", i)
		}
		if p.ID != uint64(i) {
			t.Fatalf("expected ID %d, got %d", i, p.ID)
		}
	}

	if d.Len() != 0 {
		t.Fatalf("expected len 0, got %d", d.Len())
	}

	// Pop from empty returns nil
	if p := d.Pop(); p != nil {
		t.Fatalf("expected nil from empty deque, got %v", p)
	}
}

func TestDequeSteal(t *testing.T) {
	d := NewDeque(8)

	for i := 0; i < 5; i++ {
		d.Push(&Processor{ID: uint64(i)})
	}

	// Steal returns FIFO order (oldest first)
	p := d.Steal()
	if p == nil || p.ID != 0 {
		t.Fatalf("expected ID 0, got %v", p)
	}

	p = d.Steal()
	if p == nil || p.ID != 1 {
		t.Fatalf("expected ID 1, got %v", p)
	}

	if d.Len() != 3 {
		t.Fatalf("expected len 3, got %d", d.Len())
	}
}

func TestDequeStealHalf(t *testing.T) {
	d := NewDeque(16)

	for i := 0; i < 8; i++ {
		d.Push(&Processor{ID: uint64(i)})
	}

	buf := make([]*Processor, 64)
	count := d.StealHalfInto(buf)

	if count != 4 {
		t.Fatalf("expected 4 stolen, got %d", count)
	}

	// Stolen items are FIFO (oldest first)
	for i := 0; i < count; i++ {
		if buf[i].ID != uint64(i) {
			t.Fatalf("stolen[%d] expected ID %d, got %d", i, i, buf[i].ID)
		}
	}

	if d.Len() != 4 {
		t.Fatalf("expected len 4, got %d", d.Len())
	}
}

func TestDequeGrow(t *testing.T) {
	d := NewDeque(4)

	// Push more than initial capacity
	for i := 0; i < 20; i++ {
		d.Push(&Processor{ID: uint64(i)})
	}

	if d.Len() != 20 {
		t.Fatalf("expected len 20, got %d", d.Len())
	}

	// Verify all items retrievable in LIFO order
	for i := 19; i >= 0; i-- {
		p := d.Pop()
		if p == nil || p.ID != uint64(i) {
			t.Fatalf("expected ID %d, got %v", i, p)
		}
	}
}

func TestDequeGrowDuringSteal(t *testing.T) {
	d := NewDeque(4)

	// Fill to trigger growth
	for i := 0; i < 10; i++ {
		d.Push(&Processor{ID: uint64(i)})
	}

	// Steal some while growing
	buf := make([]*Processor, 64)
	stolen := d.StealHalfInto(buf)

	// Pop remaining
	remaining := d.Len()
	for d.Len() > 0 {
		d.Pop()
	}

	// Total should equal original count
	if stolen+remaining != 10 {
		t.Fatalf("lost items: stolen=%d, remaining=%d, total should be 10", stolen, remaining)
	}
}

func TestDequeMixedOperations(t *testing.T) {
	d := NewDeque(8)

	// Push 3
	d.Push(&Processor{ID: 1})
	d.Push(&Processor{ID: 2})
	d.Push(&Processor{ID: 3})

	// Pop 1 (should get 3)
	if p := d.Pop(); p.ID != 3 {
		t.Fatalf("expected 3, got %d", p.ID)
	}

	// Steal 1 (should get 1)
	if p := d.Steal(); p.ID != 1 {
		t.Fatalf("expected 1, got %d", p.ID)
	}

	// Push 2 more
	d.Push(&Processor{ID: 4})
	d.Push(&Processor{ID: 5})

	// Len should be 3 (2, 4, 5)
	if d.Len() != 3 {
		t.Fatalf("expected len 3, got %d", d.Len())
	}
}

func TestDequeEmptyOperations(t *testing.T) {
	d := NewDeque(4)

	if d.Pop() != nil {
		t.Fatal("Pop on empty should return nil")
	}

	if d.Steal() != nil {
		t.Fatal("Steal on empty should return nil")
	}

	buf := make([]*Processor, 10)
	if n := d.StealHalfInto(buf); n != 0 {
		t.Fatalf("StealHalf on empty should return 0, got %d", n)
	}

	if !d.IsEmpty() {
		t.Fatal("should be empty")
	}
}

// Concurrency tests

func TestDequeConcurrentSteal(t *testing.T) {
	d := NewDeque(256)

	const numItems = 1000
	for i := 0; i < numItems; i++ {
		d.Push(&Processor{ID: uint64(i)})
	}

	var stolen atomic.Int64
	var wg sync.WaitGroup

	// Multiple stealers
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if p := d.Steal(); p != nil {
					stolen.Add(1)
				} else if d.IsEmpty() {
					return
				}
			}
		}()
	}

	wg.Wait()

	if stolen.Load() != numItems {
		t.Fatalf("expected %d stolen, got %d", numItems, stolen.Load())
	}
}

func TestDequeConcurrentPushPopSteal(t *testing.T) {
	d := NewDeque(64)

	const ops = 10000
	var pushed, popped, stolen atomic.Int64
	var wg sync.WaitGroup

	// Owner: push and pop
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < ops; i++ {
			d.Push(&Processor{ID: uint64(i)})
			pushed.Add(1)

			if i%3 == 0 {
				if d.Pop() != nil {
					popped.Add(1)
				}
			}
		}
	}()

	// Thieves
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < ops/2; j++ {
				if d.Steal() != nil {
					stolen.Add(1)
				}
				runtime.Gosched()
			}
		}()
	}

	wg.Wait()

	// Drain remaining
	remaining := 0
	for d.Pop() != nil {
		remaining++
	}

	total := popped.Load() + stolen.Load() + int64(remaining)
	if total != pushed.Load() {
		t.Fatalf("item mismatch: pushed=%d, retrieved=%d (popped=%d, stolen=%d, remaining=%d)",
			pushed.Load(), total, popped.Load(), stolen.Load(), remaining)
	}
}

func TestDequeConcurrentStealHalf(t *testing.T) {
	d := NewDeque(512)

	const numItems = 1000
	for i := 0; i < numItems; i++ {
		d.Push(&Processor{ID: uint64(i)})
	}

	var stolen atomic.Int64
	var wg sync.WaitGroup

	// Multiple batch stealers
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]*Processor, 64)
			for {
				n := d.StealHalfInto(buf)
				if n > 0 {
					stolen.Add(int64(n))
				} else if d.IsEmpty() {
					return
				}
				runtime.Gosched()
			}
		}()
	}

	wg.Wait()

	if stolen.Load() != numItems {
		t.Fatalf("expected %d stolen, got %d", numItems, stolen.Load())
	}
}

// Allocation tests

func TestDequeZeroAllocPushPop(t *testing.T) {
	d := NewDeque(64)

	// Pre-warm
	for i := 0; i < 32; i++ {
		d.Push(&Processor{ID: uint64(i)})
	}
	for i := 0; i < 32; i++ {
		d.Pop()
	}

	// Measure allocations
	p := &Processor{ID: 999}
	allocs := testing.AllocsPerRun(1000, func() {
		d.Push(p)
		d.Pop()
	})

	if allocs > 0 {
		t.Fatalf("expected 0 allocs, got %f", allocs)
	}
}

func TestDequeZeroAllocSteal(t *testing.T) {
	d := NewDeque(64)
	p := &Processor{ID: 999}

	// Pre-fill
	for i := 0; i < 32; i++ {
		d.Push(p)
	}

	// Measure steal allocations (reuse same processor for refill)
	allocs := testing.AllocsPerRun(100, func() {
		stolen := d.Steal()
		if stolen != nil {
			d.Push(stolen) // Refill with stolen item, no alloc
		}
	})

	if allocs > 0 {
		t.Fatalf("expected 0 allocs, got %f", allocs)
	}
}

// Benchmarks

func BenchmarkDequePush(b *testing.B) {
	d := NewDeque(1024)
	p := &Processor{ID: 1}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Push(p)
	}
}

func BenchmarkDequePop(b *testing.B) {
	d := NewDeque(1024)
	p := &Processor{ID: 1}

	// Fill
	for i := 0; i < 512; i++ {
		d.Push(p)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if d.Pop() == nil {
			// Refill when empty
			for j := 0; j < 512; j++ {
				d.Push(p)
			}
		}
	}
}

func BenchmarkDequePushPop(b *testing.B) {
	d := NewDeque(64)
	p := &Processor{ID: 1}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Push(p)
		d.Pop()
	}
}

func BenchmarkDequeSteal(b *testing.B) {
	d := NewDeque(1024)
	p := &Processor{ID: 1}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Push(p)
		d.Steal()
	}
}

func BenchmarkDequeStealHalf(b *testing.B) {
	d := NewDeque(1024)
	p := &Processor{ID: 1}
	buf := make([]*Processor, 64)

	// Fill initially
	for i := 0; i < 512; i++ {
		d.Push(p)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		n := d.StealHalfInto(buf)
		// Refill what was stolen
		for j := 0; j < n; j++ {
			d.Push(p)
		}
	}
}

func BenchmarkDequeConcurrentSteal(b *testing.B) {
	d := NewDeque(4096)
	p := &Processor{ID: 1}

	// Fill
	for i := 0; i < 2048; i++ {
		d.Push(p)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if d.Steal() == nil {
				// Refill (only one goroutine will succeed per item)
				d.Push(p)
			}
		}
	})
}
