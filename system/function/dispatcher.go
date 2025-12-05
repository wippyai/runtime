// Package function provides function call command handlers for the dispatcher system.
package function

import (
	"context"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

// Dispatcher handles function call commands.
type Dispatcher struct {
	call        dispatcher.HandlerFunc
	asyncStart  dispatcher.HandlerFunc
	asyncCancel dispatcher.HandlerFunc
	node        relay.Node
}

// NewDispatcher creates a new function dispatcher with relay node for message routing.
func NewDispatcher(node relay.Node) *Dispatcher {
	d := &Dispatcher{node: node}
	d.call = d.handleCall
	d.asyncStart = d.handleAsyncStart
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
	register(function.Call, d.call)
	register(function.AsyncStart, d.asyncStart)
	register(function.AsyncCancel, d.asyncCancel)
}

func (d *Dispatcher) handleCall(ctx context.Context, cmd dispatcher.Command, complete dispatcher.Completer) error {
	callCmd := cmd.(*function.CallCmd)

	registry := function.GetRegistry(ctx)
	if registry == nil {
		complete.Complete(function.CallResult{Error: function.ErrRegistryNotFound}, nil)
		return nil
	}

	go func() {
		result, err := registry.Call(ctx, callCmd.Task)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			complete.Complete(function.CallResult{Error: err}, nil)
			return
		}
		if result.Error != nil {
			complete.Complete(function.CallResult{Error: result.Error}, nil)
			return
		}
		complete.Complete(function.CallResult{Value: result.Value}, nil)
	}()

	return nil
}

func (d *Dispatcher) handleAsyncStart(ctx context.Context, cmd dispatcher.Command, complete dispatcher.Completer) error {
	startCmd := cmd.(*function.AsyncStartCmd)

	registry := function.GetRegistry(ctx)
	if registry == nil {
		complete.Complete(function.AsyncStartResult{Error: function.ErrRegistryNotFound}, nil)
		return nil
	}

	if d.node == nil {
		complete.Complete(function.AsyncStartResult{Error: function.ErrNodeNotFound}, nil)
		return nil
	}

	pid, ok := runtime.GetFramePID(ctx)
	if !ok {
		complete.Complete(function.AsyncStartResult{Error: function.ErrPIDNotFound}, nil)
		return nil
	}

	topic := startCmd.Topic
	node := d.node

	// Copy task before goroutine - the yield/command will be released after handler returns
	task := startCmd.Task

	// Start async call in goroutine
	go func() {
		result, err := registry.Call(ctx, task)

		// Build result payload
		var resultPayload payload.Payload
		if err != nil {
			resultPayload = payload.NewError(err)
		} else if result.Error != nil {
			resultPayload = payload.NewError(result.Error)
		} else {
			resultPayload = result.Value
		}

		// Send result via relay node - routes based on pid.Host (function ID)
		pkg := relay.NewPackage(relay.PID{}, pid, topic, resultPayload, payload.NewTerminal())
		_ = node.Send(pkg)
	}()

	// Confirm start immediately
	complete.Complete(function.AsyncStartResult{}, nil)
	return nil
}

func (d *Dispatcher) handleAsyncCancel(ctx context.Context, cmd dispatcher.Command, complete dispatcher.Completer) error {
	cancelCmd := cmd.(*function.AsyncCancelCmd)

	if d.node == nil {
		complete.Complete(nil, nil)
		return nil
	}

	pid, ok := runtime.GetFramePID(ctx)
	if !ok {
		complete.Complete(nil, nil)
		return nil
	}

	topic := cancelCmd.Topic

	// Send terminal via relay node to close the channel
	pkg := relay.NewPackage(relay.PID{}, pid, topic, payload.NewTerminal())
	_ = d.node.Send(pkg)

	complete.Complete(nil, nil)
	return nil
}
