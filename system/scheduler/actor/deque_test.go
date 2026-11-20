// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

func TestDequeNewCapacity(t *testing.T) {
	tests := []struct {
		input    int
		expected int
	}{
		{1, 1},
		{2, 2},
		{3, 4},
		{5, 8},
		{16, 16},
		{17, 32},
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

	for i := 0; i < 5; i++ {
		d.Push(&Processor{id: uint64(i)})
	}

	if d.Len() != 5 {
		t.Fatalf("expected len 5, got %d", d.Len())
	}

	// Pop returns LIFO order
	for i := 4; i >= 0; i-- {
		p := d.Pop()
		if p == nil {
			t.Fatalf("unexpected nil at position %d", i)
			return
		}
		if p.id != uint64(i) {
			t.Fatalf("expected ID %d, got %d", i, p.id)
		}
	}

	if d.Len() != 0 {
		t.Fatalf("expected len 0, got %d", d.Len())
	}

	if p := d.Pop(); p != nil {
		t.Fatalf("expected nil from empty deque, got %v", p)
	}
}

func TestDequeSteal(t *testing.T) {
	d := NewDeque(8)

	for i := 0; i < 5; i++ {
		d.Push(&Processor{id: uint64(i)})
	}

	// Steal returns FIFO order (oldest first)
	p := d.Steal()
	if p == nil || p.id != 0 {
		t.Fatalf("expected ID 0, got %v", p)
	}

	p = d.Steal()
	if p == nil || p.id != 1 {
		t.Fatalf("expected ID 1, got %v", p)
	}

	if d.Len() != 3 {
		t.Fatalf("expected len 3, got %d", d.Len())
	}
}

func TestDequeStealHalf(t *testing.T) {
	d := NewDeque(16)

	for i := 0; i < 8; i++ {
		d.Push(&Processor{id: uint64(i)})
	}

	buf := make([]*Processor, 64)
	count := d.StealHalfInto(buf)

	if count != 4 {
		t.Fatalf("expected 4 stolen, got %d", count)
	}

	for i := 0; i < count; i++ {
		if buf[i].id != uint64(i) {
			t.Fatalf("stolen[%d] expected ID %d, got %d", i, i, buf[i].id)
		}
	}

	if d.Len() != 4 {
		t.Fatalf("expected len 4, got %d", d.Len())
	}
}

// A batch thief competes with the owner for the last few processors. Verify
// identities: a duplicate processor can start a second worker after pool reuse.
func TestDequeBatchStealAndPopClaimEachProcessorOnce(t *testing.T) {
	const size = 32
	d := NewDeque(size * 2)
	var items [size]Processor
	for i := range items {
		items[i].id = uint64(i)
	}

	start := make(chan struct{})
	done := make(chan int)
	var stolen [size / 2]*Processor
	go func() {
		for range start {
			done <- d.StealHalfInto(stolen[:])
		}
	}()
	defer close(start)

	for round := range 5000 {
		for i := range items {
			d.Push(&items[i])
		}
		start <- struct{}{}
		if round%2 == 0 {
			runtime.Gosched()
		}
		var seen [size]int
		for p := d.Pop(); p != nil; p = d.Pop() {
			seen[p.id]++
		}
		n := <-done
		for _, p := range stolen[:n] {
			seen[p.id]++
		}
		for i, count := range seen {
			if count != 1 {
				t.Fatalf("round %d: processor %d claimed %d times", round, i, count)
			}
		}
	}
}

func TestDequeGrow(t *testing.T) {
	d := NewDeque(4)

	for i := 0; i < 20; i++ {
		d.Push(&Processor{id: uint64(i)})
	}

	if d.Len() != 20 {
		t.Fatalf("expected len 20, got %d", d.Len())
	}

	for i := 19; i >= 0; i-- {
		p := d.Pop()
		if p == nil || p.id != uint64(i) {
			t.Fatalf("expected ID %d, got %v", i, p)
		}
	}
}

func TestDequeMixedOperations(t *testing.T) {
	d := NewDeque(8)

	d.Push(&Processor{id: 1})
	d.Push(&Processor{id: 2})
	d.Push(&Processor{id: 3})

	if p := d.Pop(); p == nil || p.id != 3 {
		t.Fatalf("expected 3, got %v", p)
	}

	if p := d.Steal(); p == nil || p.id != 1 {
		t.Fatalf("expected 1, got %v", p)
	}

	d.Push(&Processor{id: 4})
	d.Push(&Processor{id: 5})

	if d.Len() != 3 {
		t.Fatalf("expected len 3, got %d", d.Len())
	}
}

func TestDequeConcurrentSteal(t *testing.T) {
	d := NewDeque(256)

	const numItems = 1000
	for i := 0; i < numItems; i++ {
		d.Push(&Processor{id: uint64(i)})
	}

	var stolen atomic.Int64
	var wg sync.WaitGroup

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

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < ops; i++ {
			d.Push(&Processor{id: uint64(i)})
			pushed.Add(1)
			if i%3 == 0 {
				if d.Pop() != nil {
					popped.Add(1)
				}
			}
		}
	}()

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

	remaining := 0
	for d.Pop() != nil {
		remaining++
	}

	total := popped.Load() + stolen.Load() + int64(remaining)
	if total != pushed.Load() {
		t.Fatalf("item mismatch: pushed=%d, retrieved=%d", pushed.Load(), total)
	}
}

func BenchmarkDequePushPop(b *testing.B) {
	d := NewDeque(64)
	p := &Processor{id: 1}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Push(p)
		d.Pop()
	}
}

func BenchmarkDequeSteal(b *testing.B) {
	d := NewDeque(1024)
	p := &Processor{id: 1}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Push(p)
		d.Steal()
	}
}
