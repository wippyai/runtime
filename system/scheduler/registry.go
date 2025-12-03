// Package scheduler provides scheduling and command dispatch infrastructure.
package scheduler

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/dispatcher"
)

// Registry maps CommandID to Handler with O(1) lookup.
// Uses hybrid storage: fixed array for system commands (0-255) and map for extended (256+).
//
// After Freeze() is called, lookups are lock-free for maximum performance.
type Registry struct {
	handlers [256]dispatcher.Handler                     // system commands (0-255), O(1) direct index
	extended map[dispatcher.CommandID]dispatcher.Handler // user/WASM commands (256+)
	mu       sync.RWMutex
	frozen   atomic.Bool
}

// NewRegistry creates a new dispatcher registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a handler for a command ID.
// Thread-safe for use during initialization from multiple goroutines.
// Panics if a handler is already registered (catch conflicts early).
// Panics if called after Freeze().
func (r *Registry) Register(id dispatcher.CommandID, h dispatcher.Handler) {
	if r.frozen.Load() {
		panic("cannot register handler after Freeze()")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if id < 256 {
		if r.handlers[id] != nil {
			panic(fmt.Sprintf("handler already registered for command %d", id))
		}
		r.handlers[id] = h
	} else {
		if r.extended == nil {
			r.extended = make(map[dispatcher.CommandID]dispatcher.Handler)
		}
		if r.extended[id] != nil {
			panic(fmt.Sprintf("handler already registered for command %d", id))
		}
		r.extended[id] = h
	}
}

// Freeze marks the registry as immutable, enabling lock-free lookups.
// Call this after all handlers are registered during initialization.
func (r *Registry) Freeze() {
	r.frozen.Store(true)
}

// IsFrozen returns true if the registry has been frozen.
func (r *Registry) IsFrozen() bool {
	return r.frozen.Load()
}

// Get returns the handler for a command ID.
// Returns nil if no handler registered.
// Lock-free after Freeze() is called.
func (r *Registry) Get(id dispatcher.CommandID) dispatcher.Handler {
	if r.frozen.Load() {
		if id < 256 {
			return r.handlers[id]
		}
		return r.extended[id]
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if id < 256 {
		return r.handlers[id]
	}
	return r.extended[id]
}

// Has returns true if a handler is registered for the command ID.
func (r *Registry) Has(id dispatcher.CommandID) bool {
	if r.frozen.Load() {
		if id < 256 {
			return r.handlers[id] != nil
		}
		return r.extended[id] != nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if id < 256 {
		return r.handlers[id] != nil
	}
	return r.extended[id] != nil
}

// Dispatch implements dispatcher.Dispatcher.
func (r *Registry) Dispatch(cmd dispatcher.Command) dispatcher.Handler {
	return r.Get(cmd.CmdID())
}

// Ensure Registry implements the api interfaces
var (
	_ dispatcher.Registry   = (*Registry)(nil)
	_ dispatcher.Registrar  = (*Registry)(nil)
	_ dispatcher.Dispatcher = (*Registry)(nil)
)
