package testbed

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

// V6: Real yield semantics - processor is re-queued after EACH yield
// This models what the real scheduler does: Step -> Yield -> Complete -> Re-queue

type realProcessor struct {
	steps     int
	maxSteps  int
	completed bool
}

var realProcPool = sync.Pool{
	New: func() any { return &realProcessor{} },
}

// RealSchedulerV6: Single queue, re-queue after each step
type RealSchedulerV6 struct {
	queue     chan *realProcessor
	done      chan struct{}
	wg        sync.WaitGroup
	submitted atomic.Int64
	completed atomic.Int64
}

func NewRealSchedulerV6(workers int) *RealSchedulerV6 {
	s := &RealSchedulerV6{
		queue: make(chan *realProcessor, 65536),
		done:  make(chan struct{}),
	}

	for i := 0; i < workers; i++ {
		s.wg.Add(1)
		go s.worker()
	}

	return s
}

func (s *RealSchedulerV6) worker() {
	defer s.wg.Done()
	for {
		select {
		case p := <-s.queue:
			// Execute one step
			p.steps++

			if p.steps >= p.maxSteps {
				// Done - complete
				p.completed = true
				s.completed.Add(1)
				// Return to pool
				p.steps = 0
				p.maxSteps = 0
				p.completed = false
				realProcPool.Put(p)
			} else {
				// Re-queue for next step (like Complete() does)
				s.queue <- p
			}
		case <-s.done:
			return
		}
	}
}

func (s *RealSchedulerV6) Submit(maxSteps int) {
	p := realProcPool.Get().(*realProcessor)
	p.maxSteps = maxSteps
	s.submitted.Add(1)
	s.queue <- p
}

func (s *RealSchedulerV6) Stop() {
	close(s.done)
	s.wg.Wait()
}

func BenchmarkRealSchedulerV6_10Steps(b *testing.B) {
	s := NewRealSchedulerV6(runtime.GOMAXPROCS(0))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Submit(10)
	}

	for s.completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
	b.StopTimer()
	s.Stop()
}

func BenchmarkRealSchedulerV6_10StepsParallel(b *testing.B) {
	s := NewRealSchedulerV6(runtime.GOMAXPROCS(0))

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			s.Submit(10)
		}
	})

	for s.completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
	b.StopTimer()
	s.Stop()
}

// V7: Per-worker local queues with work stealing
// Reduces global queue contention
type RealSchedulerV7 struct {
	workers    []*workerV7
	done       chan struct{}
	wg         sync.WaitGroup
	submitted  atomic.Int64
	completed  atomic.Int64
	nextWorker atomic.Uint64
}

type workerV7 struct {
	id    int
	local chan *realProcessor
	sched *RealSchedulerV7
}

func NewRealSchedulerV7(numWorkers int) *RealSchedulerV7 {
	s := &RealSchedulerV7{
		workers: make([]*workerV7, numWorkers),
		done:    make(chan struct{}),
	}

	for i := 0; i < numWorkers; i++ {
		s.workers[i] = &workerV7{
			id:    i,
			local: make(chan *realProcessor, 4096),
			sched: s,
		}
		s.wg.Add(1)
		go s.workers[i].run()
	}

	return s
}

func (w *workerV7) run() {
	defer w.sched.wg.Done()
	for {
		select {
		case p := <-w.local:
			p.steps++
			if p.steps >= p.maxSteps {
				p.completed = true
				w.sched.completed.Add(1)
				p.steps = 0
				p.maxSteps = 0
				p.completed = false
				realProcPool.Put(p)
			} else {
				// Re-queue to LOCAL queue - avoids global contention
				select {
				case w.local <- p:
				default:
					// Local full, try another worker
					other := (w.id + 1) % len(w.sched.workers)
					w.sched.workers[other].local <- p
				}
			}
		case <-w.sched.done:
			return
		}
	}
}

func (s *RealSchedulerV7) Submit(maxSteps int) {
	p := realProcPool.Get().(*realProcessor)
	p.maxSteps = maxSteps
	s.submitted.Add(1)
	// Round-robin to workers
	worker := int(s.nextWorker.Add(1)) % len(s.workers)
	s.workers[worker].local <- p
}

func (s *RealSchedulerV7) Stop() {
	close(s.done)
	s.wg.Wait()
}

func BenchmarkRealSchedulerV7_10Steps(b *testing.B) {
	s := NewRealSchedulerV7(runtime.GOMAXPROCS(0))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Submit(10)
	}

	for s.completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
	b.StopTimer()
	s.Stop()
}

func BenchmarkRealSchedulerV7_10StepsParallel(b *testing.B) {
	s := NewRealSchedulerV7(runtime.GOMAXPROCS(0))

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			s.Submit(10)
		}
	})

	for s.completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
	b.StopTimer()
	s.Stop()
}

// V8: LIFO re-queue - completed step goes to front of local queue
// Uses slice as stack instead of channel for local work
type RealSchedulerV8 struct {
	workers    []*workerV8
	global     chan *realProcessor // Overflow/new work
	done       chan struct{}
	wg         sync.WaitGroup
	submitted  atomic.Int64
	completed  atomic.Int64
	nextWorker atomic.Uint64
}

type workerV8 struct {
	id     int
	local  []*realProcessor // Stack (LIFO)
	mu     sync.Mutex
	sched  *RealSchedulerV8
	notify chan struct{}
}

func NewRealSchedulerV8(numWorkers int) *RealSchedulerV8 {
	s := &RealSchedulerV8{
		workers: make([]*workerV8, numWorkers),
		global:  make(chan *realProcessor, 65536),
		done:    make(chan struct{}),
	}

	for i := 0; i < numWorkers; i++ {
		s.workers[i] = &workerV8{
			id:     i,
			local:  make([]*realProcessor, 0, 256),
			sched:  s,
			notify: make(chan struct{}, 1),
		}
		s.wg.Add(1)
		go s.workers[i].run()
	}

	return s
}

func (w *workerV8) run() {
	defer w.sched.wg.Done()
	for {
		// Try local first (LIFO)
		w.mu.Lock()
		var p *realProcessor
		if len(w.local) > 0 {
			p = w.local[len(w.local)-1]
			w.local = w.local[:len(w.local)-1]
		}
		w.mu.Unlock()

		if p == nil {
			// Try global
			select {
			case p = <-w.sched.global:
			case <-w.sched.done:
				return
			}
		}

		// Execute step
		p.steps++
		if p.steps >= p.maxSteps {
			w.sched.completed.Add(1)
			p.steps = 0
			p.maxSteps = 0
			p.completed = false
			realProcPool.Put(p)
		} else {
			// LIFO re-queue to local
			w.mu.Lock()
			w.local = append(w.local, p)
			w.mu.Unlock()
		}
	}
}

func (s *RealSchedulerV8) Submit(maxSteps int) {
	p := realProcPool.Get().(*realProcessor)
	p.maxSteps = maxSteps
	s.submitted.Add(1)
	s.global <- p
}

func (s *RealSchedulerV8) Stop() {
	close(s.done)
	s.wg.Wait()
}

func BenchmarkRealSchedulerV8_10Steps(b *testing.B) {
	s := NewRealSchedulerV8(runtime.GOMAXPROCS(0))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Submit(10)
	}

	for s.completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
	b.StopTimer()
	s.Stop()
}

func BenchmarkRealSchedulerV8_10StepsParallel(b *testing.B) {
	s := NewRealSchedulerV8(runtime.GOMAXPROCS(0))

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			s.Submit(10)
		}
	})

	for s.completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
	b.StopTimer()
	s.Stop()
}

// V9: Hybrid - LIFO local + channel for stealing
// Most similar to current implementation but without sync.Map
type RealSchedulerV9 struct {
	workers    []*workerV9
	done       chan struct{}
	wg         sync.WaitGroup
	completed  atomic.Int64
	nextWorker atomic.Uint64
}

type workerV9 struct {
	id    int
	lifo  atomic.Pointer[realProcessor] // Hot slot
	local chan *realProcessor           // Local queue
	sched *RealSchedulerV9
}

func NewRealSchedulerV9(numWorkers int) *RealSchedulerV9 {
	s := &RealSchedulerV9{
		workers: make([]*workerV9, numWorkers),
		done:    make(chan struct{}),
	}

	for i := 0; i < numWorkers; i++ {
		s.workers[i] = &workerV9{
			id:    i,
			local: make(chan *realProcessor, 256),
			sched: s,
		}
		s.wg.Add(1)
		go s.workers[i].run()
	}

	return s
}

func (w *workerV9) run() {
	defer w.sched.wg.Done()
	for {
		// 1. Check LIFO slot
		p := w.lifo.Swap(nil)

		// 2. Check local queue
		if p == nil {
			select {
			case p = <-w.local:
			case <-w.sched.done:
				return
			default:
			}
		}

		// 3. Steal from other worker
		if p == nil {
			for i := 0; i < len(w.sched.workers); i++ {
				other := (w.id + i + 1) % len(w.sched.workers)
				select {
				case p = <-w.sched.workers[other].local:
					break
				default:
				}
			}
		}

		// 4. Block on local if nothing found
		if p == nil {
			select {
			case p = <-w.local:
			case <-w.sched.done:
				return
			}
		}

		// Execute
		p.steps++
		if p.steps >= p.maxSteps {
			w.sched.completed.Add(1)
			p.steps = 0
			p.maxSteps = 0
			realProcPool.Put(p)
		} else {
			// Re-queue to LIFO slot, overflow to local
			if old := w.lifo.Swap(p); old != nil {
				w.local <- old
			}
		}
	}
}

func (s *RealSchedulerV9) Submit(maxSteps int) {
	p := realProcPool.Get().(*realProcessor)
	p.maxSteps = maxSteps
	worker := int(s.nextWorker.Add(1)) % len(s.workers)
	s.workers[worker].local <- p
}

func (s *RealSchedulerV9) Stop() {
	close(s.done)
	s.wg.Wait()
}

func BenchmarkRealSchedulerV9_10Steps(b *testing.B) {
	s := NewRealSchedulerV9(runtime.GOMAXPROCS(0))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Submit(10)
	}

	for s.completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
	b.StopTimer()
	s.Stop()
}

func BenchmarkRealSchedulerV9_10StepsParallel(b *testing.B) {
	s := NewRealSchedulerV9(runtime.GOMAXPROCS(0))

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			s.Submit(10)
		}
	})

	for s.completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
	b.StopTimer()
	s.Stop()
}
