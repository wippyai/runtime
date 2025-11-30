package function

import (
	"context"
	"errors"

	"github.com/wippyai/runtime/api/dispatcher"
	funcapi "github.com/wippyai/runtime/api/dispatcher/func"
	"github.com/wippyai/runtime/api/function"
)

// ErrRegistryNotFound is returned when the function registry is not in context.
var ErrRegistryNotFound = errors.New("function registry not found in context")

// Dispatcher handles function call commands from the scheduler.
// It delegates to the function Registry for actual execution.
type Dispatcher struct {
	call        *CallHandler
	asyncStart  *AsyncStartHandler
	asyncAwait  *AsyncAwaitHandler
	asyncCancel *AsyncCancelHandler
}

// NewDispatcher creates a new function dispatcher.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		call:        &CallHandler{},
		asyncStart:  &AsyncStartHandler{},
		asyncAwait:  &AsyncAwaitHandler{},
		asyncCancel: &AsyncCancelHandler{},
	}
}

// RegisterAll registers all function command handlers with the dispatcher.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(funcapi.CmdCall, d.call)
	register(funcapi.CmdAsyncStart, d.asyncStart)
	register(funcapi.CmdAsyncAwait, d.asyncAwait)
	register(funcapi.CmdAsyncCancel, d.asyncCancel)
}

// CallHandler processes synchronous function call commands.
type CallHandler struct{}

func (h *CallHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	callCmd := cmd.(*funcapi.CallCmd)

	registry := function.GetRegistry(ctx)
	if registry == nil {
		emit(funcapi.Response{Error: ErrRegistryNotFound})
		return nil
	}

	result, err := registry.Call(ctx, callCmd.Task)
	if err != nil {
		emit(funcapi.Response{Error: err})
		return nil
	}

	if result.Error != nil {
		emit(funcapi.Response{Error: result.Error})
		return nil
	}

	emit(funcapi.Response{Value: result.Value})
	return nil
}

// AsyncStartHandler starts an async function call.
type AsyncStartHandler struct{}

func (h *AsyncStartHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	startCmd := cmd.(*funcapi.AsyncStartCmd)

	registry := function.GetRegistry(ctx)
	if registry == nil {
		emit(funcapi.AsyncStartResponse{Error: ErrRegistryNotFound})
		return nil
	}

	callRegistry := GetOrCreateAsyncCallRegistry(ctx)
	id := callRegistry.Start(ctx, registry, startCmd.Task)
	emit(funcapi.AsyncStartResponse{CallID: id})
	return nil
}

// AsyncAwaitHandler waits for an async call to complete.
type AsyncAwaitHandler struct{}

func (h *AsyncAwaitHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	awaitCmd := cmd.(*funcapi.AsyncAwaitCmd)

	callRegistry := GetAsyncCallRegistry(ctx)
	if callRegistry == nil {
		emit(funcapi.AsyncAwaitResponse{Error: ErrCallNotFound})
		return nil
	}

	result, err := callRegistry.Await(ctx, awaitCmd.CallID)
	if err != nil {
		cancelled := errors.Is(err, ErrCallCancelled)
		emit(funcapi.AsyncAwaitResponse{Error: err, Cancelled: cancelled})
		return nil
	}

	emit(funcapi.AsyncAwaitResponse{Value: result})
	return nil
}

// AsyncCancelHandler cancels an in-progress async call.
type AsyncCancelHandler struct{}

func (h *AsyncCancelHandler) Handle(ctx context.Context, cmd dispatcher.Command, _ dispatcher.EmitFunc) error {
	cancelCmd := cmd.(*funcapi.AsyncCancelCmd)

	callRegistry := GetAsyncCallRegistry(ctx)
	if callRegistry == nil {
		return ErrCallNotFound
	}

	return callRegistry.Cancel(cancelCmd.CallID)
}
