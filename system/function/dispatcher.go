// Package function provides function call command handlers for the dispatcher system.
package function

import (
	"context"
	"errors"
	"fmt"

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
	fmt.Printf("[DISPATCHER] handleCall: task=%v\n", callCmd.Task.ID)

	registry := function.GetRegistry(ctx)
	if registry == nil {
		fmt.Println("[DISPATCHER] registry not found")
		emit.Emit(funcapi.Response{Error: function.ErrRegistryNotFound}, nil)
		return nil
	}

	go func() {
		result, err := registry.Call(ctx, callCmd.Task)
		fmt.Printf("[DISPATCHER] registry.Call returned: result=%+v, err=%v\n", result, err)
		if ctx.Err() != nil {
			fmt.Println("[DISPATCHER] ctx cancelled, not emitting")
			return
		}
		if err != nil {
			fmt.Printf("[DISPATCHER] emitting error: %v\n", err)
			emit.Emit(funcapi.Response{Error: err}, nil)
			return
		}
		if result.Error != nil {
			fmt.Printf("[DISPATCHER] emitting result.Error: %v\n", result.Error)
			emit.Emit(funcapi.Response{Error: result.Error}, nil)
			return
		}
		fmt.Printf("[DISPATCHER] emitting result.Value: %v (type %T)\n", result.Value, result.Value)
		emit.Emit(funcapi.Response{Value: result.Value}, nil)
	}()

	return nil
}

func (d *Dispatcher) handleAsyncStart(ctx context.Context, cmd dispatcher.Command, emit dispatcher.Emitter) error {
	startCmd := cmd.(*funcapi.AsyncStartCmd)

	registry := function.GetRegistry(ctx)
	if registry == nil {
		emit.Emit(funcapi.AsyncStartResponse{Error: function.ErrRegistryNotFound}, nil)
		return nil
	}

	callRegistry := GetOrCreateAsyncCallRegistry(ctx)
	if callRegistry == nil {
		emit.Emit(funcapi.AsyncStartResponse{Error: function.ErrCallNotFound}, nil)
		return nil
	}

	id := callRegistry.Start(ctx, registry, startCmd.Task)
	emit.Emit(funcapi.AsyncStartResponse{CallID: id}, nil)
	return nil
}

func (d *Dispatcher) handleAsyncAwait(ctx context.Context, cmd dispatcher.Command, emit dispatcher.Emitter) error {
	awaitCmd := cmd.(*funcapi.AsyncAwaitCmd)

	callRegistry := GetAsyncCallRegistry(ctx)
	if callRegistry == nil {
		emit.Emit(funcapi.AsyncAwaitResponse{Error: function.ErrCallNotFound}, nil)
		return nil
	}

	go func() {
		result, err := callRegistry.Await(ctx, awaitCmd.CallID)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			cancelled := errors.Is(err, function.ErrCallCancelled)
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
