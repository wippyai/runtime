// Package function provides abstractions for managing and executing asynchronous functions.
package function

import (
	"context"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

const (
	// HostID identifies the function node in the pub/sub system
	HostID relay.HostID = "node:functions"

	// System identifies the executor system in the event bus
	System event.System = "function"

	// Register is sent TO function nodes to request registration of a new handler
	Register event.Kind = "function.register"
	// Delete is sent TO function nodes to request removal of an existing handler
	Delete event.Kind = "function.delete"

	// Accept is sent FROM function nodes when a handler registration is accepted
	Accept event.Kind = "function.accept"
	// Reject is sent FROM function nodes when a handler registration is rejected
	Reject event.Kind = "function.reject"

	// InterceptorOptionsKey is the metadata key for interceptor options in function/process configs
	InterceptorOptionsKey = "options"
)

type (
	// Func represents a core function type that processes tasks synchronously.
	// It takes a context and task as input and returns the result directly.
	// The function blocks until execution completes or the context is cancelled.
	//
	// Parameters:
	//   - ctx: Context for cancellation and value propagation
	//   - task: The task to be executed
	//
	// Returns:
	//   - *runtime.Result: The execution result
	//   - error: Any execution or validation errors
	Func func(context.Context, runtime.Task) (*runtime.Result, error)

	// FuncEntry holds both the function handler and its options for registration.
	FuncEntry struct {
		Handler Func
		Options runtime.Options
	}

	// Registry defines the interface for managing and executing functions.
	// It abstracts the function lookup and execution process, providing a
	// unified interface for function calls.
	Registry interface {
		// Call executes a function identified by the task synchronously and returns
		// the result directly. Blocks until execution completes or context is cancelled.
		Call(context.Context, runtime.Task) (*runtime.Result, error)
	}
)
