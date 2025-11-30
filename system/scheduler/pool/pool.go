// Package funcpool provides function pool scheduler interfaces and implementations.
//
// A function pool manages a set of reusable processes that execute function calls.
// Different pool implementations optimize for different workload patterns:
//
//   - Inline: Synchronous execution in caller's goroutine, for eval/embedded actors
//   - Static: Fixed-size channel-based pool, optimized for steady high load
//   - Elastic: Grows/shrinks based on demand, for spiking workloads
//   - WorkStealing: Work-stealing scheduler, for long-running tasks with varying times
package pool

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process2"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

// Pool executes function calls using managed processes.
// Implementations must be safe for concurrent use.
type Pool interface {
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

// Factory creates new Process instances.
type Factory = process2.ProcessFactory

// Dispatcher routes commands to handlers.
type Dispatcher = dispatcher.Dispatcher

// OnStart is called when a pool process is created.
// Use this for resource initialization (DB connections, etc.).
type OnStart func(proc process2.Process)

// OnStop is called when a pool process is destroyed.
// Use this for resource cleanup.
type OnStop func(proc process2.Process)

// Hooks contains lifecycle callbacks for pool processes.
type Hooks struct {
	OnStart OnStart
	OnStop  OnStop
}

// WrapFactoryWithHooks wraps a factory to call lifecycle hooks.
// OnStart is called after process creation, OnStop before Close.
func WrapFactoryWithHooks(factory Factory, hooks Hooks) Factory {
	if hooks.OnStart == nil && hooks.OnStop == nil {
		return factory
	}
	return func() (process2.Process, error) {
		proc, err := factory()
		if err != nil {
			return nil, err
		}
		if hooks.OnStart != nil {
			hooks.OnStart(proc)
		}
		if hooks.OnStop != nil {
			return &hookedProcess{proc: proc, onStop: hooks.OnStop}, nil
		}
		return proc, nil
	}
}

// hookedProcess wraps a process to call OnStop before Close.
type hookedProcess struct {
	proc   process2.Process
	onStop OnStop
}

func (h *hookedProcess) Execute(ctx context.Context, method string, input payload.Payloads) error {
	return h.proc.Execute(ctx, method, input)
}

func (h *hookedProcess) Step(results *process2.YieldResults) (process2.StepResult, error) {
	return h.proc.Step(results)
}

func (h *hookedProcess) Close() {
	h.onStop(h.proc)
	h.proc.Close()
}

func (h *hookedProcess) Send(pkg *relay.Package) error {
	return h.proc.Send(pkg)
}

// OnExecutionStart is called before each execution with context and process.
type OnExecutionStart func(ctx context.Context, proc process2.Process)

// OnExecutionComplete is called after each execution with context and result.
type OnExecutionComplete func(ctx context.Context, result *runtime.Result)

// ExecutionHooks contains per-execution lifecycle callbacks.
type ExecutionHooks struct {
	OnStart    OnExecutionStart
	OnComplete OnExecutionComplete
}

// Executor runs a process to completion with yield handling.
// This is the core execution logic shared across all pool types.
type Executor struct {
	dispatcher Dispatcher
	hooks      ExecutionHooks
}

// NewExecutor creates an executor with the given dispatcher.
func NewExecutor(d Dispatcher) *Executor {
	return &Executor{dispatcher: d}
}

// WithExecutionHooks sets execution-level hooks.
func (e *Executor) WithExecutionHooks(hooks ExecutionHooks) *Executor {
	e.hooks = hooks
	return e
}

// Run executes a process to completion, handling all yields.
func (e *Executor) Run(ctx context.Context, proc process2.Process, method string, input payload.Payloads) *runtime.Result {
	if e.hooks.OnStart != nil {
		e.hooks.OnStart(ctx, proc)
	}

	if err := proc.Execute(ctx, method, input); err != nil {
		result := &runtime.Result{Error: err}
		if e.hooks.OnComplete != nil {
			e.hooks.OnComplete(ctx, result)
		}
		return result
	}

	var yieldResults *process2.YieldResults
	for {
		stepResult, err := proc.Step(yieldResults)

		if yieldResults != nil {
			process2.ReleaseYieldResults(yieldResults)
			yieldResults = nil
		}

		if err != nil {
			result := &runtime.Result{Error: err}
			if e.hooks.OnComplete != nil {
				e.hooks.OnComplete(ctx, result)
			}
			return result
		}

		switch stepResult.Status {
		case process2.StepDone:
			ret := runtime.Result{Value: stepResult.Result}
			if e.hooks.OnComplete != nil {
				e.hooks.OnComplete(ctx, &ret)
			}
			return &ret

		case process2.StepIdle:
			result := &runtime.Result{Error: ErrIdleNotSupported}
			if e.hooks.OnComplete != nil {
				e.hooks.OnComplete(ctx, result)
			}
			return result

		case process2.StepContinue:
			yields := stepResult.GetYields()
			if len(yields) == 0 {
				continue
			}

			yieldResults = e.handleYields(ctx, yields)
			if yieldResults.Error != nil {
				result := &runtime.Result{Error: yieldResults.Error}
				if e.hooks.OnComplete != nil {
					e.hooks.OnComplete(ctx, result)
				}
				return result
			}
		}
	}
}

// handleYields executes all command handlers sequentially.
// For multiple yields, data is collected into a slice.
func (e *Executor) handleYields(ctx context.Context, yields []dispatcher.Command) *process2.YieldResults {
	res := process2.AcquireYieldResults()

	if len(yields) == 1 {
		cmd := yields[0]
		fmt.Printf("[DEBUG] handleYields: cmd=%T cmdID=%d\n", cmd, cmd.CmdID())

		handler := e.dispatcher.Dispatch(cmd)
		if handler == nil {
			fmt.Printf("[DEBUG] handleYields: NO HANDLER for cmdID=%d\n", cmd.CmdID())
			res.Error = &UnknownCommandError{CmdID: cmd.CmdID()}
			return res
		}

		fmt.Printf("[DEBUG] handleYields: handler=%T\n", handler)

		var emittedData any
		emit := func(data any) {
			if emittedData == nil {
				emittedData = data
			}
		}

		fmt.Printf("[DEBUG] handleYields: calling Handle...\n")
		res.Error = handler.Handle(ctx, cmd, emit)
		fmt.Printf("[DEBUG] handleYields: Handle returned, err=%v\n", res.Error)
		res.Data = emittedData
		return res
	}

	// Multiple yields - handle all sequentially, collect results
	results := make([]any, len(yields))
	for i, cmd := range yields {
		handler := e.dispatcher.Dispatch(cmd)
		if handler == nil {
			res.Error = &UnknownCommandError{CmdID: cmd.CmdID()}
			return res
		}

		var emittedData any
		emit := func(data any) {
			if emittedData == nil {
				emittedData = data
			}
		}

		if err := handler.Handle(ctx, cmd, emit); err != nil {
			res.Error = err
			return res
		}
		results[i] = emittedData
	}

	res.Data = results
	return res
}

// Errors

// ErrIdleNotSupported is returned when a process yields idle in a function pool.
var ErrIdleNotSupported = fmt.Errorf("idle yield not supported in function pool")

// UnknownCommandError indicates an unregistered command.
type UnknownCommandError struct {
	CmdID dispatcher.CommandID
}

func (e *UnknownCommandError) Error() string {
	return fmt.Sprintf("unknown command: %d", e.CmdID)
}
