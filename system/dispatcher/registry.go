// Package dispatcher provides a global registry for command handlers.
// One registry is shared across all schedulers, allowing efficient handler reuse.
package dispatcher

import (
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
)

// GlobalRegistry maps CommandID to Handler with O(1) lookup.
// Uses fixed array for performance (no hashing).
// This is a global singleton shared by all schedulers.
type GlobalRegistry struct {
	handlers [256]dispatcher.Handler
	mu       sync.RWMutex
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
func (r *GlobalRegistry) Register(id dispatcher.CommandID, h dispatcher.Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.handlers[id] != nil {
		panic(fmt.Sprintf("handler already registered for command %d", id))
	}
	r.handlers[id] = h
}

// Get returns the handler for a command ID.
// Returns nil if no handler registered.
func (r *GlobalRegistry) Get(id dispatcher.CommandID) dispatcher.Handler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.handlers[id]
}

// Has returns true if a handler is registered for the command ID.
func (r *GlobalRegistry) Has(id dispatcher.CommandID) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.handlers[id] != nil
}

// Register is a convenience function to register on the global registry.
func Register(id dispatcher.CommandID, h dispatcher.Handler) {
	Global().Register(id, h)
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
