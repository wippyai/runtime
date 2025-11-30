// Package resource provides unified resource management for WASM runtime.
// Mirrors the wasmexp resource package for Component Model compatibility.
package resource

import (
	"sync"
	"time"
)

// Handle is an opaque reference to a resource.
// Handle 0 is reserved and always invalid.
type Handle uint32

// Dropper is optionally implemented by resource values that need cleanup.
type Dropper interface {
	Drop()
}

// Table manages typed resources with handle allocation and lifecycle tracking.
// Uses a free list for handle reuse to minimize allocations.
type Table struct {
	mu       sync.RWMutex
	entries  []entry
	freeList []Handle
	closed   bool
}

type entry struct {
	typeID      uint32
	value       any
	borrowCount uint32
	valid       bool
}

// New creates a new resource table.
func New() *Table {
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

	// Call Dropper if implemented
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
// Used for Component Model borrow semantics.
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

// IsClosed returns true if the table has been closed.
func (t *Table) IsClosed() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.closed
}

// TypedTable provides type-safe access to resources of a specific type.
// Wraps a Table with compile-time type checking.
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
	// Verify type first
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
	t.table.Each(func(h Handle, typeID uint32, v any) bool {
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

// ResourceTypes defines type IDs for WASI resources.
// These match Component Model resource type indices.
const (
	TypeInputStream Handle = iota + 1
	TypeOutputStream
	TypePollable
	TypeIncomingRequest
	TypeIncomingBody
	TypeOutgoingResponse
	TypeOutgoingBody
	TypeFields
	TypeOutgoingRequest
	TypeIncomingResponse
	TypeBodyStream
)

// InstanceResources manages all resources for a single WASM instance.
// This is the primary entry point for resource management.
type InstanceResources struct {
	table *Table

	// Pre-allocated typed accessors for common resource types
	inputStreams      *TypedTable[*InputStream]
	outputStreams     *TypedTable[*OutputStream]
	pollables         *TypedTable[*Pollable]
	incomingRequests  *TypedTable[*IncomingRequest]
	outgoingResponses *TypedTable[*OutgoingResponse]
	fields            *TypedTable[*Fields]

	// Timer durations for clock pollables (used by poll.block)
	timerDurations *TimerDurationMap
}

// NewInstanceResources creates resource management for a WASM instance.
func NewInstanceResources() *InstanceResources {
	table := New()
	return &InstanceResources{
		table:             table,
		inputStreams:      NewTypedTable[*InputStream](table, uint32(TypeInputStream)),
		outputStreams:     NewTypedTable[*OutputStream](table, uint32(TypeOutputStream)),
		pollables:         NewTypedTable[*Pollable](table, uint32(TypePollable)),
		incomingRequests:  NewTypedTable[*IncomingRequest](table, uint32(TypeIncomingRequest)),
		outgoingResponses: NewTypedTable[*OutgoingResponse](table, uint32(TypeOutgoingResponse)),
		fields:            NewTypedTable[*Fields](table, uint32(TypeFields)),
		timerDurations:    NewTimerDurationMap(),
	}
}

// Table returns the underlying resource table.
func (r *InstanceResources) Table() *Table {
	return r.table
}

// InputStreams returns typed accessor for input streams.
func (r *InstanceResources) InputStreams() *TypedTable[*InputStream] {
	return r.inputStreams
}

// OutputStreams returns typed accessor for output streams.
func (r *InstanceResources) OutputStreams() *TypedTable[*OutputStream] {
	return r.outputStreams
}

// Pollables returns typed accessor for pollables.
func (r *InstanceResources) Pollables() *TypedTable[*Pollable] {
	return r.pollables
}

// IncomingRequests returns typed accessor for incoming HTTP requests.
func (r *InstanceResources) IncomingRequests() *TypedTable[*IncomingRequest] {
	return r.incomingRequests
}

// OutgoingResponses returns typed accessor for outgoing HTTP responses.
func (r *InstanceResources) OutgoingResponses() *TypedTable[*OutgoingResponse] {
	return r.outgoingResponses
}

// Fields returns typed accessor for HTTP headers/fields.
func (r *InstanceResources) Fields() *TypedTable[*Fields] {
	return r.fields
}

// TimerDurations returns the timer duration map for clock pollables.
func (r *InstanceResources) TimerDurations() *TimerDurationMap {
	return r.timerDurations
}

// Len returns total resource count across all types.
func (r *InstanceResources) Len() int {
	return r.table.Len()
}

// Close releases all resources for this instance.
func (r *InstanceResources) Close() error {
	return r.table.Close()
}

// Common resource types used across WASI hosts

// InputStream represents a WASI input-stream resource.
type InputStream struct {
	StreamID uint64
	Data     []byte
	Offset   int
	Closed   bool
}

// Drop implements Dropper.
func (s *InputStream) Drop() {
	s.Closed = true
	s.Data = nil
}

// OutputStream represents a WASI output-stream resource.
type OutputStream struct {
	StreamID uint64
	Buffer   []byte
	Closed   bool
}

// Drop implements Dropper.
func (s *OutputStream) Drop() {
	s.Closed = true
	s.Buffer = nil
}

// Pollable represents a WASI pollable resource.
// SourceID links to the dispatcher resource (stream, timer, etc).
type Pollable struct {
	SourceID uint64
	Ready    bool
	Expired  bool
}

// Drop implements Dropper. Returns pollable to pool.
func (p *Pollable) Drop() {
	p.Expired = true
	ReleasePollable(p)
}

var pollablePool = sync.Pool{
	New: func() any {
		return &Pollable{}
	},
}

// AcquirePollable gets a Pollable from the pool.
func AcquirePollable() *Pollable {
	p := pollablePool.Get().(*Pollable)
	p.SourceID = 0
	p.Ready = false
	p.Expired = false
	return p
}

// ReleasePollable returns a Pollable to the pool.
func ReleasePollable(p *Pollable) {
	if p != nil {
		pollablePool.Put(p)
	}
}

// IncomingRequest represents a WASI incoming-request resource.
type IncomingRequest struct {
	Method    string
	Path      string
	Query     string
	Authority string
	Scheme    string
	Headers   map[string][]string
	Body      []byte
}

// Drop implements Dropper.
func (r *IncomingRequest) Drop() {
	r.Headers = nil
	r.Body = nil
}

// OutgoingResponse represents a WASI outgoing-response resource.
type OutgoingResponse struct {
	StatusCode uint16
	Headers    map[string][]string
	Body       []byte
}

// Drop implements Dropper.
func (r *OutgoingResponse) Drop() {
	r.Headers = nil
	r.Body = nil
}

// Fields represents HTTP headers (wasi:http/types fields resource).
type Fields struct {
	Values map[string][]string
}

// Drop implements Dropper.
func (f *Fields) Drop() {
	f.Values = nil
}

// TimerDurationMap stores timer durations keyed by pollable handle.
// Used by clock hosts to store durations and poll hosts to retrieve them.
type TimerDurationMap struct {
	mu        sync.RWMutex
	durations map[Handle]time.Duration
}

// NewTimerDurationMap creates a new timer duration map.
func NewTimerDurationMap() *TimerDurationMap {
	return &TimerDurationMap{
		durations: make(map[Handle]time.Duration),
	}
}

// Store saves a duration for a pollable handle.
func (m *TimerDurationMap) Store(handle Handle, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.durations[handle] = duration
}

// Load retrieves a duration for a pollable handle.
func (m *TimerDurationMap) Load(handle Handle) (time.Duration, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	d, ok := m.durations[handle]
	return d, ok
}

// Delete removes a duration for a pollable handle.
func (m *TimerDurationMap) Delete(handle Handle) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.durations, handle)
}

// Clear removes all durations.
func (m *TimerDurationMap) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.durations = make(map[Handle]time.Duration)
}
