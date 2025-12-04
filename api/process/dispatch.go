package process

import (
	"context"
	"fmt"
	"sync"
)

type (
	// CommandID identifies a command type for handler lookup.
	CommandID uint16

	// Command represents a yield from a process requesting external work.
	Command interface {
		CmdID() CommandID
	}

	// Completer signals yield completion to the scheduler.
	// Complete is called exactly once per command to return the result.
	Completer interface {
		Complete(data any, err error)
	}

	// Handler processes commands yielded by processes.
	Handler interface {
		Handle(ctx context.Context, cmd Command, complete Completer) error
	}

	// Dispatcher routes commands to handlers.
	Dispatcher interface {
		Dispatch(cmd Command) Handler
	}

	// Registry provides O(1) command-to-handler lookup.
	Registry interface {
		Get(id CommandID) Handler
		Has(id CommandID) bool
	}

	// Registrar allows registering handlers during boot.
	Registrar interface {
		Registry
		Register(id CommandID, h Handler)
	}

	// Freezer allows freezing the registry after boot.
	Freezer interface {
		Freeze()
	}
)

// HandlerFunc adapts a function to the Handler interface.
type HandlerFunc func(ctx context.Context, cmd Command, complete Completer) error

// Handle implements Handler.
func (f HandlerFunc) Handle(ctx context.Context, cmd Command, complete Completer) error {
	return f(ctx, cmd, complete)
}

var (
	registeredCmds   = make(map[CommandID]string)
	registeredCmdsMu sync.Mutex
)

// MustRegisterCommands registers command IDs for a module.
// Panics if any ID is already registered (catches collisions at startup).
// Call this in init() of each command package.
func MustRegisterCommands(module string, ids ...CommandID) {
	registeredCmdsMu.Lock()
	defer registeredCmdsMu.Unlock()

	for _, id := range ids {
		if existing, ok := registeredCmds[id]; ok {
			panic(fmt.Sprintf("command ID %d already registered by %q, cannot register for %q", id, existing, module))
		}
		registeredCmds[id] = module
	}
}
