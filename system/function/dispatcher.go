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
	node   relay.Node
	logger *zap.Logger
}

// NewDispatcher creates a new function dispatcher with relay node for message routing.
func NewDispatcher(node relay.Node, logger *zap.Logger) *Dispatcher {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Dispatcher{node: node, logger: logger}
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
	register(function.Call, dispatcher.HandlerFunc(d.handleCall))
	register(function.AsyncStart, dispatcher.HandlerFunc(d.handleAsyncStart))
	register(function.AsyncCancel, dispatcher.HandlerFunc(d.handleAsyncCancel))
}

// acquireCloser gets a frame closer if available.
func acquireCloser(ctx context.Context) ctxapi.Closer {
	if fc := ctxapi.FrameFromContext(ctx); fc != nil {
		return fc.IncRef()
	}
	return nil
}

// extractCallError returns the first non-nil error from context, err, or result.
func extractCallError(ctx context.Context, result *runtime.Result, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return err
	}
	if result != nil && result.Error != nil {
		return result.Error
	}
	return nil
}

func (d *Dispatcher) handleCall(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	callCmd := cmd.(*function.CallCmd)

	registry := function.GetRegistry(ctx)
	if registry == nil {
		receiver.CompleteYield(tag, function.CallResult{Error: function.ErrRegistryNotFound}, nil)
		return nil
	}

	closer := acquireCloser(ctx)
	go func() {
		if closer != nil {
			defer closer.Close()
		}
		result, err := registry.Call(ctx, callCmd.Task)
		if callErr := extractCallError(ctx, result, err); callErr != nil {
			receiver.CompleteYield(tag, function.CallResult{Error: callErr}, nil)
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
	closer := acquireCloser(ctx)

	go func() {
		if closer != nil {
			defer closer.Close()
		}
		result, err := registry.Call(ctx, task)

		resultPayload := resultToPayload(result, err)
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

// resultToPayload converts result/error to payload.
func resultToPayload(result *runtime.Result, err error) payload.Payload {
	if err != nil {
		return payload.NewError(err)
	}
	if result != nil && result.Error != nil {
		return payload.NewError(result.Error)
	}
	if result != nil {
		return result.Value
	}
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
