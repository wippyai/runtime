package actor

import (
	"context"
	"sync"
	"sync/atomic"
	_ "unsafe" // for nanotime linkname

	"github.com/wippyai/runtime/api/process"
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
	StateBlocked                      // Waiting for handler to call CompleteYield()
	StateIdle                         // Waiting for Send() (external event)
	StateComplete                     // Finished execution
)

// Processor wraps a Process with scheduler metadata.
// This is the unit that flows through queues.
//
// Separation of concerns:
//   - Process: the user's state machine (pure logic, owns its own stats)
//   - Processor: scheduler's tracking wrapper (execution state only)
//
// Lifecycle:
//  1. Acquired from pool on Submit()
//  2. Flows through global queue -> worker local deque
//  3. Executed by worker, may block waiting for handler
//  4. Released back to pool on completion
type Processor struct {
	// Identity
	id  uint64    // Internal fast routing ID (for maps, queues)
	pid relay.PID // External identity (for messages, logs, callbacks)

	Process  Process // The wrapped user process
	Priority int     // Higher = more urgent (for future use)

	// State is accessed atomically - use state()/setState()/casState()
	state atomic.Int32

	// Execution context (provided by caller, frame already set up)
	ctx context.Context

	// Event queue for yield completions (replaces old YieldResults)
	queue *process.EventQueue
	gen   atomic.Uint64 // cached generation for CompleteYield

	// Step output buffer (reused across steps)
	output StepOutput

	// Back-reference for zero-alloc completion callback
	scheduler *Scheduler

	// Result channel for blocking Execute (nil for fire-and-forget Submit)
	resultCh chan *runtime.Result

	// Pooled flag indicates this processor is managed externally (funcpool).
	// Pooled processors are NOT released or unregistered on completion.
	pooled bool
}

// State returns current processor state.
func (p *Processor) State() ProcessState {
	return ProcessState(p.state.Load())
}

// SetState sets processor state (for worker use only).
func (p *Processor) SetState(s ProcessState) {
	p.state.Store(int32(s))
}

// casState atomically compares-and-swaps state. Returns true if swap succeeded.
func (p *Processor) casState(old, new ProcessState) bool {
	return p.state.CompareAndSwap(int32(old), int32(new))
}

// CompleteYield implements process.ResultReceiver.
// Called by handlers to deliver yield completion.
// Thread-safe: can be called from any goroutine.
func (p *Processor) CompleteYield(tag uint64, data any, err error) {
	if !p.queue.Push(process.Event{
		Type:  process.EventYieldComplete,
		Tag:   tag,
		Data:  data,
		Error: err,
	}, p.gen.Load()) {
		return
	}

	// Only re-schedule if transitioning from Blocked to Ready.
	// This prevents double-queueing when both handler and worker try to push.
	if p.casState(StateBlocked, StateReady) {
		sched := p.scheduler
		if sched != nil {
			sched.global.Push(p)
			sched.wake()
		}
	}
}

// ID returns the internal processor ID.
func (p *Processor) ID() uint64 {
	return p.id
}

// PID returns the external process ID.
func (p *Processor) PID() relay.PID {
	return p.pid
}

// Context returns the processor's context for cancellation checking.
func (p *Processor) Context() context.Context {
	return p.ctx
}

// Queue returns the event queue for message sender creation.
func (p *Processor) Queue() *process.EventQueue {
	return p.queue
}

// Pool for processor reuse to reduce allocations.
var processorPool = sync.Pool{
	New: func() any {
		return &Processor{
			queue: process.NewEventQueue(),
		}
	},
}

// acquireProcessor gets a processor from the pool.
func acquireProcessor() *Processor {
	return processorPool.Get().(*Processor)
}

// releaseProcessor returns a processor to the pool after clearing all fields.
func releaseProcessor(p *Processor) {
	p.id = 0
	p.pid = relay.PID{}
	p.Process = nil
	p.state.Store(0)
	p.Priority = 0
	p.ctx = nil
	p.gen.Store(0)
	p.output.Reset()
	p.scheduler = nil
	p.resultCh = nil
	p.pooled = false
	// Queue is reset on next use

	processorPool.Put(p)
}
