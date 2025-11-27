package scheduler

import (
	"sync/atomic"
	"unsafe"
)

// Deque is a Chase-Lev work-stealing deque.
//
// Design:
//   - Owner thread: Push/Pop from bottom (LIFO) - no synchronization needed
//   - Thief threads: Steal from top (FIFO) - uses CAS for coordination
//
// Memory model:
//   - bottom is only modified by owner
//   - top is modified by thieves via CAS
//   - items use atomic pointer operations for thread safety
//
// Safety:
//   - Single owner assumed for Push/Pop (not enforced, caller responsibility)
//   - Multiple thieves safe for Steal/StealHalf
//   - Grow is safe but may cause temporary contention
//
// References:
//   - "Dynamic Circular Work-Stealing Deque" - Chase & Lev, SPAA 2005
//   - Go runtime's work-stealing implementation
type Deque struct {
	buffer atomic.Pointer[dequeBuffer]
	top    atomic.Int64 // Steal end (modified by thieves with CAS)
	bottom atomic.Int64 // Push/Pop end (modified only by owner)
}

// dequeBuffer holds the circular array of items.
// Uses unsafe.Pointer for atomic item access.
type dequeBuffer struct {
	items []unsafe.Pointer // atomic Processor pointers
	mask  int64            // len(items) - 1, for fast modulo via bitwise AND
}

// NewDeque creates a deque with initial capacity (must be power of 2).
func NewDeque(capacity int) *Deque {
	// Ensure capacity is power of 2
	cap := 1
	for cap < capacity {
		cap *= 2
	}

	d := &Deque{}
	buf := &dequeBuffer{
		items: make([]unsafe.Pointer, cap),
		mask:  int64(cap - 1),
	}
	d.buffer.Store(buf)
	return d
}

// Push adds an item to the bottom (owner only).
// Grows the buffer if full.
func (d *Deque) Push(p *Processor) {
	bottom := d.bottom.Load()
	top := d.top.Load()
	buf := d.buffer.Load()

	size := bottom - top
	if size >= int64(len(buf.items)-1) {
		buf = d.grow(buf, top, bottom)
	}

	// Store item atomically
	atomic.StorePointer(&buf.items[bottom&buf.mask], unsafe.Pointer(p))

	// Store with release semantics (Store on atomic.Int64 provides this)
	d.bottom.Store(bottom + 1)
}

// Pop removes and returns an item from the bottom (owner only).
// Returns nil if deque is empty.
func (d *Deque) Pop() *Processor {
	bottom := d.bottom.Load() - 1
	buf := d.buffer.Load()

	// Store bottom first with release semantics
	d.bottom.Store(bottom)

	top := d.top.Load()

	if bottom > top {
		// Multiple items, safe to pop without CAS
		p := (*Processor)(atomic.LoadPointer(&buf.items[bottom&buf.mask]))
		return p
	}

	if bottom == top {
		// Single item, race with thieves possible
		p := (*Processor)(atomic.LoadPointer(&buf.items[bottom&buf.mask]))

		if !d.top.CompareAndSwap(top, top+1) {
			// Thief got it first
			d.bottom.Store(top + 1)
			return nil
		}

		d.bottom.Store(top + 1)
		return p
	}

	// Empty (bottom < top after our decrement)
	d.bottom.Store(top)
	return nil
}

// Steal takes one item from the top (thieves, uses CAS).
// Returns nil if deque is empty or CAS fails (retry recommended).
func (d *Deque) Steal() *Processor {
	top := d.top.Load()

	// Memory barrier before reading bottom
	bottom := d.bottom.Load()

	if top >= bottom {
		return nil
	}

	buf := d.buffer.Load()
	p := (*Processor)(atomic.LoadPointer(&buf.items[top&buf.mask]))

	if !d.top.CompareAndSwap(top, top+1) {
		// Another thief won, caller should retry
		return nil
	}

	return p
}

// StealHalfInto takes up to half the items into dst buffer.
// Returns number of items stolen. More efficient than repeated Steal().
func (d *Deque) StealHalfInto(dst []*Processor) int {
	top := d.top.Load()
	bottom := d.bottom.Load()

	size := bottom - top
	if size <= 0 {
		return 0
	}

	// Load buffer BEFORE CAS to avoid race with grow()
	buf := d.buffer.Load()

	// Take half, minimum 1
	n := size / 2
	if n < 1 {
		n = 1
	}
	if n > int64(len(dst)) {
		n = int64(len(dst))
	}

	// Read items BEFORE CAS (they're valid as long as top hasn't moved)
	for i := int64(0); i < n; i++ {
		idx := (top + i) & buf.mask
		dst[i] = (*Processor)(atomic.LoadPointer(&buf.items[idx]))
	}

	// Try to claim n items atomically
	if !d.top.CompareAndSwap(top, top+n) {
		// Contention - items we read may be invalid, return 0
		return 0
	}

	return int(n)
}

// Len returns approximate count of items.
// May be stale due to concurrent operations.
func (d *Deque) Len() int {
	bottom := d.bottom.Load()
	top := d.top.Load()
	size := bottom - top
	if size < 0 {
		return 0
	}
	return int(size)
}

// IsEmpty returns true if deque appears empty.
// May be stale due to concurrent operations.
func (d *Deque) IsEmpty() bool {
	return d.Len() == 0
}

// grow doubles the buffer capacity and copies items.
// Called by owner when buffer is full.
func (d *Deque) grow(old *dequeBuffer, top, bottom int64) *dequeBuffer {
	newCap := len(old.items) * 2
	newBuf := &dequeBuffer{
		items: make([]unsafe.Pointer, newCap),
		mask:  int64(newCap - 1),
	}

	// Copy items from old to new buffer
	for i := top; i < bottom; i++ {
		p := atomic.LoadPointer(&old.items[i&old.mask])
		atomic.StorePointer(&newBuf.items[i&newBuf.mask], p)
	}

	d.buffer.Store(newBuf)
	return newBuf
}
