// Package resource provides process-local resource table management.
package resource

import "sync"

// Handle is an opaque reference to a resource.
// Handle 0 is reserved and always invalid.
type Handle uint32

// Dropper is implemented by resources that need cleanup when removed.
type Dropper interface {
	Drop()
}

type entry struct {
	typeID      uint32
	value       any
	borrowCount uint32
	valid       bool
}

// Table manages typed resources with handle allocation and lifecycle tracking.
// Uses a free list for handle reuse to minimize allocations.
// Thread-safe for concurrent access.
type Table struct {
	mu       sync.RWMutex
	entries  []entry
	freeList []Handle
	closed   bool
}

// NewTable creates a new resource table.
func NewTable() *Table {
	return &Table{
		entries:  make([]entry, 0, 64),
		freeList: make([]Handle, 0, 16),
	}
}

// Insert adds a value with the given type ID and returns its handle.
func (t *Table) Insert(typeID uint32, value any) Handle {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return 0
	}

	e := entry{
		typeID: typeID,
		value:  value,
		valid:  true,
	}

	if len(t.freeList) > 0 {
		handle := t.freeList[len(t.freeList)-1]
		t.freeList = t.freeList[:len(t.freeList)-1]
		t.entries[handle-1] = e
		return handle
	}

	t.entries = append(t.entries, e)
	return Handle(len(t.entries))
}

// Get retrieves a value by handle, regardless of type.
func (t *Table) Get(handle Handle) (any, bool) {
	if handle == 0 {
		return nil, false
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	idx := handle - 1
	if int(idx) >= len(t.entries) {
		return nil, false
	}

	e := t.entries[idx]
	if !e.valid {
		return nil, false
	}
	return e.value, true
}

// GetTyped retrieves a value only if it matches the expected type ID.
func (t *Table) GetTyped(handle Handle, typeID uint32) (any, bool) {
	if handle == 0 {
		return nil, false
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	idx := handle - 1
	if int(idx) >= len(t.entries) {
		return nil, false
	}

	e := t.entries[idx]
	if !e.valid || e.typeID != typeID {
		return nil, false
	}
	return e.value, true
}

// Remove drops a resource and returns the value if found.
// Calls Drop() on values implementing Dropper.
// Returns false if handle is invalid or has outstanding borrows.
func (t *Table) Remove(handle Handle) (any, bool) {
	if handle == 0 {
		return nil, false
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	idx := handle - 1
	if int(idx) >= len(t.entries) {
		return nil, false
	}

	e := &t.entries[idx]
	if !e.valid {
		return nil, false
	}

	if e.borrowCount > 0 {
		return nil, false
	}

	value := e.value

	if d, ok := value.(Dropper); ok {
		d.Drop()
	}

	e.valid = false
	e.value = nil
	e.borrowCount = 0
	t.freeList = append(t.freeList, handle)

	return value, true
}

// Borrow increments the borrow count for a handle.
func (t *Table) Borrow(handle Handle) bool {
	if handle == 0 {
		return false
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	idx := handle - 1
	if int(idx) >= len(t.entries) {
		return false
	}

	e := &t.entries[idx]
	if !e.valid {
		return false
	}

	e.borrowCount++
	return true
}

// ReturnBorrow decrements the borrow count for a handle.
func (t *Table) ReturnBorrow(handle Handle) bool {
	if handle == 0 {
		return false
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	idx := handle - 1
	if int(idx) >= len(t.entries) {
		return false
	}

	e := &t.entries[idx]
	if !e.valid || e.borrowCount == 0 {
		return false
	}

	e.borrowCount--
	return true
}

// TypeID returns the type ID for a handle.
func (t *Table) TypeID(handle Handle) (uint32, bool) {
	if handle == 0 {
		return 0, false
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	idx := handle - 1
	if int(idx) >= len(t.entries) {
		return 0, false
	}

	e := t.entries[idx]
	if !e.valid {
		return 0, false
	}
	return e.typeID, true
}

// Len returns the number of active resources.
func (t *Table) Len() int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	count := 0
	for _, e := range t.entries {
		if e.valid {
			count++
		}
	}
	return count
}

// Each iterates over all active resources.
// Iteration stops if fn returns false.
func (t *Table) Each(fn func(Handle, uint32, any) bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for i, e := range t.entries {
		if e.valid {
			if !fn(Handle(i+1), e.typeID, e.value) {
				break
			}
		}
	}
}

// Clear drops all resources without closing the table.
func (t *Table) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()

	for i := range t.entries {
		if t.entries[i].valid {
			if d, ok := t.entries[i].value.(Dropper); ok {
				d.Drop()
			}
			t.entries[i].valid = false
			t.entries[i].value = nil
		}
	}
	t.freeList = t.freeList[:0]
}

// Close releases all resources and prevents further operations.
func (t *Table) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true

	for i := range t.entries {
		if t.entries[i].valid {
			if d, ok := t.entries[i].value.(Dropper); ok {
				d.Drop()
			}
			t.entries[i].valid = false
			t.entries[i].value = nil
		}
	}

	t.entries = nil
	t.freeList = nil
	return nil
}

// Reset clears all resources and prepares the table for reuse.
// Unlike Close, allows the table to be used again.
func (t *Table) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	for i := range t.entries {
		if t.entries[i].valid {
			if d, ok := t.entries[i].value.(Dropper); ok {
				d.Drop()
			}
			t.entries[i].valid = false
			t.entries[i].value = nil
		}
	}
	t.entries = t.entries[:0]
	t.freeList = t.freeList[:0]
	t.closed = false
}

// IsClosed returns true if the table has been closed.
func (t *Table) IsClosed() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.closed
}

// TypedTable provides type-safe access to resources of a specific type.
type TypedTable[T any] struct {
	table  *Table
	typeID uint32
}

// NewTypedTable creates a typed wrapper around a resource table.
func NewTypedTable[T any](table *Table, typeID uint32) *TypedTable[T] {
	return &TypedTable[T]{
		table:  table,
		typeID: typeID,
	}
}

// Insert adds a value and returns its handle.
func (t *TypedTable[T]) Insert(value T) Handle {
	return t.table.Insert(t.typeID, value)
}

// Get retrieves a value by handle.
func (t *TypedTable[T]) Get(handle Handle) (T, bool) {
	var zero T
	v, ok := t.table.GetTyped(handle, t.typeID)
	if !ok {
		return zero, false
	}
	return v.(T), true
}

// Remove drops a resource and returns the value if found.
func (t *TypedTable[T]) Remove(handle Handle) (T, bool) {
	var zero T
	if _, ok := t.table.GetTyped(handle, t.typeID); !ok {
		return zero, false
	}
	v, ok := t.table.Remove(handle)
	if !ok {
		return zero, false
	}
	return v.(T), true
}

// Len returns the number of resources of this type.
func (t *TypedTable[T]) Len() int {
	count := 0
	t.table.Each(func(_ Handle, typeID uint32, _ any) bool {
		if typeID == t.typeID {
			count++
		}
		return true
	})
	return count
}

// Each iterates over resources of this type.
func (t *TypedTable[T]) Each(fn func(Handle, T) bool) {
	t.table.Each(func(h Handle, typeID uint32, v any) bool {
		if typeID == t.typeID {
			return fn(h, v.(T))
		}
		return true
	})
}
