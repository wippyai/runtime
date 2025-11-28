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

// Executor runs a process to completion with yield handling.
// This is the core execution logic shared across all pool types.
type Executor struct {
	dispatcher Dispatcher
}

// NewExecutor creates an executor with the given dispatcher.
func NewExecutor(d Dispatcher) *Executor {
	return &Executor{dispatcher: d}
}

// Run executes a process to completion, handling all yields.
func (e *Executor) Run(ctx context.Context, proc process2.Process, method string, input payload.Payloads) *runtime.Result {
	if err := proc.Execute(ctx, method, input); err != nil {
		return &runtime.Result{Error: err}
	}

	var yieldResults *process2.YieldResults
	for {
		result, err := proc.Step(yieldResults)

		if yieldResults != nil {
			process2.ReleaseYieldResults(yieldResults)
			yieldResults = nil
		}

		if err != nil {
			return &runtime.Result{Error: err}
		}

		switch result.Status {
		case process2.StepDone:
			var ret runtime.Result
			yields := result.GetYields()
			if len(yields) > 0 {
				if p, ok := yields[0].(payload.Payload); ok {
					ret.Value = p
				}
			}
			return &ret

		case process2.StepIdle:
			return &runtime.Result{Error: ErrIdleNotSupported}

		case process2.StepContinue:
			yields := result.GetYields()
			if len(yields) == 0 {
				continue
			}

			cmd := yields[0]
			handler := e.dispatcher.Dispatch(cmd)
			if handler == nil {
				return &runtime.Result{Error: &UnknownCommandError{CmdID: cmd.CmdID()}}
			}

			yieldResults = e.handleYield(ctx, handler, cmd)
		}
	}
}

// handleYield executes a command handler.
func (e *Executor) handleYield(ctx context.Context, handler dispatcher.Handler, cmd dispatcher.Command) *process2.YieldResults {
	res := process2.AcquireYieldResults()

	var emittedData any
	emit := func(data any) {
		if emittedData == nil {
			emittedData = data
		}
	}

	err := handler.Handle(ctx, cmd, emit)
	res.Data = emittedData
	res.Error = err
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
