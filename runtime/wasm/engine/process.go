package engine

import (
	"context"
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	runtimewasm "github.com/wippyai/runtime/runtime/wasm"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
)

// Transport can map runtime payloads into call args and map call results back.
type Transport interface {
	Prepare(ctx context.Context, input payload.Payloads) ([]any, error)
	EncodeResult(ctx context.Context, result any) (payload.Payload, error)
}

// Process implements process.Process for synchronous WASM function calls.
type Process struct {
	module    *wasmrt.Module
	input     payload.Payloads
	ctx       context.Context
	result    payload.Payload
	method    string
	transport string
	limits    wasmapi.LimitsConfig
	executed  bool
}

// NewProcess creates a scheduler process that executes one WASM call per step loop.
func NewProcess(module *wasmrt.Module, transport string, limits wasmapi.LimitsConfig) *Process {
	return &Process{
		module:    module,
		transport: transport,
		limits:    limits,
	}
}

// Init captures call parameters for this process execution.
func (p *Process) Init(ctx context.Context, method string, input payload.Payloads) error {
	p.ctx = ctx
	p.method = method
	p.input = input
	p.result = nil
	p.executed = false
	return nil
}

// Step executes the WASM call once and then reports done.
func (p *Process) Step(_ []process.Event, out *process.StepOutput) error {
	if p.executed {
		out.Done(p.result)
		return nil
	}
	p.executed = true

	execCtx := p.ctx
	cancel := func() {}
	if p.limits.MaxExecutionMS > 0 {
		execCtx, cancel = context.WithTimeout(p.ctx, time.Duration(p.limits.MaxExecutionMS)*time.Millisecond)
	}
	defer cancel()

	inst, err := p.module.Instantiate(execCtx)
	if err != nil {
		return runtimewasm.NewInstantiateModuleError(err)
	}
	defer func() { _ = inst.Close(context.Background()) }()

	result, err := p.invoke(execCtx, inst)
	if err != nil {
		return err
	}

	p.result = result
	out.Done(result)
	return nil
}

// Close releases process resources.
func (p *Process) Close() {
	p.input = nil
	p.ctx = nil
	p.result = nil
}

func (p *Process) invoke(ctx context.Context, inst *wasmrt.Instance) (payload.Payload, error) {
	switch p.transport {
	case "", wasmapi.TransportTypePayload:
		return p.invokePayload(ctx, inst)
	default:
		return p.invokeCustomTransport(ctx, inst)
	}
}

func (p *Process) invokePayload(ctx context.Context, inst *wasmrt.Instance) (payload.Payload, error) {
	args := make([]any, 0, len(p.input))
	dtt := payload.GetTranscoder(ctx)

	for _, pl := range p.input {
		if pl == nil {
			continue
		}
		if pl.Format() != payload.Golang {
			if dtt == nil {
				return nil, runtimewasm.ErrTranscoderNotFound
			}
			transcoded, err := dtt.Transcode(pl, payload.Golang)
			if err != nil {
				return nil, runtimewasm.NewTranscodePayloadError(err)
			}
			pl = transcoded
		}
		args = append(args, pl.Data())
	}

	value, err := inst.Call(ctx, p.method, args...)
	if err != nil {
		return nil, runtimewasm.NewCallMethodError(p.method, err)
	}

	return payload.New(value), nil
}

func (p *Process) invokeCustomTransport(ctx context.Context, inst *wasmrt.Instance) (payload.Payload, error) {
	reg := wasmapi.GetTransportRegistry(ctx)
	if reg == nil {
		return nil, runtimewasm.ErrTransportRegistryNotFound
	}

	rawTransport, ok := reg.Get(p.transport)
	if !ok {
		return nil, runtimewasm.NewTransportNotFoundError(p.transport)
	}

	transport, ok := rawTransport.(Transport)
	if !ok {
		return nil, runtimewasm.NewTransportTypeError(p.transport)
	}

	args, err := transport.Prepare(ctx, p.input)
	if err != nil {
		return nil, runtimewasm.NewTransportPrepareError(err)
	}

	value, err := inst.Call(ctx, p.method, args...)
	if err != nil {
		return nil, runtimewasm.NewCallMethodError(p.method, err)
	}

	out, err := transport.EncodeResult(ctx, value)
	if err != nil {
		return nil, runtimewasm.NewTransportEncodeError(err)
	}
	if out == nil {
		return payload.New(value), nil
	}
	return out, nil
}

var _ process.Process = (*Process)(nil)
