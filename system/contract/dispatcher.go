package contract

import (
	"context"

	"github.com/wippyai/runtime/api/contract"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	pidpkg "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

// Dispatcher handles contract commands.
type Dispatcher struct {
	open        dispatcher.HandlerFunc
	call        dispatcher.HandlerFunc
	asyncCall   dispatcher.HandlerFunc
	asyncCancel dispatcher.HandlerFunc
	node        relay.Node
}

// NewDispatcher creates a new contract dispatcher with relay node for async routing.
func NewDispatcher(node relay.Node) *Dispatcher {
	d := &Dispatcher{node: node}
	d.open = d.handleOpen
	d.call = d.handleCall
	d.asyncCall = d.handleAsyncCall
	d.asyncCancel = d.handleAsyncCancel
	return d
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
	register(contract.Open, d.open)
	register(contract.Call, d.call)
	register(contract.AsyncCall, d.asyncCall)
	register(contract.AsyncCancel, d.asyncCancel)
}

func (d *Dispatcher) handleOpen(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	openCmd := cmd.(*contract.OpenCmd)

	instantiator := contract.GetInstantiator(ctx)
	if instantiator == nil {
		receiver.CompleteYield(tag, contract.OpenResult{Error: ErrInstantiatorNotFound}, nil)
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
		receiver.CompleteYield(tag, contract.CallResult{Error: ErrInstanceNil}, nil)
		return nil
	}

	go func() {
		result, err := callCmd.Instance.Call(ctx, callCmd.Method, callCmd.Args)
		if ctx.Err() != nil {
			receiver.CompleteYield(tag, contract.CallResult{Error: ctx.Err()}, nil)
			return
		}
		if err != nil {
			receiver.CompleteYield(tag, contract.CallResult{Error: err}, nil)
			return
		}
		if result.Error != nil {
			receiver.CompleteYield(tag, contract.CallResult{Error: result.Error}, nil)
			return
		}
		receiver.CompleteYield(tag, contract.CallResult{Value: result.Value}, nil)
	}()

	return nil
}

func (d *Dispatcher) handleAsyncCall(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	asyncCmd := cmd.(*contract.AsyncCallCmd)

	if asyncCmd.Instance == nil {
		receiver.CompleteYield(tag, contract.AsyncCallResult{Error: ErrInstanceNil}, nil)
		return nil
	}

	if d.node == nil {
		receiver.CompleteYield(tag, contract.AsyncCallResult{Error: ErrNodeNotFound}, nil)
		return nil
	}

	pid, ok := runtime.GetFramePID(ctx)
	if !ok {
		receiver.CompleteYield(tag, contract.AsyncCallResult{Error: ErrPIDNotFound}, nil)
		return nil
	}

	topic := asyncCmd.Topic
	node := d.node
	instance := asyncCmd.Instance
	method := asyncCmd.Method
	args := asyncCmd.Args

	go func() {
		result, err := instance.Call(ctx, method, args)

		var resultPayload payload.Payload
		switch {
		case err != nil:
			resultPayload = payload.NewError(err)
		case result.Error != nil:
			resultPayload = payload.NewError(result.Error)
		default:
			resultPayload = result.Value
		}

		pkg := relay.NewPackage(pidpkg.PID{}, pid, topic, resultPayload, payload.NewTerminal())
		_ = node.Send(pkg)
	}()

	receiver.CompleteYield(tag, contract.AsyncCallResult{}, nil)
	return nil
}

func (d *Dispatcher) handleAsyncCancel(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	cancelCmd := cmd.(*contract.AsyncCancelCmd)

	if d.node == nil {
		receiver.CompleteYield(tag, nil, nil)
		return nil
	}

	pid, ok := runtime.GetFramePID(ctx)
	if !ok {
		receiver.CompleteYield(tag, nil, nil)
		return nil
	}

	pkg := relay.NewPackage(pidpkg.PID{}, pid, cancelCmd.Topic, payload.NewTerminal())
	_ = d.node.Send(pkg)

	receiver.CompleteYield(tag, nil, nil)
	return nil
}

// Sentinel errors for dispatcher
var (
	ErrInstantiatorNotFound = &Error{message: "contract instantiator not found in context"}
	ErrInstanceNil          = &Error{message: "contract instance is nil"}
	ErrNodeNotFound         = &Error{message: "relay node not found"}
	ErrPIDNotFound          = &Error{message: "process PID not found in context"}
)
