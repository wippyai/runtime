package actor

import (
	"context"
	"sync"
	"sync/atomic"
	_ "unsafe" // for nanotime linkname

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

// YieldSlot stores result for one yield in multi-yield execution.
type YieldSlot struct {
	Data  any
	Error error
}

// IndexedEmitter implements dispatcher.Emitter for multi-yield.
// Embedded in Processor to avoid allocation.
type IndexedEmitter struct {
	proc *Processor
	idx  int
}

// Emit implements dispatcher.Emitter.
func (e *IndexedEmitter) Emit(data any, err error) {
	e.proc.CompleteAt(e.idx, data, err)
}

// MultiYieldContext supports zero-allocation multi-yield completion.
// Embedded in Processor to avoid allocation per multi-yield call.
type MultiYieldContext struct {
	slots         [MaxYields]YieldSlot
	emitters      [MaxYields]IndexedEmitter
	overflowSlots []YieldSlot      // for yields > MaxYields
	overflowEmit  []IndexedEmitter // for yields > MaxYields
	pending       atomic.Int32
	wakeup        chan struct{}
}

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

	// Multi-yield context (embedded to avoid allocation)
	multiYield MultiYieldContext

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

// Emit implements dispatcher.Emitter for single-yield path.
// Stores the result and re-queues the processor for execution.
//
// Thread-safe: can be called from any goroutine.
// Must be called exactly once per blocked yield.
func (p *Processor) Emit(data any, err error) {
	sched := p.scheduler
	if sched == nil {
		return
	}

	p.yieldResult.Data = data
	p.yieldResult.Error = err
	p.hasYieldResult = true
	p.State = StateReady
	p.WakeNano = nanotime()

	sched.global.Push(p)
	sched.wakeWorker()
}

// Complete is an alias for Emit (backwards compatibility).
func (p *Processor) Complete(data any, err error) {
	p.Emit(data, err)
}

// Context returns the processor's context for cancellation checking.
func (p *Processor) Context() context.Context {
	return p.ctx
}

// CompleteAt is called by handlers for multi-yield completion.
// Stores result at index and signals when all yields complete.
// Thread-safe: can be called from any goroutine.
func (p *Processor) CompleteAt(idx int, data any, err error) {
	slot := p.getSlot(idx)
	slot.Data = data
	slot.Error = err
	if p.multiYield.pending.Add(-1) == 0 {
		select {
		case p.multiYield.wakeup <- struct{}{}:
		default:
		}
	}
}

// initMultiYield prepares the multi-yield context for n yields.
func (p *Processor) initMultiYield(n int) {
	p.multiYield.pending.Store(int32(n))
	if p.multiYield.wakeup == nil {
		p.multiYield.wakeup = make(chan struct{}, 1)
	}

	// Initialize embedded emitters (common case: n <= MaxYields)
	for i := 0; i < n && i < MaxYields; i++ {
		p.multiYield.slots[i].Data = nil
		p.multiYield.slots[i].Error = nil
		p.multiYield.emitters[i].proc = p
		p.multiYield.emitters[i].idx = i
	}

	// Handle overflow beyond MaxYields (rare case)
	if n > MaxYields {
		overflow := n - MaxYields
		if cap(p.multiYield.overflowSlots) < overflow {
			p.multiYield.overflowSlots = make([]YieldSlot, overflow)
			p.multiYield.overflowEmit = make([]IndexedEmitter, overflow)
		} else {
			p.multiYield.overflowSlots = p.multiYield.overflowSlots[:overflow]
			p.multiYield.overflowEmit = p.multiYield.overflowEmit[:overflow]
		}
		for i := 0; i < overflow; i++ {
			p.multiYield.overflowSlots[i].Data = nil
			p.multiYield.overflowSlots[i].Error = nil
			p.multiYield.overflowEmit[i].proc = p
			p.multiYield.overflowEmit[i].idx = MaxYields + i
		}
	}
}

// getEmitter returns the emitter for the given yield index.
func (p *Processor) getEmitter(idx int) dispatcher.Emitter {
	if idx < MaxYields {
		return &p.multiYield.emitters[idx]
	}
	return &p.multiYield.overflowEmit[idx-MaxYields]
}

// getSlot returns the result slot for the given yield index.
func (p *Processor) getSlot(idx int) *YieldSlot {
	if idx < MaxYields {
		return &p.multiYield.slots[idx]
	}
	return &p.multiYield.overflowSlots[idx-MaxYields]
}

// waitMultiYield blocks until all yields complete or context cancels.
func (p *Processor) waitMultiYield(ctx context.Context) error {
	select {
	case <-p.multiYield.wakeup:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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
	// Keep multiYield.wakeup channel for reuse (don't nil it)
	for i := range p.multiYield.slots {
		p.multiYield.slots[i].Data = nil
		p.multiYield.slots[i].Error = nil
	}
	p.scheduler = nil
	p.resultCh = nil
	p.WakeNano = 0
	p.StepCount = 0
	p.pooled = false

	processorPool.Put(p)
}
