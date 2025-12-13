package contract

import (
	"context"

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
	register(contract.CmdOpen, dispatcher.HandlerFunc(d.handleOpen))
	register(contract.CmdCall, dispatcher.HandlerFunc(d.handleCall))
	register(contract.CmdAsyncCall, dispatcher.HandlerFunc(d.handleAsyncCall))
	register(contract.CmdAsyncCancel, dispatcher.HandlerFunc(d.handleAsyncCancel))
}

func (d *Dispatcher) handleOpen(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	openCmd := cmd.(*contract.OpenCmd)

	instantiator := contract.GetInstantiator(ctx)
	if instantiator == nil {
		receiver.CompleteYield(tag, contract.OpenResult{Error: contract.ErrInstantiatorNotFound}, nil)
		return nil
	}

	go func() {
		instance, err := instantiator.Instantiate(ctx, openCmd.BindingID, openCmd.Scope)
		if ctx.Err() != nil {
			receiver.CompleteYield(tag, contract.OpenResult{Error: ctx.Err()}, nil)
			return
		}
		if err != nil {
			receiver.CompleteYield(tag, contract.OpenResult{Error: err}, nil)
			return
		}
		receiver.CompleteYield(tag, contract.OpenResult{Instance: instance}, nil)
	}()

	return nil
}

func (d *Dispatcher) handleCall(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	callCmd := cmd.(*contract.CallCmd)

	if callCmd.Instance == nil {
		receiver.CompleteYield(tag, contract.CallResult{Error: contract.ErrInstanceNil}, nil)
		return nil
	}

	go func() {
		result, err := callCmd.Instance.Call(ctx, callCmd.Method, callCmd.Args)
		if callErr := extractCallError(ctx, result, err); callErr != nil {
			receiver.CompleteYield(tag, contract.CallResult{Error: callErr}, nil)
			return
		}
		receiver.CompleteYield(tag, contract.CallResult{Value: result.Value}, nil)
	}()

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

	go func() {
		result, err := instance.Call(ctx, method, args)

		resultPayload := resultToPayload(result, err)
		pkg := relay.NewPackage(pid.PID{}, framePID, topic, resultPayload, payload.NewTerminal())
		if err := node.Send(pkg); err != nil {
			logger.Warn("failed to send async result",
				zap.String("topic", string(topic)),
				zap.String("target", framePID.String()),
				zap.Error(err))
		}
	}()

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

	pkg := relay.NewPackage(pid.PID{}, framePID, cancelCmd.Topic, payload.NewTerminal())
	if err := d.node.Send(pkg); err != nil {
		d.logger.Warn("failed to send async cancel",
			zap.String("topic", string(cancelCmd.Topic)),
			zap.String("target", framePID.String()),
			zap.Error(err))
	}

	receiver.CompleteYield(tag, nil, nil)
	return nil
}
