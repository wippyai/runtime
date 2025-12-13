// Package function provides function call command handlers for the dispatcher system.
package function

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"go.uber.org/zap"
)

// Dispatcher handles function call commands.
type Dispatcher struct {
	call        dispatcher.HandlerFunc
	asyncStart  dispatcher.HandlerFunc
	asyncCancel dispatcher.HandlerFunc
	node        relay.Node
	logger      *zap.Logger
}

// NewDispatcher creates a new function dispatcher with relay node for message routing.
func NewDispatcher(node relay.Node, logger *zap.Logger) *Dispatcher {
	if logger == nil {
		logger = zap.NewNop()
	}
	d := &Dispatcher{node: node, logger: logger}
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

func (d *Dispatcher) handleCall(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	callCmd := cmd.(*function.CallCmd)

	registry := function.GetRegistry(ctx)
	if registry == nil {
		receiver.CompleteYield(tag, function.CallResult{Error: function.ErrRegistryNotFound}, nil)
		return nil
	}

	var closer ctxapi.Closer
	if fc := ctxapi.FrameFromContext(ctx); fc != nil {
		closer = fc.IncRef()
	}

	go func() {
		if closer != nil {
			defer closer.Close()
		}
		result, err := registry.Call(ctx, callCmd.Task)
		if ctx.Err() != nil {
			receiver.CompleteYield(tag, function.CallResult{Error: ctx.Err()}, nil)
			return
		}
		if err != nil {
			receiver.CompleteYield(tag, function.CallResult{Error: err}, nil)
			return
		}
		if result.Error != nil {
			receiver.CompleteYield(tag, function.CallResult{Error: result.Error}, nil)
			return
		}
		receiver.CompleteYield(tag, function.CallResult{Value: result.Value}, nil)
	}()

	return nil
}

func (d *Dispatcher) handleAsyncStart(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	startCmd := cmd.(*function.AsyncStartCmd)

	registry := function.GetRegistry(ctx)
	if registry == nil {
		receiver.CompleteYield(tag, function.AsyncStartResult{Error: function.ErrRegistryNotFound}, nil)
		return nil
	}

	if d.node == nil {
		receiver.CompleteYield(tag, function.AsyncStartResult{Error: function.ErrNodeNotFound}, nil)
		return nil
	}

	framePID, ok := runtime.GetFramePID(ctx)
	if !ok {
		receiver.CompleteYield(tag, function.AsyncStartResult{Error: function.ErrPIDNotFound}, nil)
		return nil
	}

	topic := startCmd.Topic
	node := d.node
	task := startCmd.Task
	logger := d.logger

	var closer ctxapi.Closer
	if fc := ctxapi.FrameFromContext(ctx); fc != nil {
		closer = fc.IncRef()
	}

	go func() {
		if closer != nil {
			defer closer.Close()
		}
		result, err := registry.Call(ctx, task)

		var resultPayload payload.Payload
		switch {
		case err != nil:
			resultPayload = payload.NewError(err)
		case result.Error != nil:
			resultPayload = payload.NewError(result.Error)
		default:
			resultPayload = result.Value
		}

		pkg := relay.NewPackage(pid.PID{}, framePID, topic, resultPayload, payload.NewTerminal())
		if err := node.Send(pkg); err != nil {
			logger.Warn("failed to send async result",
				zap.String("topic", string(topic)),
				zap.String("target", framePID.String()),
				zap.Error(err))
		}
	}()

	receiver.CompleteYield(tag, function.AsyncStartResult{}, nil)
	return nil
}

func (d *Dispatcher) handleAsyncCancel(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	cancelCmd := cmd.(*function.AsyncCancelCmd)

	if d.node == nil {
		receiver.CompleteYield(tag, nil, nil)
		return nil
	}

	framePID, ok := runtime.GetFramePID(ctx)
	if !ok {
		receiver.CompleteYield(tag, nil, nil)
		return nil
	}

	topic := cancelCmd.Topic

	pkg := relay.NewPackage(pid.PID{}, framePID, topic, payload.NewTerminal())
	if err := d.node.Send(pkg); err != nil {
		d.logger.Warn("failed to send async cancel",
			zap.String("topic", string(topic)),
			zap.String("target", framePID.String()),
			zap.Error(err))
	}

	receiver.CompleteYield(tag, nil, nil)
	return nil
}
