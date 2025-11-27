package scheduler

import (
	"math/rand"
	"runtime"
	"sync/atomic"
	"time"
	_ "unsafe"
)

// Worker is a goroutine that processes Processors from queues.
//
// Work finding priority:
//  1. LIFO slot (hot task from message passing)
//  2. Local deque (Pop) - fast, cache-friendly, no contention
//  3. Global queue (checked every globalCheckInterval) - new/returning work
//  4. Steal from random victim - load balancing
//
// Each worker owns its local deque exclusively for Push/Pop.
// Other workers can only Steal from it.
type Worker struct {
	id        int
	local     *Deque // Local work-stealing deque (owned by this worker)
	scheduler *Scheduler
	rng       *rand.Rand // Per-worker RNG to avoid lock contention

	// LIFO slot for message-passing optimization (Tokio-style)
	// Last scheduled task gets priority - hot cache locality
	lifoSlot atomic.Pointer[Processor]

	// Pre-allocated buffer for batch operations
	batchBuf [32]*Processor

	// Metrics (atomic for safe concurrent reads)
	executed atomic.Uint64 // Total processors executed
	stolen   atomic.Uint64 // Total processors stolen from others

	// Backoff state for idle workers
	spins int
}

// newWorker creates a worker with its own local deque.
func newWorker(id int, s *Scheduler) *Worker {
	return &Worker{
		id:        id,
		local:     NewDeque(s.localQueueSize),
		scheduler: s,
		rng:       rand.New(rand.NewSource(time.Now().UnixNano() + int64(id))),
	}
}

const (
	spinLimit  = 4  // Tight spins before yielding
	yieldLimit = 16 // Gosched before waiting on Cond
)

// run is the worker's main loop. Exits when scheduler.stopping is set.
// Uses Go runtime's spinning pattern for efficient parking.
func (w *Worker) run() {
	spinning := false

	for {
		proc := w.findWork()
		if proc != nil {
			// Found work - if we were spinning, stop and maybe wake another
			if spinning {
				spinning = false
				// If we were the last spinner and found work, wake another worker
				// to ensure work continues to be processed
				if w.scheduler.nspinning.Add(-1) == 0 {
					w.scheduler.wakeWorker()
				}
			}
			w.spins = 0
			w.executeOne(proc)
			w.executed.Add(1)
			continue
		}

		// No work - check if stopping
		if w.scheduler.stopping.Load() {
			if spinning {
				w.scheduler.nspinning.Add(-1)
			}
			return
		}

		// Start spinning if not already
		if !spinning {
			spinning = true
			w.scheduler.nspinning.Add(1)
		}

		// Adaptive backoff: spin -> gosched -> park
		w.spins++
		if w.spins < spinLimit {
			continue
		}
		if w.spins < yieldLimit {
			runtime.Gosched()
			continue
		}

		// Stop spinning before parking
		spinning = false
		w.scheduler.nspinning.Add(-1)

		// Park: wait for work signal
		w.scheduler.wakeMu.Lock()
		w.scheduler.nparked.Add(1)

		for {
			if w.scheduler.stopping.Load() {
				w.scheduler.nparked.Add(-1)
				w.scheduler.wakeMu.Unlock()
				return
			}
			// Check for work while holding lock
			if proc := w.findWork(); proc != nil {
				w.scheduler.nparked.Add(-1)
				w.scheduler.wakeMu.Unlock()
				w.spins = 0
				w.executeOne(proc)
				w.executed.Add(1)
				break
			}
			// No work - wait for signal
			w.scheduler.wakeCond.Wait()
		}
	}
}

// procyield spins for cycles iterations.
//
//go:linkname procyield runtime.procyield
func procyield(cycles uint32)

// findWork searches for work in priority order.
func (w *Worker) findWork() *Processor {
	// 1. LIFO slot (hot task, best cache locality)
	if p := w.lifoSlot.Swap(nil); p != nil {
		return p
	}

	// 2. Local deque (fast path, no contention)
	if p := w.local.Pop(); p != nil {
		return p
	}

	// 3. Global queue - always check (new work arrives here)
	if p := w.scheduler.global.Pop(); p != nil {
		// Batch fetch additional items to reduce contention
		n := w.scheduler.global.PopN(w.batchBuf[:16])
		for i := 0; i < n; i++ {
			if w.batchBuf[i] != nil {
				w.local.Push(w.batchBuf[i])
				w.batchBuf[i] = nil
			}
		}
		return p
	}

	// 4. Steal from a random victim
	return w.steal()
}

// steal attempts to take work from another worker's deque.
// Uses random victim selection to distribute load.
func (w *Worker) steal() *Processor {
	workers := w.scheduler.workers
	n := len(workers)
	if n <= 1 {
		return nil
	}

	// Random starting point to avoid thundering herd
	start := w.rng.Intn(n)

	for i := 0; i < n; i++ {
		victim := (start + i) % n
		if victim == w.id {
			continue
		}

		// Try to steal half of victim's work
		count := workers[victim].local.StealHalfInto(w.batchBuf[:])
		if count > 0 {
			w.stolen.Add(uint64(count))

			// Push all but first to local queue
			for j := 1; j < count; j++ {
				w.local.Push(w.batchBuf[j])
				w.batchBuf[j] = nil
			}

			first := w.batchBuf[0]
			w.batchBuf[0] = nil
			return first
		}
	}

	return nil
}

// executeOne runs a single processor through one step cycle.
func (w *Worker) executeOne(proc *Processor) {
	if proc.Process == nil {
		// Safeguard: processor was released, skip it
		return
	}
	proc.State = StateRunning

	// Prepare yield results from previous handler completion
	var yieldResults *YieldResults
	if proc.hasYieldResult {
		yieldResults = &proc.yieldResult
		proc.hasYieldResult = false
	}

	// Step the process
	result, err := proc.Process.Step(yieldResults)
	proc.StepCount++

	// Clear yield result after consumption
	if yieldResults != nil {
		proc.yieldResult.Data = nil
		proc.yieldResult.Error = nil
	}

	// Handle step error
	if err != nil {
		proc.State = StateComplete
		w.scheduler.completeProcessor(proc, nil, err)
		return
	}

	yields := result.GetYields()

	switch result.Status {
	case StepDone:
		proc.State = StateComplete
		w.scheduler.completeProcessor(proc, yields, nil)

	case StepIdle:
		proc.State = StateIdle
		w.scheduler.parkIdle(proc)

	case StepContinue:
		if len(yields) == 0 {
			// Continue without yields - use LIFO slot for hot path
			proc.State = StateReady
			if old := w.lifoSlot.Swap(proc); old != nil {
				w.local.Push(old)
			}
			return
		}

		// Dispatch first yield to handler
		cmd := yields[0]
		handler := w.scheduler.getHandler(cmd)
		if handler == nil {
			proc.State = StateComplete
			w.scheduler.completeProcessor(proc, nil, &UnknownCommandError{ID: cmd.CmdID()})
			return
		}

		proc.State = StateBlocked
		atomic.StoreInt32(&proc.executingWorker, int32(w.id+1)) // +1 so 0 means unset
		handler.Handle(cmd, proc)

		// Check if sync handler (Complete() set executingWorker to -1)
		if atomic.CompareAndSwapInt32(&proc.executingWorker, -1, 0) {
			// Sync handler completed - re-queue locally via LIFO slot
			if old := w.lifoSlot.Swap(proc); old != nil {
				w.local.Push(old)
			}
		} else {
			// Async handler - clear marker, Complete() will push to global
			atomic.StoreInt32(&proc.executingWorker, 0)
		}
	}
}
