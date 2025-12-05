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

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

// MaxPoolYields is the maximum yields that fit in embedded slots.
const MaxPoolYields = 4

// yieldSlot stores result for one yield.
type yieldSlot struct {
	Data  any
	Error error
}

// poolCompleter implements dispatcher.Completer for pool multi-yield.
type poolCompleter struct {
	ctx *multiYieldCtx
	idx int
}

func (e *poolCompleter) Complete(data any, err error) {
	e.ctx.slots[e.idx].Data = data
	e.ctx.slots[e.idx].Error = err
	if e.ctx.pending.Add(-1) == 0 {
		select {
		case e.ctx.wakeup <- struct{}{}:
		default:
		}
	}
}

// multiYieldCtx supports zero-allocation multi-yield completion.
type multiYieldCtx struct {
	slots      [MaxPoolYields]yieldSlot
	completers [MaxPoolYields]poolCompleter
	handlers   [MaxPoolYields]dispatcher.Handler
	pending    atomic.Int32
	wakeup     chan struct{}
}

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

// Factory creates new Process instances.
type Factory = process.NewFunc

// Dispatcher routes commands to handlers.
type Dispatcher = dispatcher.Dispatcher

// OnStart is called when a pool process is created.
// Use this for resource initialization (DB connections, etc.).
type OnStart func(proc process.Process)

// OnStop is called when a pool process is destroyed.
// Use this for resource cleanup.
type OnStop func(proc process.Process)

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
	return func() (process.Process, error) {
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
	proc   process.Process
	onStop OnStop
}

func (h *hookedProcess) Init(ctx context.Context, method string, input payload.Payloads) error {
	return h.proc.Init(ctx, method, input)
}

func (h *hookedProcess) Step(results *process.YieldResults) (process.StepResult, error) {
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
// Implements relay.Receiver to handle incoming messages during StepIdle.
type Executor struct {
	dispatcher Dispatcher
	hooks      ExecutionHooks
	multiCtx   multiYieldCtx // embedded for zero-alloc multi-yield

	// Wake signal for StepIdle - message was delivered to process
	wake chan struct{}

	// Process for this executor. Stored atomically for Lazy pool reuse.
	proc atomic.Pointer[process.Process]

	// Direct reference to ProcessContext for message delivery.
	// Set after Init, cleared when execution ends.
	// Send() writes directly here, bypassing Process.Send().
	inbox atomic.Pointer[engine.ProcessContext]

	// hasWork is set when messages arrive, checked/cleared at StepIdle
	hasWork atomic.Bool
}

// NewExecutor creates an executor with the given dispatcher.
func NewExecutor(d Dispatcher) *Executor {
	e := &Executor{
		dispatcher: d,
		wake:       make(chan struct{}, 1),
	}
	e.multiCtx.wakeup = make(chan struct{}, 1)
	return e
}

// Reset prepares the executor for reuse. Clears inbox and proc atomically,
// hasWork flag, and drains channels.
func (e *Executor) Reset() {
	e.inbox.Store(nil)
	e.proc.Store(nil)
	e.hasWork.Store(false)
	// Drain wake channel
	select {
	case <-e.wake:
	default:
	}
	// Drain multiCtx wakeup
	select {
	case <-e.multiCtx.wakeup:
	default:
	}
}

// Send implements relay.Receiver. Delivers message directly to inbox.
// Safe to call concurrently - returns error if inbox was cleared.
func (e *Executor) Send(pkg *relay.Package) error {
	inbox := e.inbox.Load()
	if inbox == nil {
		return ErrProcessNotFound
	}
	if !inbox.QueueMessage(pkg) {
		return ErrProcessNotFound
	}
	// Mark that work arrived and signal wake
	e.hasWork.Store(true)
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
// Captures inbox after Init and clears it before returning.
func (e *Executor) Run(ctx context.Context, proc process.Process, method string, input payload.Payloads) *runtime.Result {
	if e.hooks.OnStart != nil {
		e.hooks.OnStart(ctx, proc)
	}

	if err := proc.Init(ctx, method, input); err != nil {
		result := &runtime.Result{Error: err}
		if e.hooks.OnComplete != nil {
			e.hooks.OnComplete(ctx, result)
		}
		return result
	}

	// Capture inbox reference after Init - ProcessContext is now in the frame context
	pc := engine.GetProcessContext(ctx)
	if pc != nil {
		e.inbox.Store(pc)
	}

	// Ensure inbox is cleared when Run returns - prevents messages after completion
	defer e.inbox.Store(nil)

	var yieldResults *process.YieldResults
	for {
		stepResult, err := proc.Step(yieldResults)

		if yieldResults != nil {
			process.ReleaseYieldResults(yieldResults)
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
		case process.StepDone:
			ret := runtime.Result{Value: stepResult.Result}
			if e.hooks.OnComplete != nil {
				e.hooks.OnComplete(ctx, &ret)
			}
			return &ret

		case process.StepIdle:
			// Check if messages arrived during step - if so, continue immediately
			if e.hasWork.Swap(false) {
				continue
			}
			// Wait for wake signal or context cancellation
			select {
			case <-e.wake:
				continue
			case <-ctx.Done():
				result := &runtime.Result{Error: ctx.Err()}
				if e.hooks.OnComplete != nil {
					e.hooks.OnComplete(ctx, result)
				}
				return result
			}

		case process.StepContinue:
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

// handleYields executes all command handlers and waits for complete to be called.
// Handlers are async - they call complete.Complete() when done, possibly from another goroutine.
func (e *Executor) handleYields(ctx context.Context, yields []dispatcher.Command) *process.YieldResults {
	res := process.AcquireYieldResults()

	if len(yields) == 1 {
		cmd := yields[0]
		handler := e.dispatcher.Dispatch(cmd)
		if handler == nil {
			res.Error = &UnknownCommandError{CmdID: cmd.CmdID()}
			return res
		}

		// Single yield - use embedded emitter for zero allocation
		e.multiCtx.pending.Store(1)
		e.multiCtx.slots[0].Data = nil
		e.multiCtx.slots[0].Error = nil
		e.multiCtx.completers[0].ctx = &e.multiCtx
		e.multiCtx.completers[0].idx = 0

		if err := handler.Handle(ctx, cmd, &e.multiCtx.completers[0]); err != nil {
			res.Error = err
			return res
		}

		select {
		case <-e.multiCtx.wakeup:
			res.Data = e.multiCtx.slots[0].Data
			res.Error = e.multiCtx.slots[0].Error
		case <-ctx.Done():
			res.Error = ctx.Err()
		}
		return res
	}

	// Multiple yields - validate handlers first using embedded array
	n := len(yields)
	for i, cmd := range yields {
		e.multiCtx.handlers[i] = e.dispatcher.Dispatch(cmd)
		if e.multiCtx.handlers[i] == nil {
			res.Error = &UnknownCommandError{CmdID: cmd.CmdID()}
			return res
		}
	}

	// Initialize multi-yield context
	e.multiCtx.pending.Store(int32(n)) //nolint:gosec // n is bounded by MaxPoolYields (4)
	for i := 0; i < n && i < MaxPoolYields; i++ {
		e.multiCtx.slots[i].Data = nil
		e.multiCtx.slots[i].Error = nil
		e.multiCtx.completers[i].ctx = &e.multiCtx
		e.multiCtx.completers[i].idx = i
	}

	// Start all handlers in parallel using embedded completers
	for i, cmd := range yields {
		completer := &e.multiCtx.completers[i]
		if err := e.multiCtx.handlers[i].Handle(ctx, cmd, completer); err != nil {
			completer.Complete(nil, err)
		}
	}

	// Wait for all to complete
	select {
	case <-e.multiCtx.wakeup:
	case <-ctx.Done():
		res.Error = ctx.Err()
		return res
	}

	// Check for errors, collect results
	results := make([]any, n)
	for i := 0; i < n; i++ {
		if e.multiCtx.slots[i].Error != nil {
			res.Error = e.multiCtx.slots[i].Error
			return res
		}
		results[i] = e.multiCtx.slots[i].Data
	}

	res.Data = results
	return res
}
