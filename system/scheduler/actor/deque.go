package actor

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
// Safety:
//   - Single owner assumed for Push/Pop (not enforced, caller responsibility)
//   - Multiple thieves safe for Steal/StealHalf
type Deque struct {
	buffer atomic.Pointer[dequeBuffer]
	top    atomic.Int64 // Steal end (modified by thieves with CAS)
	bottom atomic.Int64 // Push/Pop end (modified only by owner)
}

type dequeBuffer struct {
	items []unsafe.Pointer
	mask  int64
}

// NewDeque creates a deque with initial capacity (rounded to power of 2).
func NewDeque(capacity int) *Deque {
	actualCap := 1
	for actualCap < capacity {
		actualCap *= 2
	}

	d := &Deque{}
	buf := &dequeBuffer{
		items: make([]unsafe.Pointer, actualCap),
		mask:  int64(actualCap - 1),
	}
	d.buffer.Store(buf)
	return d
}

// Push adds an item to the bottom (owner only).
func (d *Deque) Push(p *Processor) {
	bottom := d.bottom.Load()
	top := d.top.Load()
	buf := d.buffer.Load()

	size := bottom - top
	if size >= int64(len(buf.items)-1) {
		buf = d.grow(buf, top, bottom)
	}

	atomic.StorePointer(&buf.items[bottom&buf.mask], unsafe.Pointer(p))
	d.bottom.Store(bottom + 1)
}

// Pop removes and returns an item from the bottom (owner only).
func (d *Deque) Pop() *Processor {
	bottom := d.bottom.Load() - 1
	buf := d.buffer.Load()

	d.bottom.Store(bottom)

	top := d.top.Load()

	if bottom > top {
		p := (*Processor)(atomic.LoadPointer(&buf.items[bottom&buf.mask]))
		return p
	}

	if bottom == top {
		p := (*Processor)(atomic.LoadPointer(&buf.items[bottom&buf.mask]))

		if !d.top.CompareAndSwap(top, top+1) {
			d.bottom.Store(top + 1)
			return nil
		}

		d.bottom.Store(top + 1)
		return p
	}

	d.bottom.Store(top)
	return nil
}

// Steal takes one item from the top (thieves, uses CAS).
func (d *Deque) Steal() *Processor {
	top := d.top.Load()
	bottom := d.bottom.Load()

	if top >= bottom {
		return nil
	}

	buf := d.buffer.Load()
	p := (*Processor)(atomic.LoadPointer(&buf.items[top&buf.mask]))

	if !d.top.CompareAndSwap(top, top+1) {
		return nil
	}

	return p
}

// StealHalfInto takes up to half the items into dst buffer.
func (d *Deque) StealHalfInto(dst []*Processor) int {
	top := d.top.Load()
	bottom := d.bottom.Load()

	size := bottom - top
	if size <= 0 {
		return 0
	}

	buf := d.buffer.Load()

	n := size / 2
	if n < 1 {
		n = 1
	}
	if n > int64(len(dst)) {
		n = int64(len(dst))
	}

	for i := int64(0); i < n; i++ {
		idx := (top + i) & buf.mask
		dst[i] = (*Processor)(atomic.LoadPointer(&buf.items[idx]))
	}

	if !d.top.CompareAndSwap(top, top+n) {
		return 0
	}

	return int(n)
}

// Len returns approximate count of items.
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
func (d *Deque) IsEmpty() bool {
	return d.Len() == 0
}

func (d *Deque) grow(old *dequeBuffer, top, bottom int64) *dequeBuffer {
	newCap := len(old.items) * 2
	newBuf := &dequeBuffer{
		items: make([]unsafe.Pointer, newCap),
		mask:  int64(newCap - 1),
	}

	for i := top; i < bottom; i++ {
		p := atomic.LoadPointer(&old.items[i&old.mask])
		atomic.StorePointer(&newBuf.items[i&newBuf.mask], p)
	}

	d.buffer.Store(newBuf)
	return newBuf
}
