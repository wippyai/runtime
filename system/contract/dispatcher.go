// SPDX-License-Identifier: MPL-2.0

package contract

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/contract"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"go.uber.org/zap"
)

// Dispatcher handles contract commands.
type Dispatcher struct {
	node   relay.Node
	logger *zap.Logger
}

// NewDispatcher creates a new contract dispatcher with relay node for async routing.
func NewDispatcher(node relay.Node, logger *zap.Logger) *Dispatcher {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Dispatcher{node: node, logger: logger}
}

// Start is a no-op for contract dispatcher.
func (d *Dispatcher) Start(_ context.Context) error {
	return nil
}

// Stop is a no-op for contract dispatcher.
func (d *Dispatcher) Stop(_ context.Context) error {
	return nil
}

// RegisterAll registers all contract command handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(contract.Open, dispatcher.HandlerFunc(d.handleOpen))
	register(contract.Call, dispatcher.HandlerFunc(d.handleCall))
	register(contract.AsyncCall, dispatcher.HandlerFunc(d.handleAsyncCall))
	register(contract.AsyncCancel, dispatcher.HandlerFunc(d.handleAsyncCancel))
}

func (d *Dispatcher) handleOpen(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	openCmd := cmd.(*contract.OpenCmd)

	instantiator := contract.GetInstantiator(ctx)
	if instantiator == nil {
		receiver.CompleteYield(tag, contract.OpenResult{Error: contract.ErrInstantiatorNotFound}, nil)
		return nil
	}

	callCtx, fc := ctxapi.ForkFrameContext(ctx)

	go func(callCtx context.Context, callFC ctxapi.FrameContext) {
		defer ctxapi.ReleaseFrameContext(callFC)
		instance, err := instantiator.Instantiate(callCtx, openCmd.BindingID, openCmd.Scope)
		if callCtx.Err() != nil {
			receiver.CompleteYield(tag, contract.OpenResult{Error: callCtx.Err()}, nil)
			return
		}
		if err != nil {
			receiver.CompleteYield(tag, contract.OpenResult{Error: err}, nil)
			return
		}
		receiver.CompleteYield(tag, contract.OpenResult{Instance: instance}, nil)
	}(callCtx, fc)

	return nil
}

func (d *Dispatcher) handleCall(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	callCmd := cmd.(*contract.CallCmd)

	if callCmd.Instance == nil {
		receiver.CompleteYield(tag, contract.CallResult{Error: contract.ErrInstanceNil}, nil)
		return nil
	}

	callCtx, fc := ctxapi.ForkFrameContext(ctx)

	go func(callCtx context.Context, callFC ctxapi.FrameContext) {
		defer ctxapi.ReleaseFrameContext(callFC)
		result, err := callCmd.Instance.Call(callCtx, callCmd.Method, callCmd.Args)
		if callCtx.Err() != nil {
			receiver.CompleteYield(tag, contract.CallResult{Error: callCtx.Err()}, nil)
			return
		}
		if err != nil {
			receiver.CompleteYield(tag, contract.CallResult{Error: err}, nil)
			return
		}
		if result != nil && result.Error != nil {
			receiver.CompleteYield(tag, contract.CallResult{Error: result.Error}, nil)
			return
		}
		if result != nil {
			receiver.CompleteYield(tag, contract.CallResult{Value: result.Value}, nil)
		} else {
			receiver.CompleteYield(tag, contract.CallResult{}, nil)
		}
	}(callCtx, fc)

	return nil
}

func (d *Dispatcher) handleAsyncCall(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	asyncCmd := cmd.(*contract.AsyncCallCmd)

	if asyncCmd.Instance == nil {
		receiver.CompleteYield(tag, contract.AsyncCallResult{Error: contract.ErrInstanceNil}, nil)
		return nil
	}

	if d.node == nil {
		receiver.CompleteYield(tag, contract.AsyncCallResult{Error: contract.ErrNodeNotFound}, nil)
		return nil
	}

	framePID, ok := runtime.GetFramePID(ctx)
	if !ok {
		receiver.CompleteYield(tag, contract.AsyncCallResult{Error: contract.ErrPIDNotFound}, nil)
		return nil
	}

	topic := asyncCmd.Topic
	node := d.node
	instance := asyncCmd.Instance
	method := asyncCmd.Method
	args := asyncCmd.Args
	logger := d.logger

	callCtx, fc := ctxapi.ForkFrameContext(ctx)

	go func(callCtx context.Context, callFC ctxapi.FrameContext) {
		defer ctxapi.ReleaseFrameContext(callFC)
		result, err := instance.Call(callCtx, method, args)

		resultPayload := resultToPayload(result, err)
		if err := sendAsyncResult(node, framePID, topic, resultPayload); err != nil {
			logger.Warn("failed to send async result",
				zap.String("topic", topic),
				zap.String("target", framePID.String()),
				zap.Error(err))
		}
	}(callCtx, fc)

	receiver.CompleteYield(tag, contract.AsyncCallResult{}, nil)
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
	cancelCmd := cmd.(*contract.AsyncCancelCmd)

	if d.node == nil {
		receiver.CompleteYield(tag, nil, nil)
		return nil
	}

	framePID, ok := runtime.GetFramePID(ctx)
	if !ok {
		receiver.CompleteYield(tag, nil, nil)
		return nil
	}

	if err := sendAsyncCancel(d.node, framePID, cancelCmd.Topic); err != nil {
		d.logger.Warn("failed to send async cancel",
			zap.String("topic", cancelCmd.Topic),
			zap.String("target", framePID.String()),
			zap.Error(err))
	}

	receiver.CompleteYield(tag, nil, nil)
	return nil
}

func sendAsyncResult(node relay.Node, target pid.PID, topic string, result payload.Payload) error {
	pkg := relay.NewPackage(pid.PID{}, target, topic, result, payload.NewTerminal())
	return node.Send(pkg)
}

func sendAsyncCancel(node relay.Node, target pid.PID, topic string) error {
	pkg := relay.NewPackage(pid.PID{}, target, topic, payload.NewTerminal())
	return node.Send(pkg)
}
