package actor

import (
	"fmt"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/relay"
)

// Registry maps CommandID to Handler with O(1) lookup.
// Uses fixed array instead of map for performance (no hashing).
//
// CommandID ranges (convention, not enforced):
//   - 0-9: Core commands (complete, yield, error)
//   - 10-49: Time commands (sleep, timer, after)
//   - 50-99: IO commands (http, websocket)
//   - 100-149: Database commands (sql, redis)
//   - 150-199: Messaging commands (queue, pubsub)
//   - 200-255: User/extension commands
type Registry struct {
	handlers [256]dispatcher.Handler
}

// NewRegistry creates an empty handler registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a handler for a command ID.
// Panics if a handler is already registered (catch conflicts early).
func (r *Registry) Register(id dispatcher.CommandID, h dispatcher.Handler) {
	if r.handlers[id] != nil {
		panic(fmt.Sprintf("handler already registered for command %d", id))
	}
	r.handlers[id] = h
}

// MustRegister is like Register but takes multiple handlers.
// Convenience for setup code.
func (r *Registry) MustRegister(pairs ...any) {
	if len(pairs)%2 != 0 {
		panic("MustRegister requires pairs of (CommandID, Handler)")
	}
	for i := 0; i < len(pairs); i += 2 {
		id := pairs[i].(dispatcher.CommandID)
		h := pairs[i+1].(dispatcher.Handler)
		r.Register(id, h)
	}
}

// Get returns the handler for a command ID.
// Returns nil if no handler registered.
func (r *Registry) Get(id dispatcher.CommandID) dispatcher.Handler {
	return r.handlers[id]
}

// Has returns true if a handler is registered for the command ID.
func (r *Registry) Has(id dispatcher.CommandID) bool {
	return r.handlers[id] != nil
}

// UnknownCommandError indicates no handler is registered for a command.
type UnknownCommandError struct {
	ID dispatcher.CommandID
}

func (e *UnknownCommandError) Error() string {
	return fmt.Sprintf("no handler registered for command %d", e.ID)
}

// ProcessNotIdleError indicates SendTo was called for a non-idle process.
type ProcessNotIdleError struct {
	ID uint64
}

func (e *ProcessNotIdleError) Error() string {
	return fmt.Sprintf("process %d is not idle", e.ID)
}

// ProcessNotFoundError indicates Send was called for an unknown PID.
type ProcessNotFoundError struct {
	PID relay.PID
}

func (e *ProcessNotFoundError) Error() string {
	return fmt.Sprintf("process %s not found", e.PID.String())
}
