// Package engine provides WASM process implementation for wippy scheduler.
package engine

import (
	"context"

	"github.com/tetratelabs/wazero/api"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/runtime/resource"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
)

// Process implements process.Process for WASM execution with asyncify support.
type Process struct {
	runtime   *wasmrt.Runtime
	module    *wasmrt.Module
	instance  *wasmrt.Instance
	transport wasmapi.Transport
	store     *resource.Store

	ctx      context.Context
	method   string
	fn       api.Function
	fnArgs   []uint64
	asyncify *wasmengine.Asyncify

	// Async state
	pendingCmd dispatcher.Command
	result     uint64
	resultErr  error

	started bool
	tag     uint64
}

// NewProcess creates a new WASM process.
func NewProcess(runtime *wasmrt.Runtime, module *wasmrt.Module) *Process {
	return &Process{
		runtime: runtime,
		module:  module,
	}
}

// NewProcessWithTransport creates a new WASM process with a specific transport.
func NewProcessWithTransport(runtime *wasmrt.Runtime, module *wasmrt.Module, transport wasmapi.Transport) *Process {
	return &Process{
		runtime:   runtime,
		module:    module,
		transport: transport,
	}
}

// Init prepares the process for execution with method and input.
func (p *Process) Init(ctx context.Context, method string, input payload.Payloads) error {
	p.ctx = ctx
	p.method = method

	// Instantiate module if not already done
	if p.instance == nil {
		inst, err := p.module.InstantiateWithAsyncify(ctx)
		if err != nil {
			return NewInstantiateError(err)
		}
		p.instance = inst

		// Create resource store for transport
		if p.transport != nil {
			p.store = resource.NewStore()
		}

		// Get asyncify from instance
		p.asyncify = inst.Asyncify()
	} else {
		// Reset state for reuse
		p.reset()
	}

	// Get exported function
	rawFn := p.instance.GetExportedFunction(method)
	if rawFn == nil {
		return NewFunctionNotFoundError(method)
	}
	fn, ok := rawFn.(api.Function)
	if !ok {
		return NewFunctionTypeError(method)
	}
	p.fn = fn

	// Prepare arguments
	if p.transport != nil {
		var args []uint64
		if p.fnArgs != nil {
			args = p.fnArgs[:0]
		}
		args, err := p.transport.Prepare(ctx, p.store, input, args)
		if err != nil {
			return NewTransportPrepareError(err)
		}
		p.fnArgs = args
	} else {
		// Default: transcode payloads to Go types for simple functions
		if err := p.prepareArgs(ctx, input); err != nil {
			return err
		}
	}

	return nil
}

// Step advances the process state machine.
func (p *Process) Step(events []process.Event, out *process.StepOutput) error {
	// Process yield completion events
	for _, evt := range events {
		if evt.Type == process.EventYieldComplete {
			p.result = 0
			p.resultErr = evt.Error
			if evt.Data != nil {
				switch v := evt.Data.(type) {
				case uint64:
					p.result = v
				case int64:
					p.result = uint64(v)
				}
			}
			break
		}
	}

	// No asyncify - simple synchronous execution
	if p.asyncify == nil {
		return p.stepSync(out)
	}

	// Asyncify-enabled execution
	return p.stepAsync(out)
}

// stepSync handles synchronous (non-asyncify) execution.
func (p *Process) stepSync(out *process.StepOutput) error {
	if p.started {
		out.Done(nil)
		return nil
	}
	if p.fn == nil {
		return NewSchedulerStateError("process not initialized")
	}
	p.started = true

	results, err := p.fn.Call(p.ctx, p.fnArgs...)
	if err != nil {
		out.Done(nil)
		return err
	}

	if len(results) > 0 {
		out.Done(payload.New(results[0]))
	} else {
		out.Done(nil)
	}
	return nil
}

// stepAsync handles asyncify-enabled execution.
func (p *Process) stepAsync(out *process.StepOutput) error {
	if p.fn == nil {
		return NewSchedulerStateError("process not initialized")
	}

	ctx := wasmapi.WithAsyncFrame(p.ctx, &wasmapi.AsyncFrame{
		Asyncify:  p.asyncify,
		Scheduler: p,
	})

	// First call - reset asyncify stack
	if !p.started {
		p.started = true
		p.asyncify.ResetStack()
	}

	// Resume with results if rewinding
	if p.resultErr != nil {
		out.Done(nil)
		return p.resultErr
	}

	// Start rewind if we have pending results
	if p.asyncify.IsNormal(ctx) && p.pendingCmd != nil {
		if err := p.asyncify.StartRewind(ctx); err != nil {
			out.Done(nil)
			return NewSchedulerRewindError(err)
		}
	}

	// Call the function
	results, callErr := p.fn.Call(ctx, p.fnArgs...)

	// Check if unwinding (host function triggered suspend)
	if p.asyncify.IsUnwinding(ctx) {
		if err := p.asyncify.StopUnwind(ctx); err != nil {
			out.Done(nil)
			return NewSchedulerUnwindError(err)
		}
		if p.pendingCmd == nil {
			out.Done(nil)
			return NewSchedulerStateError("no pending command after unwind")
		}
		cmd := p.pendingCmd
		p.tag++
		out.Yield(cmd, p.tag)
		out.WaitForYields()
		return nil
	}

	// Real error
	if callErr != nil {
		out.Done(nil)
		return callErr
	}

	// Normal completion
	p.pendingCmd = nil
	if len(results) > 0 {
		out.Done(payload.New(results[0]))
	} else {
		out.Done(nil)
	}
	return nil
}

// SetPending stores a command to yield. Called by host functions.
func (p *Process) SetPending(cmd dispatcher.Command) {
	p.pendingCmd = cmd
}

// GetResult returns the result from last yield completion.
func (p *Process) GetResult() (uint64, error) {
	return p.result, p.resultErr
}

// ClearPending clears the pending state after resume.
func (p *Process) ClearPending() {
	p.pendingCmd = nil
	p.result = 0
	p.resultErr = nil
}

// Close releases process resources.
func (p *Process) Close() {
	if p.instance != nil {
		p.instance.Close(context.Background())
		p.instance = nil
	}
	p.ctx = nil
}

// reset clears state for process reuse.
func (p *Process) reset() {
	p.started = false
	p.fn = nil
	if p.fnArgs != nil {
		p.fnArgs = p.fnArgs[:0]
	}
	p.tag = 0
	p.pendingCmd = nil
	p.result = 0
	p.resultErr = nil
	if p.store != nil {
		p.store.Table().Reset()
	}
	if p.asyncify != nil {
		p.asyncify.ResetStack()
	}
}

// prepareArgs converts input payloads to WASM arguments.
func (p *Process) prepareArgs(ctx context.Context, input payload.Payloads) error {
	dtt := payload.GetTranscoder(ctx)
	if p.fnArgs != nil {
		p.fnArgs = p.fnArgs[:0]
	}

	for _, pl := range input {
		if pl == nil {
			continue
		}
		// Transcode to Golang format if needed
		if pl.Format() != payload.Golang && dtt != nil {
			transcoded, err := dtt.Transcode(pl, payload.Golang)
			if err != nil {
				return NewTranscodeError(err)
			}
			pl = transcoded
		}
		if data := pl.Data(); data != nil {
			switch v := data.(type) {
			case uint64:
				p.fnArgs = append(p.fnArgs, v)
			case int64:
				p.fnArgs = append(p.fnArgs, uint64(v))
			case int:
				p.fnArgs = append(p.fnArgs, uint64(v))
			case uint32:
				p.fnArgs = append(p.fnArgs, uint64(v))
			case int32:
				p.fnArgs = append(p.fnArgs, uint64(v))
			case float64:
				p.fnArgs = append(p.fnArgs, uint64(v))
			case float32:
				p.fnArgs = append(p.fnArgs, uint64(v))
			}
		}
	}
	return nil
}

// compile-time check
var _ process.Process = (*Process)(nil)
var _ wasmapi.AsyncScheduler = (*Process)(nil)
