package testbed

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

// Optimized scheduler v1: Channel-based, no sync.Map, no CAS detection
// Target: match MinimalScheduler baseline of ~124ns

type processor struct {
	id        uint64
	steps     int
	yieldData any
	done      bool
}

var procPool = sync.Pool{
	New: func() any { return &processor{} },
}

func acquireProc() *processor {
	return procPool.Get().(*processor)
}

func releaseProc(p *processor) {
	p.id = 0
	p.steps = 0
	p.yieldData = nil
	p.done = false
	procPool.Put(p)
}

// Handler simulates yield handling
type testHandler func(p *processor)

// OptSchedulerV1: Single global channel, no byPID tracking
type OptSchedulerV1 struct {
	queue     chan *processor
	done      chan struct{}
	wg        sync.WaitGroup
	completed atomic.Int64
	handler   testHandler
}

func NewOptSchedulerV1(workers int, handler testHandler) *OptSchedulerV1 {
	s := &OptSchedulerV1{
		queue:   make(chan *processor, 65536),
		done:    make(chan struct{}),
		handler: handler,
	}

	for i := 0; i < workers; i++ {
		s.wg.Add(1)
		go s.worker()
	}

	return s
}

func (s *OptSchedulerV1) worker() {
	defer s.wg.Done()
	for {
		select {
		case p := <-s.queue:
			// Simulate 10 yield cycles with handler
			for i := 0; i < 10; i++ {
				s.handler(p)
				p.steps++
			}
			s.completed.Add(1)
			releaseProc(p)
		case <-s.done:
			return
		}
	}
}

func (s *OptSchedulerV1) Submit() {
	p := acquireProc()
	s.queue <- p
}

func (s *OptSchedulerV1) Stop() {
	close(s.done)
	s.wg.Wait()
}

func BenchmarkOptSchedulerV1(b *testing.B) {
	handler := func(p *processor) { p.yieldData = nil }
	s := NewOptSchedulerV1(runtime.GOMAXPROCS(0), handler)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Submit()
	}

	for s.completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
	b.StopTimer()
	s.Stop()
}

func BenchmarkOptSchedulerV1Parallel(b *testing.B) {
	handler := func(p *processor) { p.yieldData = nil }
	s := NewOptSchedulerV1(runtime.GOMAXPROCS(0), handler)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			s.Submit()
		}
	})

	for s.completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
	b.StopTimer()
	s.Stop()
}

// OptSchedulerV2: Per-worker channels (sharded input)
type OptSchedulerV2 struct {
	queues    []chan *processor
	done      chan struct{}
	wg        sync.WaitGroup
	completed atomic.Int64
	handler   testHandler
	counter   atomic.Uint64
	numShards int
}

func NewOptSchedulerV2(workers int, handler testHandler) *OptSchedulerV2 {
	s := &OptSchedulerV2{
		queues:    make([]chan *processor, workers),
		done:      make(chan struct{}),
		handler:   handler,
		numShards: workers,
	}

	for i := 0; i < workers; i++ {
		s.queues[i] = make(chan *processor, 4096)
		s.wg.Add(1)
		go s.worker(i)
	}

	return s
}

func (s *OptSchedulerV2) worker(id int) {
	defer s.wg.Done()
	q := s.queues[id]
	for {
		select {
		case p := <-q:
			for i := 0; i < 10; i++ {
				s.handler(p)
				p.steps++
			}
			s.completed.Add(1)
			releaseProc(p)
		case <-s.done:
			return
		}
	}
}

func (s *OptSchedulerV2) Submit() {
	p := acquireProc()
	// Round-robin to shards
	shard := int(s.counter.Add(1)) % s.numShards
	s.queues[shard] <- p
}

func (s *OptSchedulerV2) Stop() {
	close(s.done)
	s.wg.Wait()
}

func BenchmarkOptSchedulerV2(b *testing.B) {
	handler := func(p *processor) { p.yieldData = nil }
	s := NewOptSchedulerV2(runtime.GOMAXPROCS(0), handler)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Submit()
	}

	for s.completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
	b.StopTimer()
	s.Stop()
}

func BenchmarkOptSchedulerV2Parallel(b *testing.B) {
	handler := func(p *processor) { p.yieldData = nil }
	s := NewOptSchedulerV2(runtime.GOMAXPROCS(0), handler)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			s.Submit()
		}
	})

	for s.completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
	b.StopTimer()
	s.Stop()
}

// OptSchedulerV3: No channel - direct goroutine per process
// Tests if channel is the bottleneck
type OptSchedulerV3 struct {
	completed atomic.Int64
	handler   testHandler
	sem       chan struct{} // Limit concurrent goroutines
}

func NewOptSchedulerV3(maxConcurrent int, handler testHandler) *OptSchedulerV3 {
	return &OptSchedulerV3{
		handler: handler,
		sem:     make(chan struct{}, maxConcurrent),
	}
}

func (s *OptSchedulerV3) Submit() {
	s.sem <- struct{}{} // Acquire
	go func() {
		p := acquireProc()
		for i := 0; i < 10; i++ {
			s.handler(p)
			p.steps++
		}
		releaseProc(p)
		s.completed.Add(1)
		<-s.sem // Release
	}()
}

func BenchmarkOptSchedulerV3(b *testing.B) {
	handler := func(p *processor) { p.yieldData = nil }
	s := NewOptSchedulerV3(runtime.GOMAXPROCS(0)*4, handler)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Submit()
	}

	for s.completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
}

func BenchmarkOptSchedulerV3Parallel(b *testing.B) {
	handler := func(p *processor) { p.yieldData = nil }
	s := NewOptSchedulerV3(runtime.GOMAXPROCS(0)*16, handler)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			s.Submit()
		}
	})

	for s.completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
}

// OptSchedulerV4: Batched submission - submit N processors at once
type OptSchedulerV4 struct {
	queue     chan []*processor
	done      chan struct{}
	wg        sync.WaitGroup
	completed atomic.Int64
	handler   testHandler
}

func NewOptSchedulerV4(workers int, handler testHandler) *OptSchedulerV4 {
	s := &OptSchedulerV4{
		queue:   make(chan []*processor, 4096),
		done:    make(chan struct{}),
		handler: handler,
	}

	for i := 0; i < workers; i++ {
		s.wg.Add(1)
		go s.worker()
	}

	return s
}

func (s *OptSchedulerV4) worker() {
	defer s.wg.Done()
	for {
		select {
		case batch := <-s.queue:
			for _, p := range batch {
				for i := 0; i < 10; i++ {
					s.handler(p)
					p.steps++
				}
				releaseProc(p)
			}
			s.completed.Add(int64(len(batch)))
		case <-s.done:
			return
		}
	}
}

func (s *OptSchedulerV4) SubmitBatch(n int) {
	batch := make([]*processor, n)
	for i := range batch {
		batch[i] = acquireProc()
	}
	s.queue <- batch
}

func (s *OptSchedulerV4) Stop() {
	close(s.done)
	s.wg.Wait()
}

func BenchmarkOptSchedulerV4Batch8(b *testing.B) {
	handler := func(p *processor) { p.yieldData = nil }
	s := NewOptSchedulerV4(runtime.GOMAXPROCS(0), handler)
	batches := b.N / 8
	if batches == 0 {
		batches = 1
	}

	b.ResetTimer()
	for i := 0; i < batches; i++ {
		s.SubmitBatch(8)
	}

	target := int64(batches * 8)
	for s.completed.Load() < target {
		runtime.Gosched()
	}
	b.StopTimer()
	s.Stop()
}

// OptSchedulerV5: Lock-free MPSC queue attempt
// Using atomic ring buffer

type lockFreeQueue struct {
	buffer [65536]*processor
	head   atomic.Uint64
	tail   atomic.Uint64
	mask   uint64
}

func newLockFreeQueue() *lockFreeQueue {
	return &lockFreeQueue{mask: 65535}
}

func (q *lockFreeQueue) push(p *processor) bool {
	for {
		tail := q.tail.Load()
		head := q.head.Load()
		if tail-head >= 65536 {
			return false // Full
		}
		if q.tail.CompareAndSwap(tail, tail+1) {
			q.buffer[tail&q.mask] = p
			return true
		}
	}
}

func (q *lockFreeQueue) pop() *processor {
	for {
		head := q.head.Load()
		tail := q.tail.Load()
		if head >= tail {
			return nil // Empty
		}
		p := q.buffer[head&q.mask]
		if p == nil {
			runtime.Gosched()
			continue
		}
		if q.head.CompareAndSwap(head, head+1) {
			q.buffer[head&q.mask] = nil
			return p
		}
	}
}

type OptSchedulerV5 struct {
	queue     *lockFreeQueue
	done      atomic.Bool
	wg        sync.WaitGroup
	completed atomic.Int64
	handler   testHandler
}

func NewOptSchedulerV5(workers int, handler testHandler) *OptSchedulerV5 {
	s := &OptSchedulerV5{
		queue:   newLockFreeQueue(),
		handler: handler,
	}

	for i := 0; i < workers; i++ {
		s.wg.Add(1)
		go s.worker()
	}

	return s
}

func (s *OptSchedulerV5) worker() {
	defer s.wg.Done()
	for !s.done.Load() {
		p := s.queue.pop()
		if p == nil {
			runtime.Gosched()
			continue
		}
		for i := 0; i < 10; i++ {
			s.handler(p)
			p.steps++
		}
		s.completed.Add(1)
		releaseProc(p)
	}
}

func (s *OptSchedulerV5) Submit() {
	p := acquireProc()
	for !s.queue.push(p) {
		runtime.Gosched()
	}
}

func (s *OptSchedulerV5) Stop() {
	s.done.Store(true)
	s.wg.Wait()
}

func BenchmarkOptSchedulerV5(b *testing.B) {
	handler := func(p *processor) { p.yieldData = nil }
	s := NewOptSchedulerV5(runtime.GOMAXPROCS(0), handler)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Submit()
	}

	for s.completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
	b.StopTimer()
	s.Stop()
}

func BenchmarkOptSchedulerV5Parallel(b *testing.B) {
	handler := func(p *processor) { p.yieldData = nil }
	s := NewOptSchedulerV5(runtime.GOMAXPROCS(0), handler)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			s.Submit()
		}
	})

	for s.completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
	b.StopTimer()
	s.Stop()
}
