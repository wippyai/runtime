package actor

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/runtime"
)

// ProcessState tracks a processor through the scheduler lifecycle.
// Lower 4 bits store the state, bit 4 is the wakeup flag.
// Combined into single atomic for race-free transitions.
type ProcessState int32

const (
	StateReady    ProcessState = iota // In run queue, waiting to execute
	StateRunning                      // Currently executing on a worker
	StateBlocked                      // Waiting for handler to call CompleteYield()
	StateIdle                         // Waiting for Send() (external event)
	StateComplete                     // Finished execution

	stateMask  ProcessState = 0x0F // Lower 4 bits for state
	wakeupFlag ProcessState = 0x10 // Bit 4: wakeup pending while Running
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
//
// Thread safety guarantee:
//
//	A processor is NEVER executed by two workers simultaneously.
//	The state machine ensures single-owner semantics:
//	- StateRunning: worker owns processor, CompleteYield sets wakeup flag instead of re-queueing
//	- StateBlocked: no owner, CompleteYield can CAS to Ready and re-queue
//	- Worker uses CAS loop to atomically check wakeup and transition state
type Processor struct {
	// Identity
	id  uint64  // Internal fast routing ID (for maps, queues)
	pid pid.PID // External identity (for messages, logs, callbacks)

	Process Process // The wrapped user process

	// State machine for single-owner guarantee.
	// Lower 4 bits = state, bit 4 = wakeup flag.
	// Combined into single atomic for race-free transitions.
	// Only the owning worker can transition from Running.
	// CompleteYield can only transition from Blocked or set wakeup on Running.
	state atomic.Int32

	// Execution context with cancellation support
	ctx    context.Context
	cancel context.CancelFunc

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

// casState atomically compares-and-swaps state (ignoring wakeup flag).
// Returns true if swap succeeded.
// NOTE: This does NOT retry on CAS failure - if state doesn't match, returns false.
// This prevents race where processor finishes quickly and gets re-queued before
// a competing worker's CAS retry sees the new Ready state.
func (p *Processor) casState(old, newState ProcessState) bool {
	current := ProcessState(p.state.Load())
	if current&stateMask != old {
		return false
	}
	// Preserve wakeup flag in new state
	newWithFlags := (newState & stateMask) | (current & wakeupFlag)
	return p.state.CompareAndSwap(int32(current), int32(newWithFlags))
}

// setWakeup atomically sets the wakeup flag if state matches expected.
// Returns true if wakeup was set (state matched).
func (p *Processor) setWakeup(expectedState ProcessState) bool {
	for {
		current := ProcessState(p.state.Load())
		if current&stateMask != expectedState {
			return false
		}
		newVal := current | wakeupFlag
		if p.state.CompareAndSwap(int32(current), int32(newVal)) {
			return true
		}
	}
}

// finishDispatch atomically clears wakeup flag and sets final state.
// If wakeup was set, transitions to Ready and returns true (caller should re-queue).
// If no wakeup, transitions to Blocked and returns false.
// For worker use only - must be called when state is Running.
func (p *Processor) finishDispatch() bool {
	for {
		current := ProcessState(p.state.Load())
		hadWakeup := current&wakeupFlag != 0
		var newState ProcessState
		if hadWakeup {
			newState = StateReady
		} else {
			newState = StateBlocked
		}
		if p.state.CompareAndSwap(int32(current), int32(newState)) {
			return hadWakeup
		}
	}
}

// CompleteYield implements dispatcher.ResultReceiver.
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

	// Try to transition Blocked→Ready and re-queue.
	// If processor is Running (worker still owns it), set wakeup flag atomically.
	// Worker will check wakeup after dispatch and re-queue if set.
	if p.casState(StateBlocked, StateReady) {
		sched := p.scheduler
		if sched != nil {
			sched.global.Push(p)
			sched.wake()
		}
		return
	}

	// Failed to transition - processor might be Running.
	// Try to set wakeup flag atomically. If state changed, that's fine -
	// either worker already moved on or another CompleteYield already woke it.
	p.setWakeup(StateRunning)
}

var processorPool = sync.Pool{
	New: func() any {
		return &Processor{
			queue: process.NewEventQueue(),
		}
	},
}

func acquireProcessor() *Processor {
	return processorPool.Get().(*Processor)
}

func releaseProcessor(p *Processor) {
	p.id = 0
	p.pid = pid.PID{}
	p.Process = nil
	p.state.Store(0)
	p.ctx = nil
	p.cancel = nil
	p.scheduler = nil
	p.resultCh = nil
	p.pooled = false
	p.output.Reset()
	processorPool.Put(p)
}
