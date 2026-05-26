// SPDX-License-Identifier: MPL-2.0

// Package pool provides function pool scheduler interfaces and implementations.
//
// A function pool manages a set of reusable processes that execute function calls.
// Different pool implementations optimize for different workload patterns:
//
//   - Inline: Synchronous execution in caller's goroutine, for eval/embedded actors
//   - Static: Fixed-size channel-based pool, optimized for steady high load
//   - Lazy: Zero processes at idle, creates on demand, ideal for low-traffic functions
package pool

import (
	"context"
	"sync/atomic"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	sysprocess "github.com/wippyai/runtime/system/process"
)

// Pool executes function calls using managed processes.
// Implementations must be safe for concurrent use.
// Also implements relay.Receiver for message routing.
type Pool interface {
	relay.Receiver

	// Call executes a function and blocks until completion.
	Call(ctx context.Context, method string, input payload.Payloads) (*runtime.Result, error)

	// Start initializes the pool and begins accepting calls.
	Start()

	// Stop gracefully shuts down the pool.
	// Waits for in-flight calls to complete.
	Stop()
}

// OnExecutionStart is a callback called before each execution with context and process.
type OnExecutionStart func(ctx context.Context, proc process.Process)

// OnExecutionComplete is a callback called after each execution with context and result.
type OnExecutionComplete func(ctx context.Context, result *runtime.Result)

// ExecutionHooks contains per-execution lifecycle callbacks.
type ExecutionHooks struct {
	OnStart    OnExecutionStart
	OnComplete OnExecutionComplete
}

// Request holds a pending function call for queue-based pools.
type Request struct {
	Ctx      context.Context
	ResultCh chan *runtime.Result
	Method   string
	Input    payload.Payloads
}

// Executor runs a process to completion with yield handling.
// This is the core execution logic shared across all pool types.
// Implements relay.Receiver to handle incoming messages via EventQueue.
// Implements dispatcher.ResultReceiver for zero-allocation handler completion.
type Executor struct {
	dispatcher dispatcher.Dispatcher
	hooks      ExecutionHooks
	queue      *process.EventQueue
	wake       chan struct{}
	output     process.StepOutput
	gen        atomic.Uint64
	active     atomic.Bool
}

// NewExecutor creates an executor with the given dispatcher.
func NewExecutor(d dispatcher.Dispatcher) *Executor {
	queue := process.NewEventQueue()
	e := &Executor{
		dispatcher: d,
		wake:       make(chan struct{}, 1),
		queue:      queue,
	}
	// Initialize gen to match queue generation so Send works immediately
	e.gen.Store(queue.Generation())
	return e
}

// Reset prepares the executor for reuse.
func (e *Executor) Reset() {
	e.active.Store(false)
	// Drain wake channel
	select {
	case <-e.wake:
	default:
	}
	// Reset queue for next execution
	e.queue.Reset()
	e.gen.Store(e.queue.Generation())
}

// CompleteYield implements dispatcher.ResultReceiver.
// Called by handlers to deliver yield completion.
// Thread-safe: can be called from any goroutine.
func (e *Executor) CompleteYield(tag uint64, data any, err error) {
	if e.queue.Push(process.Event{
		Type:  process.EventYieldComplete,
		Tag:   tag,
		Data:  data,
		Error: err,
	}, e.gen.Load()) {
		// Signal wake
		select {
		case e.wake <- struct{}{}:
		default:
		}
	}
}

// Send implements relay.Receiver. Delivers message via EventQueue.
// Safe to call concurrently. Messages can be queued before Run() starts.
func (e *Executor) Send(pkg *relay.Package) error {
	// Push message event to queue with generation check
	if !e.queue.Push(process.Event{
		Type: process.EventMessage,
		Data: pkg,
	}, e.gen.Load()) {
		return process.ErrProcessNotFound
	}
	// Signal wake
	select {
	case e.wake <- struct{}{}:
	default:
	}
	return nil
}

// WithExecutionHooks sets execution-level hooks.
func (e *Executor) WithExecutionHooks(hooks ExecutionHooks) *Executor {
	e.hooks = hooks
	return e
}

// Run executes a process to completion, handling all yields.
// aborter is implemented by processes (e.g. engine.Process) that can drain
// their ephemeral channel producers when execution is cancelled before the
// normal Close/clearExecution teardown runs.
type aborter interface{ Abort() }

// cancelResult drains the process's ephemeral producers (if it supports it)
// and builds the cancellation result, firing the OnComplete hook. Used on
// every ctx.Done() early-exit so a cancelled process doesn't leak ephemerals.
func (e *Executor) cancelResult(ctx context.Context, proc process.Process) *runtime.Result {
	if a, ok := proc.(aborter); ok {
		a.Abort()
	}
	result := &runtime.Result{Error: ctx.Err()}
	if e.hooks.OnComplete != nil {
		e.hooks.OnComplete(ctx, result)
	}
	return result
}

func (e *Executor) Run(ctx context.Context, proc process.Process, method string, input payload.Payloads) *runtime.Result {
	if e.hooks.OnStart != nil {
		e.hooks.OnStart(ctx, proc)
	}

	// Reset queue for this execution
	e.queue.Reset()
	e.gen.Store(e.queue.Generation())

	// Enable Send routing
	e.active.Store(true)

	// Ensure active flag cleared and queue closed when Run returns
	defer func() {
		e.active.Store(false)
		e.queue.Close()
	}()

	if err := proc.Init(ctx, method, input); err != nil {
		result := &runtime.Result{Error: err}
		if e.hooks.OnComplete != nil {
			e.hooks.OnComplete(ctx, result)
		}
		return result
	}

	for {
		// Drain events from queue
		events := e.queue.Drain()

		// Reset output for this step
		e.output.Reset()

		// Step the process
		if err := proc.Step(events, &e.output); err != nil {
			result := &runtime.Result{Error: err}
			if e.hooks.OnComplete != nil {
				e.hooks.OnComplete(ctx, result)
			}
			return result
		}

		status := e.output.Status()

		// Handle Done first - no yields to dispatch
		if status == process.StepDone {
			ret := runtime.Result{Value: e.output.Result()}
			if e.hooks.OnComplete != nil {
				e.hooks.OnComplete(ctx, &ret)
			}
			return &ret
		}

		// Dispatch any yields (for all statuses except Done)
		yields := e.output.Yields()
		for _, y := range yields {
			handler := e.dispatcher.Dispatch(y.Cmd)
			if handler == nil {
				e.queue.PushDirect(process.Event{
					Type:  process.EventYieldComplete,
					Tag:   y.Tag,
					Error: sysprocess.NewUnknownCommandError(y.Cmd.CmdID()),
				})
				continue
			}
			if err := handler.Handle(ctx, y.Cmd, y.Tag, e); err != nil {
				e.queue.PushDirect(process.Event{
					Type:  process.EventYieldComplete,
					Tag:   y.Tag,
					Error: err,
				})
			}
		}

		// Handle status after dispatching yields (StepDone handled above)
		switch status {
		case process.StepContinue:
			// Check if events arrived, otherwise step again immediately
			if e.queue.HasEvents() {
				continue
			}
			if len(yields) == 0 {
				// No yields dispatched, step again immediately
				continue
			}
			// Yields were dispatched, wait for completions
			select {
			case <-e.wake:
				continue
			case <-e.queue.Signal():
				continue
			case <-ctx.Done():
				return e.cancelResult(ctx, proc)
			}

		case process.StepYield:
			// Wait for yield completions
			if e.queue.HasEvents() {
				continue
			}
			select {
			case <-e.wake:
				continue
			case <-e.queue.Signal():
				continue
			case <-ctx.Done():
				return e.cancelResult(ctx, proc)
			}

		case process.StepIdle:
			// Wait for messages
			if e.queue.HasEvents() {
				continue
			}
			select {
			case <-e.wake:
				continue
			case <-e.queue.Signal():
				continue
			case <-ctx.Done():
				return e.cancelResult(ctx, proc)
			}

		case process.StepDone:
			// handled above before switch

		case process.StepUpgrade:
			// upgrade not supported in pool scheduler
		}
	}
}
