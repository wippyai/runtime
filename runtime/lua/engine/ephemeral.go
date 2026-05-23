// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"sync"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
)

// TopicEphemeral is the reserved topic that delivers ephemeral channel
// router frames. deliverMessage special-cases this exact constant and
// dispatches via the per-process ephemeralRouter without creating an entry
// in subs.byTopic, handlers, or any other lifetime-broad map.
//
// Producers that route per-call channels (time.after, time.timer,
// time.ticker, websocket subscribe, http stream, ...) target this topic
// and tag each payload with EphemeralFrame{Epoch, ChID, Gen}.
const TopicEphemeral = "@pid/route"

// OverflowPolicy is applied when an ephemeral frame's value cannot be sent
// to the target channel (buffer full and no waiting receiver). Each
// registered entry picks the policy that matches its semantic contract.
type OverflowPolicy uint8

const (
	// OverflowDrop discards the new value silently. Default for clock-style
	// signals (tickers) where a missed tick is acceptable.
	OverflowDrop OverflowPolicy = iota

	// OverflowCoalesce replaces the oldest buffered value with the new one.
	// Right for latest-state feeds where only the most recent value matters.
	OverflowCoalesce

	// OverflowClose closes the channel and stops the producer when overflow
	// occurs. Required for ordered streams (websocket, http body) where
	// silently dropping a frame would corrupt the stream. The Lua consumer
	// observes a closed channel and can decide how to recover.
	OverflowClose
)

// EphemeralValueConverter is an optional per-entry transform that turns the
// incoming payloads into the Lua value pushed onto the channel. Returning
// nil suppresses delivery for that frame.
//
// When the converter is nil the router falls back to PayloadsToLua.
type EphemeralValueConverter func(ctx context.Context, l *lua.LState, source pid.PID, payloads []payload.Payload) lua.LValue

// EphemeralFrame is the wire envelope for every ephemeral router payload.
//
// It is wrapped in exactly one payload.Payload (Golang format) and sent to
// TopicEphemeral via the relay. The flags HasValue and Close are explicit
// so a close-only frame (HasValue=false, Close=true) is unambiguous.
type EphemeralFrame struct {
	Source   pid.PID
	Payloads []payload.Payload
	Epoch    uint64
	ChID     uint64
	Gen      uint64
	HasValue bool
	Close    bool
}

// NewEphemeralFramePayload wraps a frame in a payload suitable for
// relay.NewPackage. Producers should always go through this helper to keep
// the envelope format consistent.
func NewEphemeralFramePayload(f *EphemeralFrame) payload.Payload {
	return payload.NewPayload(f, payload.Golang)
}

// epEntry is one live ephemeral channel registered with the router.
type epEntry struct {
	channel        *Channel
	convert        EphemeralValueConverter
	producerStop   func()
	stopOnce       sync.Once
	gen            uint64
	overflowPolicy OverflowPolicy
}

func (e *epEntry) callStop() {
	if e.producerStop == nil {
		return
	}
	e.stopOnce.Do(e.producerStop)
}

// ephemeralRouter owns the per-process map of active ephemeral channels.
// Allocated lazily on first RegisterEphemeral. The router never creates a
// subs.byTopic / handlers entry; routing happens via a top-level branch in
// deliverMessage keyed on TopicEphemeral.
//
// Concurrency model: routing, registration, BumpGen, Stop, and Drain all
// run on the process step goroutine. The router still uses an internal
// mutex so Abort (called from the scheduler goroutine during cancellation)
// can safely bump the epoch and snapshot producerStop closures.
type ephemeralRouter struct {
	mu      sync.Mutex
	nextID  uint64
	entries map[uint64]*epEntry
}

func newEphemeralRouter() *ephemeralRouter {
	return &ephemeralRouter{
		entries: make(map[uint64]*epEntry, 4),
	}
}

// register installs an entry and returns its chID. Caller is responsible
// for telling the producer (clock dispatcher, ws read loop, ...) the
// matching (epoch, chID, initialGen=0).
func (r *ephemeralRouter) register(ch *Channel, convert EphemeralValueConverter, producerStop func(), policy OverflowPolicy) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	id := r.nextID
	r.entries[id] = &epEntry{
		channel:        ch,
		convert:        convert,
		producerStop:   producerStop,
		overflowPolicy: policy,
	}
	return id
}

// bumpGen advances the entry's generation and returns the new value.
// Returns (0, false) if no entry exists. Producers must include the
// returned gen in subsequent frames; old-gen frames are dropped by Route.
func (r *ephemeralRouter) bumpGen(chID uint64) (uint64, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[chID]
	if !ok {
		return 0, false
	}
	e.gen++
	return e.gen, true
}

// lookup returns the entry by chID. Used by Route on the step thread.
func (r *ephemeralRouter) lookup(chID uint64) (*epEntry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[chID]
	return e, ok
}

// remove deletes an entry and returns it. Used by Route when handling a
// close frame or an OverflowClose policy.
func (r *ephemeralRouter) remove(chID uint64) (*epEntry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[chID]
	if !ok {
		return nil, false
	}
	delete(r.entries, chID)
	return e, true
}

// snapshotEntriesForDrain clears the entry map under lock and returns the
// removed entries so the caller can call producerStop and Close on them
// outside the lock. Used by drainEphemeralChannels.
func (r *ephemeralRouter) snapshotEntriesForDrain() []*epEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.entries) == 0 {
		return nil
	}
	out := make([]*epEntry, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e)
	}
	r.entries = make(map[uint64]*epEntry, 4)
	return out
}

// snapshotStopFuncs returns the producerStop closures of all current
// entries WITHOUT clearing the entries. Used by Process.Abort, which must
// stop external producers from a non-step goroutine but cannot touch
// Channel state.
func (r *ephemeralRouter) snapshotStopFuncs() []func() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.entries) == 0 {
		return nil
	}
	out := make([]func(), 0, len(r.entries))
	for _, e := range r.entries {
		e := e
		out = append(out, e.callStop)
	}
	return out
}

// size returns the number of registered entries. Used by tests and by
// HasSubscriptions to decide whether the process should idle vs deadlock.
func (r *ephemeralRouter) size() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}
