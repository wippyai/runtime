// Package resource provides unified resource management for WASM runtime.
// Builds on api/resource for core Table operations, adding WASM-specific types.
package resource

import (
	"sync"
	"time"

	apiresource "github.com/wippyai/runtime/api/resource"
)

// Re-export core types from api/resource for convenience.
type (
	Handle  = apiresource.Handle
	Table   = apiresource.Table
	Dropper = apiresource.Dropper
)

// New creates a new resource table.
var New = apiresource.NewTable

// ResourceTypes defines type IDs for WASI resources.
// These match Component Model resource type indices.
const (
	TypeInputStream uint32 = iota + 1
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
// Uses resource.Store internally to support both Dropper-based cleanup
// and AddCleanup for arbitrary cleanup functions.
type InstanceResources struct {
	store *apiresource.Store

	// Pre-allocated typed accessors for common resource types
	inputStreams      *apiresource.TypedTable[*InputStream]
	outputStreams     *apiresource.TypedTable[*OutputStream]
	pollables         *apiresource.TypedTable[*Pollable]
	incomingRequests  *apiresource.TypedTable[*IncomingRequest]
	outgoingResponses *apiresource.TypedTable[*OutgoingResponse]
	fields            *apiresource.TypedTable[*Fields]

	// Timer durations for clock pollables (used by poll.block)
	timerDurations *TimerDurationMap
}

// NewInstanceResources creates resource management for a WASM instance.
func NewInstanceResources() *InstanceResources {
	store := apiresource.NewStore()
	table := store.Table()
	return &InstanceResources{
		store:             store,
		inputStreams:      apiresource.NewTypedTable[*InputStream](table, TypeInputStream),
		outputStreams:     apiresource.NewTypedTable[*OutputStream](table, TypeOutputStream),
		pollables:         apiresource.NewTypedTable[*Pollable](table, TypePollable),
		incomingRequests:  apiresource.NewTypedTable[*IncomingRequest](table, TypeIncomingRequest),
		outgoingResponses: apiresource.NewTypedTable[*OutgoingResponse](table, TypeOutgoingResponse),
		fields:            apiresource.NewTypedTable[*Fields](table, TypeFields),
		timerDurations:    NewTimerDurationMap(),
	}
}

// Table returns the underlying resource table.
func (r *InstanceResources) Table() *Table {
	return r.store.Table()
}

// Store returns the underlying resource store.
func (r *InstanceResources) Store() *apiresource.Store {
	return r.store
}

// AddCleanup registers a cleanup function to run on Close.
// Cleanups run in LIFO order (last added runs first).
// Returns a cancel function that prevents this cleanup from running.
func (r *InstanceResources) AddCleanup(fn func() error) func() {
	return r.store.AddCleanup(fn)
}

// InputStreams returns typed accessor for input streams.
func (r *InstanceResources) InputStreams() *apiresource.TypedTable[*InputStream] {
	return r.inputStreams
}

// OutputStreams returns typed accessor for output streams.
func (r *InstanceResources) OutputStreams() *apiresource.TypedTable[*OutputStream] {
	return r.outputStreams
}

// Pollables returns typed accessor for pollables.
func (r *InstanceResources) Pollables() *apiresource.TypedTable[*Pollable] {
	return r.pollables
}

// IncomingRequests returns typed accessor for incoming HTTP requests.
func (r *InstanceResources) IncomingRequests() *apiresource.TypedTable[*IncomingRequest] {
	return r.incomingRequests
}

// OutgoingResponses returns typed accessor for outgoing HTTP responses.
func (r *InstanceResources) OutgoingResponses() *apiresource.TypedTable[*OutgoingResponse] {
	return r.outgoingResponses
}

// Fields returns typed accessor for HTTP headers/fields.
func (r *InstanceResources) Fields() *apiresource.TypedTable[*Fields] {
	return r.fields
}

// TimerDurations returns the timer duration map for clock pollables.
func (r *InstanceResources) TimerDurations() *TimerDurationMap {
	return r.timerDurations
}

// Len returns total resource count across all types.
func (r *InstanceResources) Len() int {
	return r.store.Table().Len()
}

// Close releases all resources for this instance.
// Runs cleanup functions (LIFO), then closes table (calls Drop on resources).
func (r *InstanceResources) Close() error {
	return r.store.Close()
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
	Data     []byte
	Closed   bool
}

// Drop implements Dropper.
func (s *OutputStream) Drop() {
	s.Closed = true
}

// Pollable represents a WASI pollable resource.
type Pollable struct {
	SourceID uint64
	Ready    bool
	Expired  bool
}

// Drop implements Dropper.
func (p *Pollable) Drop() {}

var pollablePool = sync.Pool{
	New: func() any { return &Pollable{} },
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
	pollablePool.Put(p)
}

// IncomingRequest represents a WASI incoming-request resource.
type IncomingRequest struct {
	Method      string
	Path        string
	Scheme      string
	Authority   string
	Headers     Handle
	Body        Handle
	Consumed    bool
	PathQuery   string
	HasPathStr  bool
	QueryString string
}

// Drop implements Dropper.
func (r *IncomingRequest) Drop() {}

// OutgoingResponse represents a WASI outgoing-response resource.
type OutgoingResponse struct {
	StatusCode uint16
	Headers    Handle
	Body       Handle
}

// Drop implements Dropper.
func (r *OutgoingResponse) Drop() {}

// Fields represents WASI HTTP headers/trailers.
type Fields struct {
	Entries [][2]string
	Mutable bool
}

// Drop implements Dropper.
func (f *Fields) Drop() {}

// TimerDurationMap maps pollable handles to their timer durations.
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
	m.durations[handle] = duration
	m.mu.Unlock()
}

// Load retrieves the duration for a pollable handle.
func (m *TimerDurationMap) Load(handle Handle) (time.Duration, bool) {
	m.mu.RLock()
	d, ok := m.durations[handle]
	m.mu.RUnlock()
	return d, ok
}

// Delete removes a pollable's timer duration.
func (m *TimerDurationMap) Delete(handle Handle) {
	m.mu.Lock()
	delete(m.durations, handle)
	m.mu.Unlock()
}

// Clear removes all timer durations.
func (m *TimerDurationMap) Clear() {
	m.mu.Lock()
	m.durations = make(map[Handle]time.Duration)
	m.mu.Unlock()
}
