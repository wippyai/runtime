// Package dispatcher provides a global registry for command handlers.
// One registry is shared across all schedulers, allowing efficient handler reuse.
package dispatcher

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/dispatcher"
)

// GlobalRegistry maps CommandID to Handler with O(1) lookup.
// Uses hybrid storage: fixed array for system commands (0-255) and map for extended (256+).
// This is a global singleton shared by all schedulers.
//
// After Freeze() is called, lookups are lock-free for maximum performance.
type GlobalRegistry struct {
	handlers [256]dispatcher.Handler                     // system commands (0-255), O(1) direct index
	extended map[dispatcher.CommandID]dispatcher.Handler // user/WASM commands (256+)
	mu       sync.RWMutex
	frozen   atomic.Bool
}

var (
	global     *GlobalRegistry
	globalOnce sync.Once
)

// Global returns the singleton global registry.
func Global() *GlobalRegistry {
	globalOnce.Do(func() {
		global = &GlobalRegistry{}
	})
	return global
}

// Register adds a handler for a command ID.
// Thread-safe for use during initialization from multiple goroutines.
// Panics if a handler is already registered (catch conflicts early).
// Panics if called after Freeze().
func (r *GlobalRegistry) Register(id dispatcher.CommandID, h dispatcher.Handler) {
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
func (r *GlobalRegistry) Freeze() {
	r.frozen.Store(true)
}

// Get returns the handler for a command ID.
// Returns nil if no handler registered.
// Lock-free after Freeze() is called.
func (r *GlobalRegistry) Get(id dispatcher.CommandID) dispatcher.Handler {
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
func (r *GlobalRegistry) Has(id dispatcher.CommandID) bool {
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

// Register is a convenience function to register on the global registry.
func Register(id dispatcher.CommandID, h dispatcher.Handler) {
	Global().Register(id, h)
}

// Freeze is a convenience function to freeze the global registry.
// Call this after all handlers are registered to enable lock-free lookups.
func Freeze() {
	Global().Freeze()
}

// Get is a convenience function to get from the global registry.
func Get(id dispatcher.CommandID) dispatcher.Handler {
	return Global().Get(id)
}

// Has is a convenience function to check the global registry.
func Has(id dispatcher.CommandID) bool {
	return Global().Has(id)
}

// Dispatch implements dispatcher.Dispatcher.
func (r *GlobalRegistry) Dispatch(cmd dispatcher.Command) dispatcher.Handler {
	return r.Get(cmd.CmdID())
}

// Dispatcher returns the global registry as a dispatcher.Dispatcher.
func Dispatcher() dispatcher.Dispatcher {
	return Global()
}
