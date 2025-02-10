package runtime

import (
	"context"
	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/events"
)

// Event system and kind constants for the executor package
const (
	// FunctionSystem identifies the executor system in the event bus
	FunctionSystem events.System = "functions"

	// RegisterFunctionCommand is the event kind for registering a new handler
	RegisterFunctionCommand events.Kind = "functions.register"

	// DeleteFunctionCommand is the event kind for removing an existing handler
	DeleteFunctionCommand events.Kind = "functions.remove"

	// AcceptFunction is the event kind for accepting a new handler
	AcceptFunction events.Kind = "functions.accept"

	// RejectFunction is the event kind for rejecting a new handler
	RejectFunction events.Kind = "functions.reject"
)

type (
	// Func is the core function type that processes tasks
	// It returns a channel for streaming results and any immediate initialization errors
	Func func(context.Context, Task) (chan *Result, error)

	// FuncRegistry provides the interface for executing functions
	// It abstracts the function lookup and execution process
	FuncRegistry interface {
		Call(context.Context, Task) (chan *Result, error)
	}
)

func GetFunctions(ctx context.Context) FuncRegistry {
	return ctx.Value(contextapi.FunctionsCtx).(FuncRegistry)
}
