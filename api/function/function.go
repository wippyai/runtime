package function

import (
	"context"
	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/runtime"
)

// Event system and kind constants for the executor package
const (
	// System identifies the executor system in the event bus
	System events.System = "function"

	// RegisterFunctionHandler is the event kind for registering a new handler
	RegisterFunctionHandler events.Kind = "function.register"

	// DeleteFunctionHandler is the event kind for removing an existing handler
	DeleteFunctionHandler events.Kind = "function.remove"

	// AcceptFunction is the event kind for accepting a new handler
	AcceptFunction events.Kind = "function.accept"

	// RejectFunction is the event kind for rejecting a new handler
	RejectFunction events.Kind = "function.reject"
)

type (
	// Func is the core function type that processes tasks
	// It returns a channel for streaming results and any immediate initialization errors
	Func func(context.Context, runtime.Task) (chan *runtime.Result, error)

	// FuncRegistry provides the interface for executing functions
	// It abstracts the function lookup and execution process
	FuncRegistry interface {
		Call(context.Context, runtime.Task) (chan *runtime.Result, error)
	}
)

func GetFunctions(ctx context.Context) FuncRegistry {
	return ctx.Value(contextapi.FunctionsCtx).(FuncRegistry)
}
