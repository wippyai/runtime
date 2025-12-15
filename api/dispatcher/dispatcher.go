// Package dispatcher provides command dispatch interfaces for the process system.
package dispatcher

import (
	"context"
	"fmt"
	"sync"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var registryCtxKey = &ctxapi.Key{Name: "dispatcher.registry"}

// HandlerKind is the registry kind for handlers.
const HandlerKind = "dispatcher.handler"

type (
	// CommandID identifies a command type for handler lookup.
	CommandID uint16

	// Command represents a yield from a process requesting external work.
	Command interface {
		CmdID() CommandID
	}

	// ResultReceiver receives yield completion results.
	// Scheduler implements this - no Completer allocation needed.
	ResultReceiver interface {
		CompleteYield(tag uint64, data any, err error)
	}

	// Handler processes commands yielded by processes.
	// tag is the correlation tag, receiver is where to send results.
	Handler interface {
		Handle(ctx context.Context, cmd Command, tag uint64, receiver ResultReceiver) error
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
type HandlerFunc func(ctx context.Context, cmd Command, tag uint64, receiver ResultReceiver) error

// Handle implements Handler.
func (f HandlerFunc) Handle(ctx context.Context, cmd Command, tag uint64, receiver ResultReceiver) error {
	return f(ctx, cmd, tag, receiver)
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

// WithRegistry stores a dispatcher registry in the AppContext.
func WithRegistry(ctx context.Context, r Registry) error {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctxapi.ErrNoAppContext
	}
	ac.With(registryCtxKey, r)
	return nil
}

// GetRegistry retrieves the dispatcher registry from AppContext.
func GetRegistry(ctx context.Context) Registry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if r, ok := ac.Get(registryCtxKey).(Registry); ok {
		return r
	}
	return nil
}

// GetRegistrar retrieves the dispatcher registrar from AppContext.
func GetRegistrar(ctx context.Context) Registrar {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if r, ok := ac.Get(registryCtxKey).(Registrar); ok {
		return r
	}
	return nil
}

// GetDispatcher retrieves the dispatcher from AppContext.
func GetDispatcher(ctx context.Context) Dispatcher {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if d, ok := ac.Get(registryCtxKey).(Dispatcher); ok {
		return d
	}
	return nil
}
