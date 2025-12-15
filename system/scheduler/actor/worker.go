package actor

import (
	"context"
	"runtime"
	"sync/atomic"

	"github.com/wippyai/runtime/api/process"
	sysprocess "github.com/wippyai/runtime/system/process"
)

type Worker struct {
	id        int
	local     *Deque
	scheduler *Scheduler
	batchBuf  [32]*Processor
	executed  atomic.Uint64
	stolen    atomic.Uint64
}

func newWorker(id int, s *Scheduler) *Worker {
	return &Worker{
		id:        id,
		scheduler: s,
		local:     NewDeque(s.localQueueSize),
	}
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
			return
		}

		spins++
		if spins < 4 {
			continue
		}
		if spins < 16 {
			runtime.Gosched()
			continue
		}

		spins = 0
		s.wakeMu.Lock()
		for {
			if proc := w.findWork(); proc != nil {
				n := s.global.Len()
				if n > 0 {
					s.wakeCond.Signal()
				}
				s.wakeMu.Unlock()
				w.executeOne(proc)
				w.executed.Add(1)
				break
			}
			// Only exit when no work AND stopping
			if s.stopping.Load() {
				s.wakeMu.Unlock()
				return
			}
			s.wakeCond.Wait()
		}
	}
}

func (w *Worker) findWork() *Processor {
	if p := w.local.Pop(); p != nil {
		return p
	}
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

	start := int(w.scheduler.nextID.Load()) % n //nolint:gosec // safe: n is always small (worker count)
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
	if proc.Process == nil {
		return
	}

	// Atomically transition Ready->Running. If CAS fails, process was
	// terminated or is in unexpected state - skip execution.
	if !proc.casState(StateReady, StateRunning) {
		return
	}

	// Check context cancellation before executing (Terminate sets this)
	if proc.ctx != nil && proc.ctx.Err() != nil {
		if !proc.casState(StateRunning, StateComplete) {
			return
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
	err := proc.Process.Step(events, &proc.output)

	if err != nil {
		if !proc.casState(StateRunning, StateComplete) {
			return
		}
		proc.queue.Close()
		w.scheduler.complete(proc, nil, err)
		return
	}

	status := proc.output.Status()

	// Handle Done first - no yields to dispatch
	if status == process.StepDone {
		if !proc.casState(StateRunning, StateComplete) {
			return
		}
		proc.queue.Close()
		w.scheduler.complete(proc, &proc.output, nil)
		return
	}

	// Dispatch any yields (for all statuses except Done)
	yields := proc.output.Yields()
	if len(yields) > 0 {
		w.dispatchYields(proc.ctx, proc, yields)
		return
	}

	// No yields - handle status (StepDone handled above)
	switch status {
	case process.StepDone:
	case process.StepContinue:
		if !proc.casState(StateRunning, StateReady) {
			return
		}
		w.scheduler.global.Push(proc)
		w.scheduler.wake()

	case process.StepYield:
		if !proc.casState(StateRunning, StateBlocked) {
			return
		}
		if proc.queue.HasEvents() {
			if proc.casState(StateBlocked, StateReady) {
				w.scheduler.global.Push(proc)
				w.scheduler.wake()
			}
		}

	case process.StepIdle:
		if !proc.casState(StateRunning, StateIdle) {
			return
		}
		if proc.queue.HasEvents() {
			if proc.casState(StateIdle, StateReady) {
				w.scheduler.global.Push(proc)
				w.scheduler.wake()
			}
		}
	}
}

// dispatchYields sends all yields to handlers.
// Processor state is StateRunning during this call.
// CompleteYield sets wakeup flag instead of re-queueing while Running.
func (w *Worker) dispatchYields(ctx context.Context, proc *Processor, yields []process.Yield) {
	completer := proc.queue.NewYieldCompleter(w.scheduler)

	for _, y := range yields {
		handler := w.scheduler.getHandler(y.Cmd)
		if handler == nil {
			proc.queue.PushDirect(process.Event{
				Type:  process.EventYieldComplete,
				Tag:   y.Tag,
				Error: process.NewUnknownCommandError(y.Cmd.CmdID()),
			})
			continue
		}
		if err := handler.Handle(ctx, y.Cmd, y.Tag, completer); err != nil {
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
		w.scheduler.global.Push(proc)
		w.scheduler.wake()
		return
	}

	// Now in StateBlocked. Check for events from PushDirect above.
	if proc.queue.HasEvents() {
		if proc.casState(StateBlocked, StateReady) {
			w.scheduler.global.Push(proc)
			w.scheduler.wake()
		}
	}
}
