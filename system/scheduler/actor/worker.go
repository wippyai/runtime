package actor

import (
	"context"
	"runtime"
	"sync/atomic"

	"github.com/wippyai/runtime/api/dispatcher"
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
	proc.State = StateRunning

	var yieldResults *YieldResults
	if proc.hasYieldResult {
		yieldResults = &proc.yieldResult
		proc.hasYieldResult = false
	}

	result, err := proc.Process.Step(yieldResults)

	if yieldResults != nil {
		proc.yieldResult.Data = nil
		proc.yieldResult.Error = nil
	}

	if err != nil {
		proc.State = StateComplete
		w.scheduler.complete(proc, nil, err)
		return
	}

	switch result.Status {
	case StepDone:
		proc.State = StateComplete
		w.scheduler.complete(proc, &result, nil)

	case StepIdle:
		proc.State = StateIdle
		w.scheduler.parkIdle(proc)

	case StepContinue:
		yields := result.GetYields()
		if len(yields) == 0 {
			proc.State = StateReady
			if w.local != nil {
				if old := w.lifoSlot.Swap(proc); old != nil {
					w.local.Push(old)
				}
			} else {
				w.scheduler.global.Push(proc)
				w.scheduler.wake()
			}
			return
		}

		proc.State = StateBlocked
		ctx := proc.Context()

		if len(yields) == 1 {
			cmd := yields[0]
			handler := w.scheduler.getHandler(cmd)
			if handler == nil {
				proc.State = StateComplete
				w.scheduler.complete(proc, nil, &UnknownCommandError{ID: cmd.CmdID()})
				return
			}
			if err := handler.Handle(ctx, cmd, proc); err != nil {
				proc.Emit(nil, err)
			}
		} else {
			handlers := make([]dispatcher.Handler, len(yields))
			for i, cmd := range yields {
				handlers[i] = w.scheduler.getHandler(cmd)
				if handlers[i] == nil {
					proc.State = StateComplete
					w.scheduler.complete(proc, nil, &UnknownCommandError{ID: cmd.CmdID()})
					return
				}
			}
			go w.handleMultiYields(ctx, proc, yields, handlers)
		}
	}
}

func (w *Worker) handleMultiYields(ctx context.Context, proc *Processor, yields []dispatcher.Command, handlers []dispatcher.Handler) {
	n := len(yields)
	proc.initMultiYield(n)

	for i, cmd := range yields {
		emitter := proc.getEmitter(i)
		if err := handlers[i].Handle(ctx, cmd, emitter); err != nil {
			emitter.Emit(nil, err)
		}
	}

	if err := proc.waitMultiYield(ctx); err != nil {
		proc.Emit(nil, err)
		return
	}

	results := make([]any, n)
	for i := 0; i < n; i++ {
		slot := proc.getSlot(i)
		if slot.Error != nil {
			proc.Emit(nil, slot.Error)
			return
		}
		results[i] = slot.Data
	}
	proc.Emit(results, nil)
}
