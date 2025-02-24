package function

import (
	"context"
	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/runtime"
)

const (
	// HostID identifies the function node in the pub/sub system
	HostID pubsub.HostID = "node:functions"

	// System identifies the executor system in the event bus
	System events.System = "function"

	// FuncRegister is sent TO function nodes to request registration of a new handler
	FuncRegister events.Kind = "function.register"

	// FuncDelete is sent TO function nodes to request removal of an existing handler
	FuncDelete events.Kind = "function.delete"

	// FuncAccept is sent FROM function nodes when a handler registration is accepted
	FuncAccept events.Kind = "function.accept"

	// FuncReject is sent FROM function nodes when a handler registration is rejected
	FuncReject events.Kind = "function.reject"
)

type (
	// Func represents a core function type that processes tasks asynchronously.
	// It takes a context and task as input and returns a channel for streaming
	// results and any immediate initialization errors.
	//
	// Parameters:
	//   - ctx: Context for cancellation and value propagation
	//   - task: The task to be executed
	//
	// Returns:
	//   - chan *runtime.Result: Channel for streaming execution results, closed on completion
	//   - error: Any immediate initialization or validation errors
	Func func(context.Context, runtime.Task) (chan *runtime.Result, error)

	// Registry defines the interface for managing and executing functions.
	// It abstracts the function lookup and execution process, providing a
	// unified interface for function calls.
	Registry interface {
		// Call executes a function identified by the task and returns a channel
		// for streaming results and any immediate errors.
		Call(context.Context, runtime.Task) (chan *runtime.Result, error)
	}

	// Context holds function-specific context information.
	// This includes metadata about the function instance being executed.
	Context struct {
		// PID is a unique process identifier that includes the function ID
		PID pubsub.PID
	}
)

// WithContext creates a new context containing the function context information.
// This allows function-specific data to be propagated through the context chain.
func WithContext(ctx context.Context, function *Context) context.Context {
	return context.WithValue(ctx, contextapi.FunctionCtx, function)
}

// GetContext retrieves the function context from the provided context.
// It returns the function-specific context information stored in the context chain.
func GetContext(ctx context.Context) *Context {
	return ctx.Value(contextapi.FunctionCtx).(*Context)
}

// GetFuncsRegistry retrieves the function registry from the provided context.
// It returns the registry interface that can be used to execute functions.
func GetFuncsRegistry(ctx context.Context) Registry {
	return ctx.Value(contextapi.FunctionsCtx).(Registry)
}
