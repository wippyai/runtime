package actor

import (
	"context"
	"runtime"
	"sync/atomic"

	"github.com/wippyai/runtime/api/process"
)

type Worker struct {
	id        int
	local     *Deque
	scheduler *Scheduler
	batchBuf  [32]*Processor
	lifoSlot  atomic.Pointer[Processor]
	executed  atomic.Uint64
	stolen    atomic.Uint64
}

func newWorker(id int, s *Scheduler, stealing bool) *Worker {
	w := &Worker{id: id, scheduler: s}
	if stealing {
		w.local = NewDeque(s.localQueueSize)
	}
	return w
}

func (w *Worker) run() {
	if w.local != nil {
		w.runStealing()
	} else {
		w.runGlobal()
	}
}

func (w *Worker) runGlobal() {
	s := w.scheduler
	for {
		if proc := s.global.Pop(); proc != nil {
			w.executeOne(proc)
			w.executed.Add(1)
			continue
		}

		if s.stopping.Load() {
			return
		}

		s.wakeMu.Lock()
		atomic.AddUint32(&s.idle, 1)
		for s.global.IsEmpty() && !s.stopping.Load() {
			s.wakeCond.Wait()
		}
		atomic.AddUint32(&s.idle, ^uint32(0))
		s.wakeMu.Unlock()
	}
}

func (w *Worker) runStealing() {
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
		atomic.AddUint32(&s.idle, 1)
		for {
			if s.stopping.Load() {
				atomic.AddUint32(&s.idle, ^uint32(0))
				s.wakeMu.Unlock()
				return
			}
			if proc := w.findWork(); proc != nil {
				n := s.global.Len()
				atomic.AddUint32(&s.idle, ^uint32(0))
				if n > 0 {
					s.wakeCond.Signal()
				}
				s.wakeMu.Unlock()
				w.executeOne(proc)
				w.executed.Add(1)
				break
			}
			s.wakeCond.Wait()
		}
	}
}

func (w *Worker) findWork() *Processor {
	if p := w.lifoSlot.Swap(nil); p != nil {
		return p
	}
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

	start := int(w.scheduler.nextID.Load()) % n
	if start == w.id {
		start = (start + 1) % n
	}

	for i := 0; i < n; i++ {
		victim := (start + i) % n
		if victim == w.id || workers[victim].local == nil {
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

	proc.SetState(StateRunning)

	// Drain events from queue
	events := proc.queue.Drain()

	// Reset output for this step
	proc.output.Reset()

	// Step the process
	err := proc.Process.Step(events, &proc.output)

	if err != nil {
		proc.SetState(StateComplete)
		proc.queue.Close()
		w.scheduler.complete(proc, nil, err)
		return
	}

	switch proc.output.Status() {
	case StepDone:
		proc.SetState(StateComplete)
		proc.queue.Close()
		w.scheduler.complete(proc, &proc.output, nil)

	case StepIdle:
		proc.SetState(StateIdle)
		w.scheduler.parkIdle(proc)

	case StepContinue:
		yields := proc.output.Yields()
		if len(yields) == 0 {
			// No yields - continue immediately if events pending
			if proc.queue.HasEvents() {
				proc.SetState(StateReady)
				w.scheduler.global.Push(proc)
				w.scheduler.wake()
			} else {
				// Wait for events
				proc.SetState(StateBlocked)
			}
			return
		}

		// Set blocked BEFORE dispatching so CompleteYield can use CAS
		proc.SetState(StateBlocked)
		ctx := proc.Context()

		// Dispatch all yields - pass proc as ResultReceiver (zero allocation!)
		w.dispatchYields(ctx, proc, yields)
	}
}

// dispatchYields sends all yields to handlers.
func (w *Worker) dispatchYields(ctx context.Context, proc *Processor, yields []Yield) {
	for _, y := range yields {
		handler := w.scheduler.getHandler(y.Cmd)
		if handler == nil {
			// Unknown command - complete with error immediately
			proc.queue.PushDirect(process.Event{
				Type:  process.EventYieldComplete,
				Tag:   y.Tag,
				Error: &UnknownCommandError{ID: y.Cmd.CmdID()},
			})
			continue
		}
		if err := handler.Handle(ctx, y.Cmd, y.Tag, proc); err != nil {
			// Handler returned error - complete with error
			proc.queue.PushDirect(process.Event{
				Type:  process.EventYieldComplete,
				Tag:   y.Tag,
				Error: err,
			})
		}
	}

	// If events arrived during dispatch (from PushDirect above or CompleteYield),
	// try to transition from Blocked to Ready and re-queue.
	// CompleteYield may have already done this, in which case CAS fails (safe no-op).
	if proc.queue.HasEvents() {
		if proc.casState(StateBlocked, StateReady) {
			w.scheduler.global.Push(proc)
			w.scheduler.wake()
		}
	}
}
