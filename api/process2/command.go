package process2

import (
	"context"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/registry"
)

// Registry kind for dispatcher handlers.
const (
	KindHandler registry.Kind = "dispatcher.handler"
)

// CommandID identifies a command type for handler lookup.
//
// Reserved ranges (convention):
//   - 0-9: Core commands (complete, yield, error)
//   - 10-19: Time commands (sleep, timer, ticker)
//   - 50-59: Stream commands (read, write, seek)
//   - 60-79: HTTP commands
//   - 80-89: WebSocket commands
//   - 100-119: SQL commands
//   - 120-129: Store commands (kv)
//   - 130-139: Excel commands
//   - 150-159: Queue commands
//   - 160-179: Relay commands (pubsub, inbox)
//   - 200-209: Function commands (call, async)
//   - 210-219: Exec commands (process)
//   - 1000+: User/WASM/extension commands
//
// Use MustRegisterCommands in init() to catch collisions at startup.
type CommandID uint16

// Core interfaces for the command dispatch system.
type (
	// Command represents a yield from a process requesting external work.
	// Commands are pure data - they carry no callbacks or internal references.
	// The scheduler dispatches commands to handlers based on CmdID().
	Command interface {
		CmdID() CommandID
	}

	// Emitter signals yield completion to the scheduler.
	// Thread-safe - can be called from any goroutine.
	// Must be called exactly once per yield to resume the process.
	Emitter interface {
		Emit(data any, err error)
	}

	// Handler processes commands yielded by processes.
	// Handlers are registered per CommandID and invoked by the scheduler.
	//
	// CRITICAL: Handlers MUST NOT block. They should:
	//   - Set up async operations (timers, I/O callbacks)
	//   - Call emit.Emit() when results are ready
	//   - Return immediately
	Handler interface {
		Handle(ctx context.Context, cmd Command, emit Emitter) error
	}

	// Dispatcher routes commands to handlers.
	Dispatcher interface {
		Dispatch(cmd Command) Handler
	}

	// Callable allows stateless function calls on a reusable runtime process.
	// Used by function pools to execute methods without full process lifecycle.
	Callable interface {
		Call(ctx context.Context, method string, args ...any) (any, error)
	}

	// AsyncScheduler is the interface for WASM asyncify schedulers.
	// Defined here to avoid boxing allocations - SetPending takes Command directly.
	AsyncScheduler interface {
		SetPending(cmd Command)
		GetResult() (uint64, error)
		ClearPending()
	}
)

// HandlerFunc adapts a function to the Handler interface.
type HandlerFunc func(ctx context.Context, cmd Command, emit Emitter) error

// Handle implements Handler.
func (f HandlerFunc) Handle(ctx context.Context, cmd Command, emit Emitter) error {
	return f(ctx, cmd, emit)
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
