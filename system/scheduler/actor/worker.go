package actor

import (
	"context"
	"sync/atomic"

	"github.com/wippyai/runtime/api/dispatcher"
)

// Worker processes Processors from the global queue.
// Simplified design for async dispatchers - no work-stealing needed.
type Worker struct {
	id        int
	scheduler *Scheduler

	// Metrics
	executed atomic.Uint64
}

// newWorker creates a worker.
func newWorker(id int, s *Scheduler) *Worker {
	return &Worker{
		id:        id,
		scheduler: s,
	}
}

// run is the worker's main loop.
// Simple: pop from queue, execute, park if empty.
func (w *Worker) run() {
	s := w.scheduler

	for {
		// Try to get work from global queue
		proc := s.global.Pop()
		if proc != nil {
			w.executeOne(proc)
			w.executed.Add(1)
			continue
		}

		// No work - check if stopping
		if s.stopping.Load() {
			return
		}

		// Park until work arrives or shutdown
		s.wakeMu.Lock()
		for s.global.IsEmpty() && !s.stopping.Load() {
			s.wakeCond.Wait()
		}
		s.wakeMu.Unlock()
	}
}

// executeOne runs a single processor through one step cycle.
func (w *Worker) executeOne(proc *Processor) {
	if proc.Process == nil {
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

	switch result.Status {
	case StepDone:
		proc.State = StateComplete
		w.scheduler.completeProcessor(proc, &result, nil)

	case StepIdle:
		proc.State = StateIdle
		w.scheduler.parkIdle(proc)

	case StepContinue:
		yields := result.GetYields()
		if len(yields) == 0 {
			// Continue without yields - re-queue immediately
			proc.State = StateReady
			w.scheduler.global.Push(proc)
			w.scheduler.wakeWorker()
			return
		}

		proc.State = StateBlocked
		ctx := proc.Context()

		if len(yields) == 1 {
			// Single yield - fast path, pass Processor as Emitter (zero allocation)
			cmd := yields[0]
			handler := w.scheduler.getHandler(cmd)
			if handler == nil {
				proc.State = StateComplete
				w.scheduler.completeProcessor(proc, nil, &UnknownCommandError{ID: cmd.CmdID()})
				return
			}
			if err := handler.Handle(ctx, cmd, proc); err != nil {
				proc.Emit(nil, err)
			}
		} else {
			// Multiple yields - validate and run in parallel
			handlers := make([]dispatcher.Handler, len(yields))
			for i, cmd := range yields {
				handlers[i] = w.scheduler.getHandler(cmd)
				if handlers[i] == nil {
					proc.State = StateComplete
					w.scheduler.completeProcessor(proc, nil, &UnknownCommandError{ID: cmd.CmdID()})
					return
				}
			}
			go w.handleMultipleYields(ctx, proc, yields, handlers)
		}
	}
}

// handleMultipleYields executes multiple handlers in parallel and waits for all.
// Uses embedded emitters for zero allocation when yields <= MaxYields.
func (w *Worker) handleMultipleYields(ctx context.Context, proc *Processor, yields []dispatcher.Command, handlers []dispatcher.Handler) {
	n := len(yields)
	proc.initMultiYield(n)

	for i, cmd := range yields {
		emitter := proc.getEmitter(i)
		if err := handlers[i].Handle(ctx, cmd, emitter); err != nil {
			emitter.Emit(nil, err)
		}
	}

	// Wait for all to complete
	if err := proc.waitMultiYield(ctx); err != nil {
		proc.Emit(nil, err)
		return
	}

	// Check for errors, collect results
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
