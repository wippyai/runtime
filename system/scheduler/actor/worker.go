// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"fmt"
	goruntime "runtime"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/runtime"
	sysprocess "github.com/wippyai/runtime/system/process"
)

type Worker struct {
	batchBuf  [32]*Processor
	local     *Deque
	inject    *InjectQueue
	scheduler *Scheduler
	parkCond  *sync.Cond
	id        int
	executed  atomic.Uint64
	stolen    atomic.Uint64
	parkMu    sync.Mutex
	notified  atomic.Bool
}

func newWorker(id int, s *Scheduler) *Worker {
	w := &Worker{
		id:        id,
		scheduler: s,
		local:     NewDeque(s.localQueueSize),
		inject:    NewInjectQueue(),
	}
	w.parkCond = sync.NewCond(&w.parkMu)
	return w
}

func (w *Worker) run() {
	s := w.scheduler
	spins := 0

	for {
		if proc := w.findWork(); proc != nil {
			spins = 0
			w.executeOne(proc)
			w.executed.Add(1)
			continue
		}

		if s.stopping.Load() {
			w.drain()
			return
		}

		spins++
		if spins < 4 {
			continue
		}
		if spins < 16 {
			goruntime.Gosched()
			continue
		}

		spins = 0
		w.park()
		if s.stopping.Load() {
			w.drain()
			return
		}
	}
}

func (w *Worker) park() {
	s := w.scheduler
	w.parkMu.Lock()
	for {
		if proc := w.findWork(); proc != nil {
			shouldWake := s.global.Len() > 0
			w.parkMu.Unlock()
			if shouldWake {
				s.wakeAny()
			}
			w.executeOne(proc)
			w.executed.Add(1)
			return
		}
		if s.stopping.Load() {
			w.parkMu.Unlock()
			return
		}

		// Consume one pending wake token without sleeping.
		if w.notified.Swap(false) {
			continue
		}
		w.parkCond.Wait()
	}
}

func (w *Worker) drain() {
	for {
		if proc := w.findWork(); proc != nil {
			w.executeOne(proc)
			w.executed.Add(1)
			continue
		}
		return
	}
}

func (w *Worker) signal() bool {
	// Coalesce wakeups with a CAS token. This prevents missed wake races where
	// a worker is transitioning into park() while work is enqueued.
	if !w.notified.CompareAndSwap(false, true) {
		return false
	}

	w.parkMu.Lock()
	w.parkCond.Signal()
	w.parkMu.Unlock()
	return true
}

func (w *Worker) findWork() *Processor {
	// Check local deque first (LIFO, cache-hot)
	if p := w.local.Pop(); p != nil {
		return p
	}

	// Check inject queue (async completions with affinity to this worker)
	if p := w.inject.Pop(); p != nil {
		// Drain more from inject to local for batch processing
		n := w.inject.Drain(w.batchBuf[:16])
		for i := 0; i < n; i++ {
			w.local.Push(w.batchBuf[i])
			w.batchBuf[i] = nil
		}
		return p
	}

	// Check global queue (new submissions, no affinity)
	if p := w.scheduler.global.Pop(); p != nil {
		n := w.scheduler.global.PopN(w.batchBuf[:16])
		for i := 0; i < n; i++ {
			w.local.Push(w.batchBuf[i])
			w.batchBuf[i] = nil
		}
		return p
	}

	return w.steal()
}

func (w *Worker) steal() *Processor {
	workers := w.scheduler.workers
	n := len(workers)
	if n <= 1 {
		return nil
	}

	start := int(w.scheduler.nextID.Load()) % n
	if start == w.id {
		start = (start + 1) % n
	}

	for i := 0; i < n; i++ {
		victim := (start + i) % n
		if victim == w.id {
			continue
		}

		count := workers[victim].local.StealHalfInto(w.batchBuf[:])
		if count > 0 {
			w.stolen.Add(uint64(count))
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

func (w *Worker) executeOne(proc *Processor) {
	// Set worker affinity before any yields can complete.
	// This ensures async completions route back to this worker.
	proc.lastWorker.Store(int32(w.id))

	// Atomically transition Ready->Running. If CAS fails, process was
	// terminated or is in unexpected state - skip execution.
	if !proc.casState(StateReady, StateRunning) {
		return
	}

	stepper := proc.Process
	if stepper == nil {
		// Released/stale processor entry. Transition out of Running so future
		// stale queue pops are ignored.
		_ = proc.casState(StateRunning, StateComplete)
		return
	}

	// Check context cancellation before executing (Terminate sets this)
	if proc.ctx != nil && proc.ctx.Err() != nil {
		if !proc.casState(StateRunning, StateComplete) {
			return
		}
		// Signal ephemeral producers to stop before tearing down the queue,
		// so a terminated process doesn't leak its channel-anchored producers.
		if a, ok := stepper.(interface{ Abort() }); ok {
			a.Abort()
		}
		proc.queue.Close()
		w.scheduler.complete(proc, nil, sysprocess.ErrTerminated)
		return
	}

	// Drain events from queue
	events := proc.queue.Drain()

	// Reset output for this step
	proc.output.Reset()

	// Step the process
	err := stepper.Step(events, &proc.output)
	proc.steps.Add(1)

	// Snapshot process stats when collection is enabled
	if err == nil && w.scheduler.collectStats.Load() {
		if sp, ok := stepper.(process.StatsProvider); ok {
			if a := sp.Stats(); a != nil {
				if bag, ok := a.(attrs.Bag); ok {
					proc.stats.Store(&bag)
				}
			}
		}
	}

	if err != nil {
		if !proc.casState(StateRunning, StateComplete) {
			return
		}
		proc.queue.Close()
		w.scheduler.complete(proc, nil, err)
		return
	}

	status := proc.output.Status()

	// Dispatch any yields (for non-Done statuses)
	if status != process.StepDone {
		yields := proc.output.Yields()
		if len(yields) > 0 {
			w.dispatchYields(proc.ctx, proc, yields)
			return
		}
	}

	// Handle status
	switch status {
	case process.StepDone:
		if !proc.casState(StateRunning, StateComplete) {
			return
		}
		proc.queue.Close()
		w.scheduler.complete(proc, &proc.output, nil)

	case process.StepContinue:
		if !proc.casState(StateRunning, StateReady) {
			return
		}
		// Push to local deque - same worker will pick it up next iteration.
		// No wake needed since we're the active worker.
		w.local.Push(proc)

	case process.StepYield:
		if !proc.casState(StateRunning, StateBlocked) {
			return
		}
		if proc.queue.HasEvents() {
			if proc.casState(StateBlocked, StateReady) {
				w.local.Push(proc)
			}
		}

	case process.StepIdle:
		if !proc.casState(StateRunning, StateIdle) {
			return
		}
		if proc.queue.HasEvents() {
			if proc.casState(StateIdle, StateReady) {
				w.local.Push(proc)
			}
		}

	case process.StepUpgrade:
		req := proc.output.Upgrade()
		if req == nil {
			proc.Process.Close()
			if !proc.casState(StateRunning, StateComplete) {
				return
			}
			proc.queue.Close()
			w.scheduler.complete(proc, nil, fmt.Errorf("upgrade: no request"))
			return
		}

		factory := process.GetFactory(proc.ctx)
		if factory == nil {
			proc.Process.Close()
			if !proc.casState(StateRunning, StateComplete) {
				return
			}
			proc.queue.Close()
			w.scheduler.complete(proc, nil, fmt.Errorf("upgrade: no factory"))
			return
		}

		// Resolve source (empty = current definition)
		source := req.Source
		if source.Name == "" {
			var ok bool
			source, ok = runtime.GetFrameID(proc.ctx)
			if !ok {
				proc.Process.Close()
				if !proc.casState(StateRunning, StateComplete) {
					return
				}
				proc.queue.Close()
				w.scheduler.complete(proc, nil, fmt.Errorf("upgrade: no source"))
				return
			}
		}

		// Create new process
		newProc, meta, err := factory.Create(source)
		if err != nil {
			proc.Process.Close()
			if !proc.casState(StateRunning, StateComplete) {
				return
			}
			proc.queue.Close()
			w.scheduler.complete(proc, nil, fmt.Errorf("upgrade: create failed: %w", err))
			return
		}

		// Close old process
		proc.Process.Close()

		// Swap
		proc.Process = newProc

		// Init new process
		method := "main"
		if meta != nil && meta.Method != "" {
			method = meta.Method
		}
		upgradeCtx, _ := ctxapi.OpenFrameContext(proc.ctx)
		if err := newProc.Init(upgradeCtx, method, req.Input); err != nil {
			proc.Process.Close()
			if !proc.casState(StateRunning, StateComplete) {
				return
			}
			proc.queue.Close()
			w.scheduler.complete(proc, nil, fmt.Errorf("upgrade: init failed: %w", err))
			return
		}
		proc.ctx = upgradeCtx

		// Success - re-queue to local
		if !proc.casState(StateRunning, StateReady) {
			return
		}
		w.local.Push(proc)
	}
}

// dispatchYields sends all yields to handlers.
// Processor state is StateRunning during this call.
// CompleteYield sets wakeup flag instead of re-queueing while Running.
func (w *Worker) dispatchYields(ctx context.Context, proc *Processor, yields []process.Yield) {
	for _, y := range yields {
		handler := w.scheduler.getHandler(y.Cmd)
		if handler == nil {
			proc.queue.PushDirect(process.Event{
				Type:  process.EventYieldComplete,
				Tag:   y.Tag,
				Error: sysprocess.NewUnknownCommandError(y.Cmd.CmdID()),
			})
			continue
		}
		if err := handler.Handle(ctx, y.Cmd, y.Tag, proc); err != nil {
			proc.queue.PushDirect(process.Event{
				Type:  process.EventYieldComplete,
				Tag:   y.Tag,
				Error: err,
			})
		}
	}

	// Atomically transition to final state.
	// If wakeup was set by CompleteYield, state becomes Ready - re-queue.
	// Otherwise, state becomes Blocked.
	if proc.finishDispatch() {
		// Wakeup was set during dispatch - push to local, we'll execute next.
		w.local.Push(proc)
		return
	}

	// Now in StateBlocked. Check for events from PushDirect above.
	if proc.queue.HasEvents() {
		if proc.casState(StateBlocked, StateReady) {
			w.local.Push(proc)
		}
	}
}
