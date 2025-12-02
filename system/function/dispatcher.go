// Package function provides function call command handlers for the dispatcher system.
package function

import (
	"context"
	"errors"

	"github.com/wippyai/runtime/api/dispatcher"
	funcapi "github.com/wippyai/runtime/api/dispatcher/func"
	"github.com/wippyai/runtime/api/function"
)

// Dispatcher handles function call commands.
type Dispatcher struct {
	call        dispatcher.HandlerFunc
	asyncStart  dispatcher.HandlerFunc
	asyncAwait  dispatcher.HandlerFunc
	asyncCancel dispatcher.HandlerFunc
}

// NewDispatcher creates a new function dispatcher.
func NewDispatcher() *Dispatcher {
	d := &Dispatcher{}
	d.call = d.handleCall
	d.asyncStart = d.handleAsyncStart
	d.asyncAwait = d.handleAsyncAwait
	d.asyncCancel = d.handleAsyncCancel
	return d
}

// Start is a no-op for function dispatcher.
func (d *Dispatcher) Start(_ context.Context) error {
	return nil
}

// Stop is a no-op for function dispatcher.
func (d *Dispatcher) Stop(_ context.Context) error {
	return nil
}

// RegisterAll registers all function command handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(funcapi.CmdCall, d.call)
	register(funcapi.CmdAsyncStart, d.asyncStart)
	register(funcapi.CmdAsyncAwait, d.asyncAwait)
	register(funcapi.CmdAsyncCancel, d.asyncCancel)
}

func (d *Dispatcher) handleCall(ctx context.Context, cmd dispatcher.Command, emit dispatcher.Emitter) error {
	callCmd := cmd.(*funcapi.CallCmd)

	registry := function.GetRegistry(ctx)
	if registry == nil {
		emit.Emit(funcapi.Response{Error: ErrRegistryNotFound}, nil)
		return nil
	}

	go func() {
		result, err := registry.Call(ctx, callCmd.Task)
		if err != nil {
			emit.Emit(funcapi.Response{Error: err}, nil)
			return
		}
		if result.Error != nil {
			emit.Emit(funcapi.Response{Error: result.Error}, nil)
			return
		}
		emit.Emit(funcapi.Response{Value: result.Value}, nil)
	}()

	return nil
}

func (d *Dispatcher) handleAsyncStart(ctx context.Context, cmd dispatcher.Command, emit dispatcher.Emitter) error {
	startCmd := cmd.(*funcapi.AsyncStartCmd)

	registry := function.GetRegistry(ctx)
	if registry == nil {
		emit.Emit(funcapi.AsyncStartResponse{Error: ErrRegistryNotFound}, nil)
		return nil
	}

	callRegistry := GetOrCreateAsyncCallRegistry(ctx)
	id := callRegistry.Start(ctx, registry, startCmd.Task)
	emit.Emit(funcapi.AsyncStartResponse{CallID: id}, nil)
	return nil
}

func (d *Dispatcher) handleAsyncAwait(ctx context.Context, cmd dispatcher.Command, emit dispatcher.Emitter) error {
	awaitCmd := cmd.(*funcapi.AsyncAwaitCmd)

	callRegistry := GetAsyncCallRegistry(ctx)
	if callRegistry == nil {
		emit.Emit(funcapi.AsyncAwaitResponse{Error: ErrCallNotFound}, nil)
		return nil
	}

	go func() {
		result, err := callRegistry.Await(ctx, awaitCmd.CallID)
		if err != nil {
			cancelled := errors.Is(err, ErrCallCancelled)
			emit.Emit(funcapi.AsyncAwaitResponse{Error: err, Cancelled: cancelled}, nil)
			return
		}
		emit.Emit(funcapi.AsyncAwaitResponse{Value: result}, nil)
	}()

	return nil
}

func (d *Dispatcher) handleAsyncCancel(ctx context.Context, cmd dispatcher.Command, emit dispatcher.Emitter) error {
	cancelCmd := cmd.(*funcapi.AsyncCancelCmd)

	callRegistry := GetAsyncCallRegistry(ctx)
	if callRegistry == nil {
		emit.Emit(nil, nil)
		return nil
	}

	_ = callRegistry.Cancel(cancelCmd.CallID)
	emit.Emit(nil, nil)
	return nil
}
