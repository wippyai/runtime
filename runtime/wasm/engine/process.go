// Package engine provides WASM process implementation for wippy scheduler.
package engine

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero/api"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
)

// Process implements process.Process for WASM execution with asyncify support.
type Process struct {
	runtime   *wasmrt.Runtime
	module    *wasmrt.Module
	instance  *wasmrt.Instance
	method    string
	args      []any
	ctx       context.Context
	result    any
	err       error
	asyncify  *wasmengine.Asyncify
	scheduler *Scheduler
	fn        api.Function
	fnArgs    []uint64
	started   bool
}

// NewProcess creates a new WASM process.
func NewProcess(runtime *wasmrt.Runtime, module *wasmrt.Module) *Process {
	return &Process{
		runtime: runtime,
		module:  module,
	}
}

// Init pre-instantiates the WASM module for reuse across calls.
// Called once per worker - instance is reused for all subsequent Execute calls.
func (p *Process) Init(ctx context.Context) error {
	inst, err := p.module.Instantiate(ctx)
	if err != nil {
		return fmt.Errorf("instantiate module: %w", err)
	}
	p.instance = inst

	// Try to enable asyncify
	if err := inst.EnableAsyncify(wasmengine.AsyncifyConfig{
		StackSize: wasmengine.AsyncifyDefaultStackSize,
		DataAddr:  wasmengine.AsyncifyDataAddr,
	}); err == nil {
		p.asyncify = inst.Asyncify()
		p.scheduler = NewScheduler(p.asyncify)
	}

	return nil
}

// Execute starts execution with context, method and input payloads.
// Reuses existing instance if Init was called.
func (p *Process) Execute(ctx context.Context, method string, input payload.Payloads) error {
	p.ctx = ctx
	p.method = method

	// Lazy init if not pre-initialized
	if p.instance == nil {
		if err := p.Init(ctx); err != nil {
			return err
		}
	} else {
		// Reset state for reuse
		p.started = false
		p.result = nil
		p.err = nil
		p.fn = nil
		p.fnArgs = p.fnArgs[:0]
		if p.scheduler != nil {
			p.scheduler.Reset()
		}
	}

	// Convert input payloads to Go args using transcoder
	dtt := payload.GetTranscoder(ctx)
	p.args = make([]any, 0, len(input))
	for _, pl := range input {
		if pl == nil {
			continue
		}
		// Transcode to Golang format if needed
		if pl.Format() != payload.Golang && dtt != nil {
			transcoded, err := dtt.Transcode(pl, payload.Golang)
			if err != nil {
				return fmt.Errorf("transcode payload: %w", err)
			}
			pl = transcoded
		}
		if data := pl.Data(); data != nil {
			p.args = append(p.args, data)
		}
	}

	return nil
}

// Step advances the process by one iteration.
func (p *Process) Step(results *process.YieldResults) (process.StepResult, error) {
	var result process.StepResult

	// No asyncify support - simple call
	if p.scheduler == nil {
		if p.started {
			result.Status = process.StepDone
			if p.result != nil {
				result.Result = payload.New(p.result)
			}
			return result, p.err
		}
		p.started = true
		p.result, p.err = p.instance.Call(p.ctx, p.method, p.args...)
		result.Status = process.StepDone
		if p.result != nil {
			result.Result = payload.New(p.result)
		}
		return result, p.err
	}

	// Asyncify-enabled execution
	ctx := WithAsyncify(p.ctx, p.asyncify)
	ctx = WithScheduler(ctx, p.scheduler)

	// First call - initialize scheduler
	if !p.started {
		p.started = true
		rawFn := p.instance.GetExportedFunction(p.method)
		if rawFn == nil {
			p.err = fmt.Errorf("function %q not found", p.method)
			result.Status = process.StepDone
			return result, p.err
		}
		fn, ok := rawFn.(api.Function)
		if !ok {
			p.err = fmt.Errorf("function %q has unexpected type", p.method)
			result.Status = process.StepDone
			return result, p.err
		}
		p.fn = fn
		if err := p.scheduler.Execute(ctx, fn, p.fnArgs...); err != nil {
			p.err = err
			result.Status = process.StepDone
			return result, err
		}
	}

	// Convert yield results to scheduler format
	var yr *YieldResult
	if results != nil {
		if results.Data != nil {
			switch v := results.Data.(type) {
			case uint64:
				yr = &YieldResult{Value: v}
			case int64:
				yr = &YieldResult{Value: uint64(v)}
			}
		}
		if results.Error != nil {
			yr = &YieldResult{Error: results.Error}
		}
	}

	// Step the scheduler
	sr, err := p.scheduler.Step(ctx, yr)
	if err != nil {
		p.err = err
		result.Status = process.StepDone
		return result, err
	}

	switch sr.Status {
	case SchedulerDone:
		result.Status = process.StepDone
		if len(sr.Results) > 0 {
			p.result = sr.Results[0]
			result.Result = payload.New(p.result)
		}
		return result, nil

	case SchedulerContinue:
		result.Status = process.StepContinue
		result.AddYield(sr.Command)
		return result, nil
	}

	result.Status = process.StepDone
	return result, nil
}

// Send delivers external messages to the process.
func (p *Process) Send(_ *relay.Package) error {
	return fmt.Errorf("WASM processes do not support external messages")
}

// Close releases process resources.
func (p *Process) Close() {
	if p.instance != nil {
		p.instance.Close(context.Background())
		p.instance = nil
	}
	p.ctx = nil
}

// Result returns the result of the function call.
func (p *Process) Result() any {
	return p.result
}

// compile-time check
var _ process.Process = (*Process)(nil)
