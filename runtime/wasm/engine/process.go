package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	envapi "github.com/wippyai/runtime/api/env"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/runtime/runtime/security"
	runtimewasm "github.com/wippyai/runtime/runtime/wasm"
	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"
	wasmtransport "github.com/wippyai/runtime/runtime/wasm/transport"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
)

// Transport can map runtime payloads into call args and map call results back.
type Transport interface {
	Prepare(ctx context.Context, input payload.Payloads) ([]any, error)
	EncodeResult(ctx context.Context, result any) (payload.Payload, error)
}

// Process implements process.Process for WASM function execution.
// Asyncified modules run through session-based yield/resume; synchronous
// modules execute as direct calls.
type Process struct {
	module            *wasmrt.Module
	input             payload.Payloads
	ctx               context.Context
	execCtx           context.Context
	result            payload.Payload
	method            string
	transport         string
	wasi              wasmapi.WASIConfig
	limits            wasmapi.LimitsConfig
	fsReg             fsapi.Registry
	resolvedTransport Transport
	inst              *wasmrt.Instance
	session           *wasmrt.CallSession
	asyncValues       *wippyhost.AsyncValueStore
	callArgs          []any
	pendingYield      *wasmengine.YieldResult
	cancel            context.CancelFunc
	pendingTag        uint64
	yieldSeq          uint64
	waitingYield      bool
	done              bool
}

// NewProcess creates a scheduler process for WASM execution.
func NewProcess(
	module *wasmrt.Module,
	transport string,
	wasi wasmapi.WASIConfig,
	limits wasmapi.LimitsConfig,
	fsReg fsapi.Registry,
) *Process {
	return &Process{
		module:    module,
		transport: transport,
		wasi:      wasi,
		limits:    limits,
		fsReg:     fsReg,
	}
}

// Init captures call parameters for this process execution.
func (p *Process) Init(ctx context.Context, method string, input payload.Payloads) error {
	p.endExecution()
	p.ctx = ctx
	p.method = method
	p.input = input
	p.result = nil
	p.pendingYield = nil
	p.pendingTag = 0
	p.waitingYield = false
	p.done = false
	return nil
}

// Step advances process state using scheduler event completions.
func (p *Process) Step(events []process.Event, out *process.StepOutput) error {
	if p.done {
		out.Done(p.result)
		return nil
	}

	if err := p.applyEvents(events); err != nil {
		p.endExecution()
		return err
	}

	if p.inst == nil {
		if err := p.startExecution(); err != nil {
			p.endExecution()
			return err
		}
	}

	if p.session == nil {
		return p.stepSync(out)
	}
	return p.stepAsync(out)
}

// Close releases process resources.
func (p *Process) Close() {
	p.endExecution()
	p.input = nil
	p.ctx = nil
	p.execCtx = nil
	p.result = nil
}

func (p *Process) stepSync(out *process.StepOutput) error {
	value, err := p.inst.Call(p.execCtx, p.method, p.callArgs...)
	if err != nil {
		p.endExecution()
		return runtimewasm.NewCallMethodError(p.method, err)
	}

	result, err := p.encodeResult(p.execCtx, value)
	if err != nil {
		p.endExecution()
		return err
	}

	p.result = result
	p.done = true
	p.endExecution()
	out.Done(result)
	return nil
}

func (p *Process) stepAsync(out *process.StepOutput) error {
	if p.waitingYield && p.pendingYield == nil {
		out.WaitForYields()
		return nil
	}

	sr, err := p.session.Step(p.execCtx, p.pendingYield)
	p.pendingYield = nil
	if err != nil {
		p.endExecution()
		return runtimewasm.NewCallMethodError(p.method, err)
	}

	switch sr.Status {
	case wasmengine.StepContinue:
		cmd, bridgeErr := bridgePendingCommand(sr.PendingOp)
		if bridgeErr != nil {
			p.endExecution()
			return bridgeErr
		}

		p.yieldSeq++
		p.pendingTag = p.yieldSeq
		p.waitingYield = true
		out.Yield(cmd, p.pendingTag)
		out.WaitForYields()
		return nil

	case wasmengine.StepDone:
		value, liftErr := p.session.LiftResult(p.execCtx, sr.Results)
		if liftErr != nil {
			p.endExecution()
			return runtimewasm.NewCallMethodError(p.method, liftErr)
		}

		result, encErr := p.encodeResult(p.execCtx, value)
		if encErr != nil {
			p.endExecution()
			return encErr
		}

		p.result = result
		p.done = true
		p.endExecution()
		out.Done(result)
		return nil

	case wasmengine.StepIdle:
		out.Idle()
		return nil

	default:
		out.Continue()
		return nil
	}
}

func (p *Process) startExecution() error {
	execCtx := p.ctx
	cancel := func() {}
	if p.limits.MaxExecutionMS > 0 {
		execCtx, cancel = context.WithTimeout(p.ctx, time.Duration(p.limits.MaxExecutionMS)*time.Millisecond)
	}

	configuredCtx, err := p.withWASICallConfig(execCtx)
	if err != nil {
		cancel()
		return err
	}
	execCtx = configuredCtx

	p.asyncValues = wippyhost.NewAsyncValueStore()
	execCtx = wippyhost.WithAsyncValueStore(execCtx, p.asyncValues)

	inst, err := p.module.InstantiateWithAsyncify(execCtx)
	if err != nil {
		cancel()
		return runtimewasm.NewInstantiateModuleError(err)
	}

	args, err := p.prepareArgs(execCtx)
	if err != nil {
		_ = inst.Close(context.Background())
		cancel()
		return err
	}

	p.execCtx = execCtx
	p.cancel = cancel
	p.inst = inst
	p.callArgs = args

	if inst.Scheduler() == nil {
		return nil
	}

	session, err := inst.StartCall(execCtx, p.method, args...)
	if err != nil {
		p.endExecution()
		return runtimewasm.NewCallMethodError(p.method, err)
	}
	p.session = session
	return nil
}

func (p *Process) endExecution() {
	if p.inst != nil {
		_ = p.inst.Close(context.Background())
		p.inst = nil
	}
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.execCtx = nil
	p.session = nil
	p.callArgs = nil
	p.pendingYield = nil
	p.pendingTag = 0
	p.waitingYield = false
	if p.asyncValues != nil {
		p.asyncValues.Reset()
		p.asyncValues = nil
	}
}

func (p *Process) applyEvents(events []process.Event) error {
	if !p.waitingYield {
		return nil
	}

	for _, ev := range events {
		if ev.Type != process.EventYieldComplete {
			continue
		}
		if ev.Tag == 0 || ev.Tag != p.pendingTag {
			continue
		}

		value, err := p.resolveYieldValue(ev.Data)
		if err != nil {
			return runtimewasm.NewAsyncYieldResultTypeError(ev.Data)
		}

		p.pendingYield = &wasmengine.YieldResult{
			Value: value,
			Error: ev.Error,
		}
		p.waitingYield = false
		p.pendingTag = 0
		return nil
	}

	return nil
}

func (p *Process) withWASICallConfig(ctx context.Context) (context.Context, error) {
	cfg, err := p.resolveWASICallConfig(ctx)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return ctx, nil
	}
	return wippyhost.WithWASICallConfig(ctx, cfg), nil
}

func (p *Process) resolveWASICallConfig(ctx context.Context) (*wippyhost.WASICallConfig, error) {
	if p.wasi.Cwd == "" && len(p.wasi.Args) == 0 && len(p.wasi.Env) == 0 && len(p.wasi.Mounts) == 0 {
		return nil, nil
	}

	callCfg := &wippyhost.WASICallConfig{
		Cwd: p.wasi.Cwd,
	}
	if len(p.wasi.Args) > 0 {
		callCfg.Args = append([]string(nil), p.wasi.Args...)
	}

	if len(p.wasi.Env) > 0 {
		envReg := envapi.GetRegistry(ctx)
		if envReg == nil {
			return nil, runtimewasm.ErrEnvRegistryNotFound
		}

		callCfg.Env = make(map[string]string, len(p.wasi.Env))
		for _, item := range p.wasi.Env {
			id := item.ID.String()
			value, found, err := envReg.Lookup(ctx, id)
			if err != nil {
				if errors.Is(err, envapi.ErrVariableNotFound) {
					if item.Required {
						return nil, runtimewasm.NewWASIEnvRequiredNotFoundError(id)
					}
					continue
				}
				return nil, runtimewasm.NewWASIEnvLookupError(id, err)
			}
			if !found {
				if item.Required {
					return nil, runtimewasm.NewWASIEnvRequiredNotFoundError(id)
				}
				continue
			}
			callCfg.Env[item.Name] = value
		}
	}

	if len(p.wasi.Mounts) > 0 {
		callCfg.Mounts = make([]wippyhost.WASIMountBinding, 0, len(p.wasi.Mounts))
		for _, item := range p.wasi.Mounts {
			fsID := item.FS.String()
			if !security.IsAllowed(ctx, "fs.get", fsID, nil) {
				return nil, runtimewasm.NewFSAccessDeniedError(fsID)
			}
			if p.fsReg == nil {
				return nil, runtimewasm.ErrFSRegistryNotFound
			}
			fsys, ok := p.fsReg.GetFS(fsID)
			if !ok {
				return nil, runtimewasm.NewWASIMountFilesystemNotFoundError(fsID)
			}
			callCfg.Mounts = append(callCfg.Mounts, wippyhost.WASIMountBinding{
				Filesystem: fsys,
				Guest:      item.Guest,
				ReadOnly:   item.ReadOnly,
			})
		}
	}

	return callCfg, nil
}

func (p *Process) invokePayload(ctx context.Context, inst *wasmrt.Instance) (payload.Payload, error) {
	args, err := p.preparePayloadArgs(ctx)
	if err != nil {
		return nil, err
	}

	value, err := inst.Call(ctx, p.method, args...)
	if err != nil {
		return nil, runtimewasm.NewCallMethodError(p.method, err)
	}

	return payload.New(value), nil
}

func (p *Process) invokeCustomTransport(ctx context.Context, inst *wasmrt.Instance) (payload.Payload, error) {
	args, err := p.prepareCustomArgs(ctx)
	if err != nil {
		return nil, err
	}

	value, err := inst.Call(ctx, p.method, args...)
	if err != nil {
		return nil, runtimewasm.NewCallMethodError(p.method, err)
	}

	return p.encodeCustomResult(ctx, value)
}

func (p *Process) prepareArgs(ctx context.Context) ([]any, error) {
	switch p.transport {
	case "", wasmapi.TransportTypePayload:
		return p.preparePayloadArgs(ctx)
	default:
		return p.prepareCustomArgs(ctx)
	}
}

func (p *Process) encodeResult(ctx context.Context, value any) (payload.Payload, error) {
	switch p.transport {
	case "", wasmapi.TransportTypePayload:
		return payload.New(value), nil
	default:
		return p.encodeCustomResult(ctx, value)
	}
}

func (p *Process) preparePayloadArgs(ctx context.Context) ([]any, error) {
	return wasmtransport.PayloadsToArgs(ctx, p.input)
}

func (p *Process) prepareCustomArgs(ctx context.Context) ([]any, error) {
	transport := p.resolvedTransport
	if transport == nil {
		reg := wasmapi.GetTransportRegistry(ctx)
		if reg == nil {
			return nil, runtimewasm.ErrTransportRegistryNotFound
		}

		rawTransport, ok := reg.Get(p.transport)
		if !ok {
			return nil, runtimewasm.NewTransportNotFoundError(p.transport)
		}

		var castOK bool
		transport, castOK = rawTransport.(Transport)
		if !castOK {
			return nil, runtimewasm.NewTransportTypeError(p.transport)
		}
		p.resolvedTransport = transport
	}

	args, err := transport.Prepare(ctx, p.input)
	if err != nil {
		return nil, runtimewasm.NewTransportPrepareError(err)
	}
	return args, nil
}

func (p *Process) encodeCustomResult(ctx context.Context, value any) (payload.Payload, error) {
	transport := p.resolvedTransport
	if transport == nil {
		reg := wasmapi.GetTransportRegistry(ctx)
		if reg == nil {
			return nil, runtimewasm.ErrTransportRegistryNotFound
		}
		rawTransport, ok := reg.Get(p.transport)
		if !ok {
			return nil, runtimewasm.NewTransportNotFoundError(p.transport)
		}
		castTransport, castOK := rawTransport.(Transport)
		if !castOK {
			return nil, runtimewasm.NewTransportTypeError(p.transport)
		}
		transport = castTransport
		p.resolvedTransport = transport
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

func (p *Process) resolveYieldValue(data any) (uint64, error) {
	value, err := yieldResultValue(data)
	if err == nil {
		return value, nil
	}

	if p.asyncValues == nil {
		return 0, fmt.Errorf("non-numeric yield result requires async value store: %T", data)
	}

	return p.asyncValues.Put(data), nil
}

func bridgePendingCommand(op wasmengine.PendingOp) (dispatcher.Command, error) {
	if op == nil {
		return nil, runtimewasm.NewAsyncPendingCommandError(nil)
	}

	withCommand, ok := op.(interface{ ToCommand() dispatcher.Command })
	if !ok {
		return nil, runtimewasm.NewAsyncPendingCommandError(op)
	}

	cmd := withCommand.ToCommand()
	if cmd == nil {
		return nil, runtimewasm.NewAsyncPendingCommandError(op)
	}
	return cmd, nil
}

func yieldResultValue(data any) (uint64, error) {
	switch v := data.(type) {
	case nil:
		return 0, nil
	case uint64:
		return v, nil
	case uint32:
		return uint64(v), nil
	case uint16:
		return uint64(v), nil
	case uint8:
		return uint64(v), nil
	case int:
		if v < 0 {
			return 0, fmt.Errorf("negative int value")
		}
		return uint64(v), nil
	case int64:
		if v < 0 {
			return 0, fmt.Errorf("negative int64 value")
		}
		return uint64(v), nil
	case int32:
		if v < 0 {
			return 0, fmt.Errorf("negative int32 value")
		}
		return uint64(v), nil
	case int16:
		if v < 0 {
			return 0, fmt.Errorf("negative int16 value")
		}
		return uint64(v), nil
	case int8:
		if v < 0 {
			return 0, fmt.Errorf("negative int8 value")
		}
		return uint64(v), nil
	default:
		return 0, fmt.Errorf("unsupported type %T", data)
	}
}

var _ process.Process = (*Process)(nil)
