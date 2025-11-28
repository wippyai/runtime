package testbed

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

// V10: Production-ready scheduler with LIFO local + global overflow
// Matches real scheduler semantics: Process.Step() -> yield -> re-queue

type StepStatus int

const (
	StepContinue StepStatus = iota
	StepDone
)

type ProcessV10 interface {
	Step() StepStatus
}

type ProcessorV10 struct {
	process ProcessV10
	steps   int
}

var procPoolV10 = sync.Pool{
	New: func() any { return &ProcessorV10{} },
}

type SchedulerV10 struct {
	workers    []*workerV10
	global     chan *ProcessorV10 // Overflow and new submissions
	done       atomic.Bool
	wg         sync.WaitGroup
	completed  atomic.Int64
	nextWorker atomic.Uint64
}

type workerV10 struct {
	id      int
	local   []*ProcessorV10 // LIFO stack
	localMu sync.Mutex
	sched   *SchedulerV10
}

func NewSchedulerV10(numWorkers int) *SchedulerV10 {
	s := &SchedulerV10{
		workers: make([]*workerV10, numWorkers),
		global:  make(chan *ProcessorV10, 16384),
	}

	// Initialize all workers first
	for i := 0; i < numWorkers; i++ {
		s.workers[i] = &workerV10{
			id:    i,
			local: make([]*ProcessorV10, 0, 256),
			sched: s,
		}
	}

	// Then start them
	for i := 0; i < numWorkers; i++ {
		s.wg.Add(1)
		go s.workers[i].run()
	}

	return s
}

func (w *workerV10) run() {
	defer w.sched.wg.Done()

	for !w.sched.done.Load() {
		proc := w.findWork()
		if proc == nil {
			runtime.Gosched()
			continue
		}

		// Execute one step
		status := proc.process.Step()
		proc.steps++

		switch status {
		case StepDone:
			w.sched.completed.Add(1)
			proc.process = nil
			proc.steps = 0
			procPoolV10.Put(proc)

		case StepContinue:
			// LIFO re-queue to local
			w.localMu.Lock()
			w.local = append(w.local, proc)
			w.localMu.Unlock()
		}
	}
}

func (w *workerV10) findWork() *ProcessorV10 {
	// 1. Check local LIFO
	w.localMu.Lock()
	if len(w.local) > 0 {
		proc := w.local[len(w.local)-1]
		w.local = w.local[:len(w.local)-1]
		w.localMu.Unlock()
		return proc
	}
	w.localMu.Unlock()

	// 2. Check global (non-blocking)
	select {
	case proc := <-w.sched.global:
		return proc
	default:
	}

	// 3. Steal from other workers
	for i := 0; i < len(w.sched.workers); i++ {
		other := (w.id + i + 1) % len(w.sched.workers)
		victim := w.sched.workers[other]

		victim.localMu.Lock()
		if len(victim.local) > 1 {
			// Steal half
			n := len(victim.local) / 2
			stolen := make([]*ProcessorV10, n)
			copy(stolen, victim.local[:n])
			victim.local = victim.local[n:]
			victim.localMu.Unlock()

			// Keep first, push rest to local
			w.localMu.Lock()
			w.local = append(w.local, stolen[1:]...)
			w.localMu.Unlock()

			return stolen[0]
		}
		victim.localMu.Unlock()
	}

	return nil
}

func (s *SchedulerV10) Submit(p ProcessV10) {
	proc := procPoolV10.Get().(*ProcessorV10)
	proc.process = p

	// Try local first (round-robin)
	worker := int(s.nextWorker.Add(1)) % len(s.workers)
	w := s.workers[worker]

	w.localMu.Lock()
	if len(w.local) < 256 {
		w.local = append(w.local, proc)
		w.localMu.Unlock()
		return
	}
	w.localMu.Unlock()

	// Overflow to global
	s.global <- proc
}

func (s *SchedulerV10) Stop() {
	s.done.Store(true)
	s.wg.Wait()
}

// Test process that runs N steps
type counterProcess struct {
	current int
	target  int
}

func (p *counterProcess) Step() StepStatus {
	p.current++
	if p.current >= p.target {
		return StepDone
	}
	return StepContinue
}

func BenchmarkSchedulerV10_10Steps(b *testing.B) {
	s := NewSchedulerV10(runtime.GOMAXPROCS(0))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Submit(&counterProcess{target: 10})
	}

	for s.completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
	b.StopTimer()
	s.Stop()
}

func BenchmarkSchedulerV10_10StepsParallel(b *testing.B) {
	s := NewSchedulerV10(runtime.GOMAXPROCS(0))

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			s.Submit(&counterProcess{target: 10})
		}
	})

	for s.completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
	b.StopTimer()
	s.Stop()
}

// V10b: Same but with no mutex on local (atomic LIFO slot + channel)
type SchedulerV10b struct {
	workers    []*workerV10b
	done       atomic.Bool
	wg         sync.WaitGroup
	completed  atomic.Int64
	nextWorker atomic.Uint64
}

type workerV10b struct {
	id    int
	lifo  atomic.Pointer[ProcessorV10] // Hot LIFO slot
	local chan *ProcessorV10           // Local queue
	sched *SchedulerV10b
}

func NewSchedulerV10b(numWorkers int) *SchedulerV10b {
	s := &SchedulerV10b{
		workers: make([]*workerV10b, numWorkers),
	}

	for i := 0; i < numWorkers; i++ {
		s.workers[i] = &workerV10b{
			id:    i,
			local: make(chan *ProcessorV10, 256),
			sched: s,
		}
	}

	for i := 0; i < numWorkers; i++ {
		s.wg.Add(1)
		go s.workers[i].run()
	}

	return s
}

func (w *workerV10b) run() {
	defer w.sched.wg.Done()

	for !w.sched.done.Load() {
		// 1. Check LIFO slot
		proc := w.lifo.Swap(nil)

		// 2. Check local channel
		if proc == nil {
			select {
			case proc = <-w.local:
			default:
			}
		}

		// 3. Steal from others
		if proc == nil {
			for i := 0; i < len(w.sched.workers); i++ {
				other := (w.id + i + 1) % len(w.sched.workers)
				select {
				case proc = <-w.sched.workers[other].local:
					goto found
				default:
				}
			}
		}

		if proc == nil {
			// Block on local
			select {
			case proc = <-w.local:
			default:
				runtime.Gosched()
				continue
			}
		}

	found:
		if proc == nil {
			continue
		}

		status := proc.process.Step()
		proc.steps++

		switch status {
		case StepDone:
			w.sched.completed.Add(1)
			proc.process = nil
			proc.steps = 0
			procPoolV10.Put(proc)

		case StepContinue:
			// LIFO slot, overflow to channel
			if old := w.lifo.Swap(proc); old != nil {
				w.local <- old
			}
		}
	}
}

func (s *SchedulerV10b) Submit(p ProcessV10) {
	proc := procPoolV10.Get().(*ProcessorV10)
	proc.process = p

	worker := int(s.nextWorker.Add(1)) % len(s.workers)
	s.workers[worker].local <- proc
}

func (s *SchedulerV10b) Stop() {
	s.done.Store(true)
	s.wg.Wait()
}

func BenchmarkSchedulerV10b_10Steps(b *testing.B) {
	s := NewSchedulerV10b(runtime.GOMAXPROCS(0))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Submit(&counterProcess{target: 10})
	}

	for s.completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
	b.StopTimer()
	s.Stop()
}

func BenchmarkSchedulerV10b_10StepsParallel(b *testing.B) {
	s := NewSchedulerV10b(runtime.GOMAXPROCS(0))

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			s.Submit(&counterProcess{target: 10})
		}
	})

	for s.completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
	b.StopTimer()
	s.Stop()
}

// Comparison with current real scheduler throughput test
func BenchmarkSchedulerV10_1Step(b *testing.B) {
	s := NewSchedulerV10(runtime.GOMAXPROCS(0))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Submit(&counterProcess{target: 1})
	}

	for s.completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
	b.StopTimer()
	s.Stop()
}

// V11: Run all steps without re-queue (batch execution per process)
type SchedulerV11 struct {
	workers    []*workerV11
	global     chan *ProcessorV10
	done       atomic.Bool
	wg         sync.WaitGroup
	completed  atomic.Int64
	nextWorker atomic.Uint64
}

type workerV11 struct {
	id    int
	local chan *ProcessorV10
	sched *SchedulerV11
}

func NewSchedulerV11(numWorkers int) *SchedulerV11 {
	s := &SchedulerV11{
		workers: make([]*workerV11, numWorkers),
		global:  make(chan *ProcessorV10, 16384),
	}

	for i := 0; i < numWorkers; i++ {
		s.workers[i] = &workerV11{
			id:    i,
			local: make(chan *ProcessorV10, 256),
			sched: s,
		}
	}

	for i := 0; i < numWorkers; i++ {
		s.wg.Add(1)
		go s.workers[i].run()
	}

	return s
}

func (w *workerV11) run() {
	defer w.sched.wg.Done()

	for !w.sched.done.Load() {
		var proc *ProcessorV10

		// Try local first
		select {
		case proc = <-w.local:
		default:
		}

		// Try global
		if proc == nil {
			select {
			case proc = <-w.sched.global:
			default:
			}
		}

		// Steal from others
		if proc == nil {
			for i := 0; i < len(w.sched.workers); i++ {
				other := (w.id + i + 1) % len(w.sched.workers)
				select {
				case proc = <-w.sched.workers[other].local:
					goto found
				default:
				}
			}
		}

		if proc == nil {
			runtime.Gosched()
			continue
		}

	found:
		// Run ALL steps without re-queue
		for proc.process.Step() == StepContinue {
		}

		w.sched.completed.Add(1)
		proc.process = nil
		proc.steps = 0
		procPoolV10.Put(proc)
	}
}

func (s *SchedulerV11) Submit(p ProcessV10) {
	proc := procPoolV10.Get().(*ProcessorV10)
	proc.process = p

	worker := int(s.nextWorker.Add(1)) % len(s.workers)
	select {
	case s.workers[worker].local <- proc:
	default:
		s.global <- proc
	}
}

func (s *SchedulerV11) Stop() {
	s.done.Store(true)
	s.wg.Wait()
}

func BenchmarkSchedulerV11_10Steps(b *testing.B) {
	s := NewSchedulerV11(runtime.GOMAXPROCS(0))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Submit(&counterProcess{target: 10})
	}

	for s.completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
	b.StopTimer()
	s.Stop()
}

func BenchmarkSchedulerV11_10StepsParallel(b *testing.B) {
	s := NewSchedulerV11(runtime.GOMAXPROCS(0))

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			s.Submit(&counterProcess{target: 10})
		}
	})

	for s.completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
	b.StopTimer()
	s.Stop()
}

// V12: Sync handler detection - continue without re-queue if handler completes inline
// This matches real scheduler semantics where sync handlers avoid queue overhead

type SyncDetectingScheduler struct {
	workers    []*syncWorker
	global     chan *ProcessorV10
	done       atomic.Bool
	wg         sync.WaitGroup
	completed  atomic.Int64
	nextWorker atomic.Uint64
}

type syncWorker struct {
	id        int
	local     []*ProcessorV10
	localMu   sync.Mutex
	sched     *SyncDetectingScheduler
	executing atomic.Pointer[ProcessorV10] // Currently executing processor
}

func NewSyncDetectingScheduler(numWorkers int) *SyncDetectingScheduler {
	s := &SyncDetectingScheduler{
		workers: make([]*syncWorker, numWorkers),
		global:  make(chan *ProcessorV10, 16384),
	}

	for i := 0; i < numWorkers; i++ {
		s.workers[i] = &syncWorker{
			id:    i,
			local: make([]*ProcessorV10, 0, 256),
			sched: s,
		}
	}

	for i := 0; i < numWorkers; i++ {
		s.wg.Add(1)
		go s.workers[i].run()
	}

	return s
}

func (w *syncWorker) run() {
	defer w.sched.wg.Done()

	for !w.sched.done.Load() {
		proc := w.findWork()
		if proc == nil {
			runtime.Gosched()
			continue
		}

		// Process until done or async yield
		for {
			status := proc.process.Step()
			proc.steps++

			if status == StepDone {
				w.sched.completed.Add(1)
				proc.process = nil
				proc.steps = 0
				procPoolV10.Put(proc)
				break
			}

			// StepContinue - check if "handler" completed synchronously
			// In real scheduler, this is where handler.Handle() would be called
			// For sync handler: continue loop immediately
			// For async handler: proc gets re-queued by Complete() callback

			// Simulate sync handler - just continue without re-queue
		}
	}
}

func (w *syncWorker) findWork() *ProcessorV10 {
	// Check local LIFO
	w.localMu.Lock()
	if len(w.local) > 0 {
		proc := w.local[len(w.local)-1]
		w.local = w.local[:len(w.local)-1]
		w.localMu.Unlock()
		return proc
	}
	w.localMu.Unlock()

	// Check global
	select {
	case proc := <-w.sched.global:
		return proc
	default:
	}

	// Steal
	for i := 0; i < len(w.sched.workers); i++ {
		other := (w.id + i + 1) % len(w.sched.workers)
		victim := w.sched.workers[other]

		victim.localMu.Lock()
		if len(victim.local) > 1 {
			n := len(victim.local) / 2
			stolen := make([]*ProcessorV10, n)
			copy(stolen, victim.local[:n])
			victim.local = victim.local[n:]
			victim.localMu.Unlock()

			w.localMu.Lock()
			w.local = append(w.local, stolen[1:]...)
			w.localMu.Unlock()
			return stolen[0]
		}
		victim.localMu.Unlock()
	}

	return nil
}

func (s *SyncDetectingScheduler) Submit(p ProcessV10) {
	proc := procPoolV10.Get().(*ProcessorV10)
	proc.process = p

	worker := int(s.nextWorker.Add(1)) % len(s.workers)
	w := s.workers[worker]

	w.localMu.Lock()
	if len(w.local) < 256 {
		w.local = append(w.local, proc)
		w.localMu.Unlock()
		return
	}
	w.localMu.Unlock()

	s.global <- proc
}

func (s *SyncDetectingScheduler) Stop() {
	s.done.Store(true)
	s.wg.Wait()
}

func BenchmarkSyncDetecting_10Steps(b *testing.B) {
	s := NewSyncDetectingScheduler(runtime.GOMAXPROCS(0))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Submit(&counterProcess{target: 10})
	}

	for s.completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
	b.StopTimer()
	s.Stop()
}

func BenchmarkSyncDetecting_10StepsParallel(b *testing.B) {
	s := NewSyncDetectingScheduler(runtime.GOMAXPROCS(0))

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			s.Submit(&counterProcess{target: 10})
		}
	})

	for s.completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
	b.StopTimer()
	s.Stop()
}
