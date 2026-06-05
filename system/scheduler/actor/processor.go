// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/attrs"
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
	ctx        context.Context
	Process    process.Process
	stats      atomic.Pointer[attrs.Bag]
	resultCh   chan *runtime.Result
	scheduler  *Scheduler
	cancel     context.CancelFunc
	queue      *process.EventQueue
	pid        pid.PID
	output     process.StepOutput
	gen        atomic.Uint64
	steps      atomic.Uint64
	id         uint64
	startedAt  int64
	state      atomic.Int32
	lastWorker atomic.Int32
	pooled     bool
}

// casState atomically transitions the masked state from old to newState,
// preserving the wakeup flag. It returns false only when the masked state no
// longer equals old, i.e. another transition won the race. A CAS failure caused
// solely by a concurrent setWakeup flipping the wakeup bit (while the masked
// state still equals old) is retried: dropping the transition there would
// strand the processor in Running|wakeup with a queued event and never re-queue
// it. The retry is reachable only when old is StateRunning (the single masked
// state setWakeup writes against), so every other caller stays single-shot, and
// it is bounded because setWakeup is idempotent.
func (p *Processor) casState(old, newState ProcessState) bool {
	for {
		current := ProcessState(p.state.Load())
		if current&stateMask != old {
			return false
		}
		newWithFlags := (newState & stateMask) | (current & wakeupFlag)
		if p.state.CompareAndSwap(int32(current), int32(newWithFlags)) {
			return true
		}
	}
}

// setWakeup atomically sets the wakeup flag if state matches expected.
func (p *Processor) setWakeup(expectedState ProcessState) {
	for {
		current := ProcessState(p.state.Load())
		if current&stateMask != expectedState {
			return
		}
		newVal := current | wakeupFlag
		if p.state.CompareAndSwap(int32(current), int32(newVal)) {
			return
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
			sched.injectOrGlobal(p)
		}
		return
	}

	// Failed to transition - processor might be Running.
	// Try to set wakeup flag atomically. If state changed, that's fine -
	// either worker already moved on or another CompleteYield already woke it.
	p.setWakeup(StateRunning)
}

// StateName returns a human-readable name for the process state.
func StateName(s ProcessState) string {
	switch s & stateMask {
	case StateReady:
		return "ready"
	case StateRunning:
		return "running"
	case StateBlocked:
		return "blocked"
	case StateIdle:
		return "idle"
	case StateComplete:
		return "complete"
	default:
		return "unknown"
	}
}

const noWorkerAffinity = -1

var processorPool = sync.Pool{
	New: func() any {
		p := &Processor{
			queue: process.NewEventQueue(),
		}
		p.lastWorker.Store(noWorkerAffinity)
		return p
	},
}

func acquireProcessor() *Processor {
	return processorPool.Get().(*Processor)
}

func releaseProcessor(p *Processor) {
	p.id = 0
	p.pid = pid.PID{}
	p.startedAt = 0
	// Never leave pooled processors in Ready state; stale queue references must
	// fail Ready->Running CAS and be ignored.
	p.state.Store(int32(StateComplete))
	p.Process = nil
	p.ctx = nil
	p.cancel = nil
	p.scheduler = nil
	p.lastWorker.Store(noWorkerAffinity)
	p.resultCh = nil
	p.pooled = false
	p.steps.Store(0)
	p.stats.Store(nil)
	p.output.Reset()
	processorPool.Put(p)
}
