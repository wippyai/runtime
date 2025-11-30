// Package dispatcher provides interfaces for command dispatch and handling.
// Commands are yielded by processes and dispatched to handlers.
// Handlers can emit multiple values (streaming) or just complete (one-shot).
package dispatcher

import (
	"context"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/registry"
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

// Command represents a yield from a process requesting external work.
// Commands are pure data - they carry no callbacks or internal references.
// The scheduler dispatches commands to handlers based on CmdID().
type Command interface {
	CmdID() CommandID
}

// EmitFunc sends a value to the process.
// Can be called zero or more times before completion.
// Thread-safe - can be called from any goroutine.
//
// For one-shot handlers: typically not called (just complete).
// For streaming handlers: called for each value in the stream.
type EmitFunc func(data any)

// Handler processes commands yielded by processes.
// Handlers are registered per CommandID and invoked by the scheduler.
//
// CRITICAL: Handlers MUST NOT block. They should:
//   - Set up async operations (timers, I/O callbacks)
//   - Call emit() when results are ready
//   - Return immediately
//
// The Handle method receives:
//   - ctx: Caller's context (FrameContext) - for resource lookup and cancellation
//   - cmd: The command to handle
//   - emit: Function to send values to the process (call when async operation completes)
//
// Returns:
//   - nil: Command accepted (async operation started or completed)
//   - error: Command rejected (invalid params, resource not found, etc.)
//
// Patterns:
//
//	Immediate (e.g., now):
//	  func Handle(ctx, cmd, emit) error {
//	      emit(time.Now().UnixNano())
//	      return nil
//	  }
//
//	Async (e.g., sleep):
//	  func Handle(ctx, cmd, emit) error {
//	      timer := time.AfterFunc(cmd.Duration, func() {
//	          emit(nil)
//	      })
//	      // Register cleanup in case process dies
//	      resource.GetStore(ctx).AddCleanup(func() error {
//	          timer.Stop()
//	          return nil
//	      })
//	      return nil
//	  }
//
// Thread safety:
//   - ctx, cmd, and emit are safe to use from spawned goroutines
//   - emit may be called multiple times for streaming handlers
//   - After process dies, emit calls are ignored (safe to call)
type Handler interface {
	Handle(ctx context.Context, cmd Command, emit EmitFunc) error
}

// HandlerFunc adapts a function to the Handler interface.
type HandlerFunc func(ctx context.Context, cmd Command, emit EmitFunc) error

// Handle implements Handler.
func (f HandlerFunc) Handle(ctx context.Context, cmd Command, emit EmitFunc) error {
	return f(ctx, cmd, emit)
}

// Callable allows stateless function calls on a reusable runtime process.
// Used by function pools to execute methods without full process lifecycle.
type Callable interface {
	Call(ctx context.Context, method string, args ...any) (any, error)
}

// Registry kind for dispatcher handlers in the global registry.
const KindHandler registry.Kind = "dispatcher.handler"

// Dispatcher routes commands to handlers.
type Dispatcher interface {
	Dispatch(cmd Command) Handler
}

// AsyncScheduler is the interface for WASM asyncify schedulers.
// Defined here to avoid boxing allocations - SetPending takes Command directly.
type AsyncScheduler interface {
	SetPending(cmd Command)
	GetResult() (uint64, error)
	ClearPending()
}
