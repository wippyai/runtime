// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"errors"
	goruntime "runtime"

	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/system/scheduler/affinity"
)

// ErrInvalidWorkerCount is returned when a resize would leave the scheduler
// without an execution worker.
var ErrInvalidWorkerCount = errors.New("worker count must be greater than zero")

// workerSet is immutable after publication. Readers can retain a snapshot
// without locking while a concurrent resize publishes its successor.
type workerSet struct {
	active []*Worker
}

func (s *Scheduler) Start() {
	s.controlMu.Lock()
	defer s.controlMu.Unlock()

	if s.started || s.isStopping() {
		return
	}
	s.started = true
	for _, w := range s.workerSnapshot() {
		s.startWorker(w)
	}
}

// ResizeWorkers changes scheduler execution capacity without replacing the
// scheduler or any processors. Growing appends workers. Shrinking first removes
// the highest-numbered workers from routing; each then finishes its current
// step, hands queued work to the global queue, and exits asynchronously.
func (s *Scheduler) ResizeWorkers(target int) error {
	if target <= 0 {
		return ErrInvalidWorkerCount
	}
	if s.isStopping() {
		return process.ErrSchedulerStopping
	}

	s.controlMu.Lock()
	defer s.controlMu.Unlock()

	if s.isStopping() {
		return process.ErrSchedulerStopping
	}

	current := s.workerSnapshot()
	if target == len(current) {
		return nil
	}
	if target > len(current) {
		s.growWorkers(current, target)
		return nil
	}
	s.shrinkWorkers(current, target)
	return nil
}

func (s *Scheduler) growWorkers(current []*Worker, target int) {
	resized := append([]*Worker(nil), current...)
	added := make([]*Worker, 0, target-len(current))
	for id := len(current); id < target; id++ {
		// IDs are indexes in the published worker set, not global worker
		// identities. A same-ID predecessor may still be finishing retirement,
		// but it is absent from routing and owns a distinct done channel.
		w := newWorker(id, s)
		resized = append(resized, w)
		added = append(added, w)
	}
	s.storeWorkers(resized)
	if s.started {
		for _, w := range added {
			s.startWorker(w)
		}
	}
	s.wakeAll()
}

func (s *Scheduler) shrinkWorkers(current []*Worker, target int) {
	active := append([]*Worker(nil), current[:target]...)
	retiring := append([]*Worker(nil), current[target:]...)

	// Publish the smaller routing set before retirement. A sender that already
	// holds the old snapshot is synchronized by Worker.retire, while all later
	// senders immediately fall back to the global queue.
	s.storeWorkers(active)
	for _, w := range retiring {
		w.retire()
	}
	if !s.started {
		s.collectRetiredStats(retiring)
		return
	}
	go s.awaitRetiredWorkers(retiring)
}

func (s *Scheduler) awaitRetiredWorkers(retiring []*Worker) {
	for _, w := range retiring {
		<-w.done
	}
	s.collectRetiredStats(retiring)
}

func (s *Scheduler) collectRetiredStats(retiring []*Worker) {
	for _, w := range retiring {
		s.retiredExecuted.Add(w.executed.Load())
		s.retiredStolen.Add(w.stolen.Load())
	}
}

func (s *Scheduler) startWorker(w *Worker) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer close(w.done)
		if len(s.pinSet) > 0 {
			goruntime.LockOSThread()
			defer goruntime.UnlockOSThread()
			_ = affinity.Apply(s.pinSet)
		}
		w.run()
	}()
}

func (s *Scheduler) workerSnapshot() []*Worker {
	set := s.workers.Load()
	if set == nil {
		return nil
	}
	return set.active
}

func (s *Scheduler) storeWorkers(workers []*Worker) {
	s.workers.Store(&workerSet{active: workers})
}
