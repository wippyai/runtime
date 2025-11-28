package testbed

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	_ "unsafe"
)

// Experiment 1: What's the absolute minimum overhead?
// Just a counter to establish baseline Go overhead.

func BenchmarkRawCounter(b *testing.B) {
	var counter atomic.Int64
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			counter.Add(1)
		}
	})
}

// Experiment 2: Channel send/receive overhead
func BenchmarkChannelRoundtrip(b *testing.B) {
	ch := make(chan struct{}, 1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch <- struct{}{}
		<-ch
	}
}

func BenchmarkChannelParallel(b *testing.B) {
	ch := make(chan int, 1024)
	done := make(chan struct{})

	// Consumer
	go func() {
		for range ch {
		}
		close(done)
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch <- i
	}
	close(ch)
	<-done
}

// Experiment 3: Minimal work-stealing deque throughput
// Just push/pop without any process overhead

type minimalTask struct {
	id int
}

var taskPool = sync.Pool{
	New: func() any { return &minimalTask{} },
}

func BenchmarkPoolGetPut(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			t := taskPool.Get().(*minimalTask)
			t.id = 1
			taskPool.Put(t)
		}
	})
}

// Experiment 4: Minimal scheduler - just queue + workers
// No handlers, no yields, just measure scheduling overhead

type minimalProcessor struct {
	steps int
	done  bool
}

type minimalScheduler struct {
	queue     chan *minimalProcessor
	done      chan struct{}
	wg        sync.WaitGroup
	completed atomic.Int64
}

func newMinimalScheduler(workers int) *minimalScheduler {
	s := &minimalScheduler{
		queue: make(chan *minimalProcessor, 65536),
		done:  make(chan struct{}),
	}

	for i := 0; i < workers; i++ {
		s.wg.Add(1)
		go s.worker()
	}

	return s
}

func (s *minimalScheduler) worker() {
	defer s.wg.Done()
	for {
		select {
		case p := <-s.queue:
			// Simulate 10 yield cycles
			for i := 0; i < 10; i++ {
				p.steps++
			}
			p.done = true
			s.completed.Add(1)
		case <-s.done:
			return
		}
	}
}

func (s *minimalScheduler) submit(p *minimalProcessor) {
	s.queue <- p
}

func (s *minimalScheduler) stop() {
	close(s.done)
	s.wg.Wait()
}

func BenchmarkMinimalScheduler(b *testing.B) {
	s := newMinimalScheduler(runtime.GOMAXPROCS(0))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := &minimalProcessor{}
		s.submit(p)
	}

	// Wait for completion
	for s.completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
	b.StopTimer()
	s.stop()
}

func BenchmarkMinimalSchedulerParallel(b *testing.B) {
	s := newMinimalScheduler(runtime.GOMAXPROCS(0))

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			p := &minimalProcessor{}
			s.submit(p)
		}
	})

	// Wait for completion
	for s.completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
	b.StopTimer()
	s.stop()
}

// Experiment 5: Sharded scheduler - each worker has own queue
// Producers hash to specific worker queues

type shardedScheduler struct {
	queues    []chan *minimalProcessor
	done      chan struct{}
	wg        sync.WaitGroup
	completed atomic.Int64
	numShards int
}

func newShardedScheduler(workers int) *shardedScheduler {
	s := &shardedScheduler{
		queues:    make([]chan *minimalProcessor, workers),
		done:      make(chan struct{}),
		numShards: workers,
	}

	for i := 0; i < workers; i++ {
		s.queues[i] = make(chan *minimalProcessor, 4096)
		s.wg.Add(1)
		go s.worker(i)
	}

	return s
}

func (s *shardedScheduler) worker(id int) {
	defer s.wg.Done()
	q := s.queues[id]
	for {
		select {
		case p := <-q:
			for i := 0; i < 10; i++ {
				p.steps++
			}
			p.done = true
			s.completed.Add(1)
		case <-s.done:
			return
		}
	}
}

func (s *shardedScheduler) submit(p *minimalProcessor, hint int) {
	s.queues[hint%s.numShards] <- p
}

func (s *shardedScheduler) stop() {
	close(s.done)
	s.wg.Wait()
}

func BenchmarkShardedScheduler(b *testing.B) {
	s := newShardedScheduler(runtime.GOMAXPROCS(0))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := &minimalProcessor{}
		s.submit(p, i)
	}

	for s.completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
	b.StopTimer()
	s.stop()
}

func BenchmarkShardedSchedulerParallel(b *testing.B) {
	s := newShardedScheduler(runtime.GOMAXPROCS(0))
	var counter atomic.Int64

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			p := &minimalProcessor{}
			hint := int(counter.Add(1))
			s.submit(p, hint)
		}
	})

	for s.completed.Load() < int64(b.N) {
		runtime.Gosched()
	}
	b.StopTimer()
	s.stop()
}

// Experiment 6: Direct function call overhead (no channels)
// Inline execution - theoretical maximum

func BenchmarkDirectExecution(b *testing.B) {
	var completed atomic.Int64

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			p := &minimalProcessor{}
			for i := 0; i < 10; i++ {
				p.steps++
			}
			p.done = true
			completed.Add(1)
		}
	})
}
