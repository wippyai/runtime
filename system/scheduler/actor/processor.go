package actor

import (
	"context"
	"sync"
	"sync/atomic"
	_ "unsafe" // for nanotime linkname

	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

// nanotime returns monotonic time in nanoseconds.
// Uses runtime.nanotime which is faster than time.Now().UnixNano()
// because it skips wall clock calculation.
//
//go:linkname nanotime runtime.nanotime
func nanotime() int64

// ProcessState tracks a processor through the scheduler lifecycle.
type ProcessState int32

const (
	StateReady    ProcessState = iota // In run queue, waiting to execute
	StateRunning                      // Currently executing on a worker
	StateBlocked                      // Waiting for handler to call Complete()
	StateIdle                         // Waiting for Send() (external event)
	StateComplete                     // Finished execution
)

// Processor wraps a Process with scheduler metadata.
// This is the unit that flows through queues.
//
// Separation of concerns:
//   - Process: the user's state machine (pure logic)
//   - Processor: scheduler's tracking wrapper (execution state)
//
// Lifecycle:
//  1. Acquired from pool on Submit()
//  2. Flows through global queue -> worker local deque
//  3. Executed by worker, may block waiting for handler
//  4. Released back to pool on completion
//
// Note: Scheduler owns full process lifecycle. Context cancellation
// is caller's responsibility (frame context set up before Submit).
type Processor struct {
	// Identity
	ID  uint64    // Internal fast routing ID (for maps, queues)
	PID relay.PID // External identity (for messages, logs, callbacks)

	Process  Process      // The wrapped user process
	State    ProcessState // Current scheduler state
	Priority int          // Higher = more urgent (for future use)

	// Execution context (provided by caller, frame already set up)
	ctx context.Context

	// Yield results storage (embedded to avoid allocation)
	// Handler calls Complete() which stores here, next Step() consumes
	yieldResult    YieldResults
	hasYieldResult bool

	// Executing worker ID for sync handler detection (-1 = not executing)
	// Set by worker before Handle(), cleared after
	// Complete() uses this to determine sync vs async re-queue
	executingWorker int32

	// Back-reference for zero-alloc Complete() callback
	scheduler *Scheduler

	// Result channel for blocking Execute (nil for fire-and-forget Submit)
	resultCh chan *runtime.Result

	// Timing (monotonic nanoseconds for minimal overhead)
	WakeNano int64 // Last time processor was queued for execution

	// Metrics
	StepCount uint64 // Number of Step() calls

	// Pooled flag indicates this processor is managed externally (funcpool).
	// Pooled processors are NOT released or unregistered on completion.
	pooled bool
}

// Complete is called by handlers when async work finishes.
// Stores the result and re-queues the processor for execution.
//
// Thread-safe: can be called from any goroutine.
// Must be called exactly once per blocked yield.
func (p *Processor) Complete(data any, err error) {
	// Capture scheduler reference first - may be nil if processor was released
	sched := p.scheduler
	if sched == nil {
		return
	}

	p.yieldResult.Data = data
	p.yieldResult.Error = err
	p.hasYieldResult = true
	p.State = StateReady

	// Check if sync handler (executingWorker > 0 means worker is still in Handle())
	workerID := atomic.LoadInt32(&p.executingWorker)
	if workerID > 0 && atomic.CompareAndSwapInt32(&p.executingWorker, workerID, -1) {
		// Sync handler - worker will re-queue locally after Handle() returns
		return
	}

	// Async handler or worker already moved on - push to global queue and wake worker
	sched.global.Push(p)
	sched.wakeWorker()
}

// Context returns the processor's context for cancellation checking.
func (p *Processor) Context() context.Context {
	return p.ctx
}

// Pool for processor reuse to reduce allocations.
var processorPool = sync.Pool{
	New: func() any { return &Processor{} },
}

// acquireProcessor gets a processor from the pool.
func acquireProcessor() *Processor {
	return processorPool.Get().(*Processor)
}

// releaseProcessor returns a processor to the pool after clearing all fields.
// Critical: must clear all references to avoid memory leaks.
func releaseProcessor(p *Processor) {
	// Clear all fields
	p.ID = 0
	p.PID = relay.PID{}
	p.Process = nil
	p.State = 0
	p.Priority = 0
	p.ctx = nil
	p.yieldResult.Data = nil
	p.yieldResult.Error = nil
	p.hasYieldResult = false
	p.executingWorker = 0
	p.scheduler = nil
	p.resultCh = nil
	p.WakeNano = 0
	p.StepCount = 0
	p.pooled = false

	processorPool.Put(p)
}
