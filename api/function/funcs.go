// Package function provides abstractions for managing and executing asynchronous functions.
package function

import (
	"context"
	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/runtime"
)

const (
	// HostID identifies the function node in the pub/sub system
	HostID pubsub.HostID = "node:functions"

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

	OptionsRegister event.Kind = "function.optionsregister"
	OptionsDelete   event.Kind = "function.optionsdelete"
	OptionsAccept   event.Kind = "function.optionsaccept"
	OptionsReject   event.Kind = "function.optionsreject"
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
)
