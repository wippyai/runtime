// Package funcpool provides function pool scheduler interfaces and implementations.
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

// Config contains common pool configuration.
type Config struct {
	// Workers is the number of worker goroutines/processes.
	// For elastic pools, this is the initial size.
	Workers int

	// QueueSize is the capacity of the work queue.
	// Calls block when queue is full.
	QueueSize int

	// MaxWorkers is the maximum workers for elastic pools.
	// Ignored by fixed-size pools.
	MaxWorkers int

	// IdleTimeout controls when elastic pools shrink.
	// Ignored by fixed-size pools.
	IdleTimeout int
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		Workers:   4,
		QueueSize: 256,
	}
}

// OnExecutionStart is called before each execution with context and process.
type OnExecutionStart func(ctx context.Context, proc process.Process)

// OnExecutionComplete is called after each execution with context and result.
type OnExecutionComplete func(ctx context.Context, result *runtime.Result)

// ExecutionHooks contains per-execution lifecycle callbacks.
type ExecutionHooks struct {
	OnStart    OnExecutionStart
	OnComplete OnExecutionComplete
}

// Executor runs a process to completion with yield handling.
// This is the core execution logic shared across all pool types.
// Implements relay.Receiver to handle incoming messages via EventQueue.
// Implements process.ResultReceiver for zero-allocation handler completion.
type Executor struct {
	dispatcher dispatcher.Dispatcher
	hooks      ExecutionHooks

	// Event queue for yield completions and messages
	queue *process.EventQueue
	gen   atomic.Uint64 // cached generation for CompleteYield (atomic for concurrent access)

	// Step output buffer (reused across steps)
	output process.StepOutput

	// Wake signal for StepIdle - event was delivered to queue
	wake chan struct{}

	// active indicates executor is running and can receive messages
	active atomic.Bool
}

// NewExecutor creates an executor with the given dispatcher.
func NewExecutor(d dispatcher.Dispatcher) *Executor {
	e := &Executor{
		dispatcher: d,
		wake:       make(chan struct{}, 1),
		queue:      process.NewEventQueue(),
	}
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

// CompleteYield implements process.ResultReceiver.
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
// Safe to call concurrently - returns error if executor is not active.
func (e *Executor) Send(pkg *relay.Package) error {
	if !e.active.Load() {
		return process.ErrProcessNotFound
	}
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

		switch e.output.Status() {
		case process.StepDone:
			ret := runtime.Result{Value: e.output.Result()}
			if e.hooks.OnComplete != nil {
				e.hooks.OnComplete(ctx, &ret)
			}
			return &ret

		case process.StepIdle:
			// Check if events arrived during step - if so, continue immediately
			if e.queue.HasEvents() {
				continue
			}
			// Wait for wake signal or context cancellation
			select {
			case <-e.wake:
				continue
			case <-e.queue.Signal():
				continue
			case <-ctx.Done():
				result := &runtime.Result{Error: ctx.Err()}
				if e.hooks.OnComplete != nil {
					e.hooks.OnComplete(ctx, result)
				}
				return result
			}

		case process.StepContinue:
			yields := e.output.Yields()
			if len(yields) == 0 {
				// No yields means step again immediately
				continue
			}

			// Dispatch yields - pass e as ResultReceiver (zero allocation!)
			for _, y := range yields {
				handler := e.dispatcher.Dispatch(y.Cmd)
				if handler == nil {
					// Unknown command - complete with error immediately
					e.queue.PushDirect(process.Event{
						Type:  process.EventYieldComplete,
						Tag:   y.Tag,
						Error: &process.UnknownCommandError{CmdID: y.Cmd.CmdID()},
					})
					continue
				}
				if err := handler.Handle(ctx, y.Cmd, y.Tag, e); err != nil {
					// Handler returned error - complete with error
					e.queue.PushDirect(process.Event{
						Type:  process.EventYieldComplete,
						Tag:   y.Tag,
						Error: err,
					})
				}
			}

			// Check if results are already ready before blocking
			if e.queue.HasEvents() {
				continue
			}

			// Wait for first completion
			select {
			case <-e.wake:
				continue
			case <-e.queue.Signal():
				continue
			case <-ctx.Done():
				result := &runtime.Result{Error: ctx.Err()}
				if e.hooks.OnComplete != nil {
					e.hooks.OnComplete(ctx, result)
				}
				return result
			}
		}
	}
}
