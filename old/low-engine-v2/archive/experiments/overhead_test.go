package testbed

import (
	"sync"
	"sync/atomic"
	"testing"
)

// Isolate individual overhead sources

// 1. sync.Map overhead
func BenchmarkSyncMapStore(b *testing.B) {
	var m sync.Map
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			m.Store(i, i)
			i++
		}
	})
}

func BenchmarkSyncMapLoadStore(b *testing.B) {
	var m sync.Map
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			m.Store(i, i)
			m.Load(i)
			m.Delete(i)
			i++
		}
	})
}

// 2. String key overhead (pid.String())
func BenchmarkSyncMapStringKey(b *testing.B) {
	var m sync.Map
	keys := make([]string, b.N)
	for i := range keys {
		keys[i] = "test-" + string(rune(i%256))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Store(keys[i%len(keys)], i)
		m.Delete(keys[i%len(keys)])
	}
}

// 3. sync.Cond overhead
func BenchmarkSyncCondSignal(b *testing.B) {
	var mu sync.Mutex
	cond := sync.NewCond(&mu)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cond.Signal()
		}
	})
}

func BenchmarkSyncCondWaitSignal(b *testing.B) {
	b.Skip("deadlocks in benchmark harness")
}

// 4. Mutex-protected queue vs channel
type mutexQueue struct {
	mu    sync.Mutex
	items []int
}

func (q *mutexQueue) push(v int) {
	q.mu.Lock()
	q.items = append(q.items, v)
	q.mu.Unlock()
}

func (q *mutexQueue) pop() (int, bool) {
	q.mu.Lock()
	if len(q.items) == 0 {
		q.mu.Unlock()
		return 0, false
	}
	v := q.items[0]
	q.items = q.items[1:]
	q.mu.Unlock()
	return v, true
}

func BenchmarkMutexQueue(b *testing.B) {
	q := &mutexQueue{items: make([]int, 0, 1024)}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			q.push(1)
			q.pop()
		}
	})
}

func BenchmarkChannelQueue(b *testing.B) {
	ch := make(chan int, 1024)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ch <- 1
			<-ch
		}
	})
}

// 5. Atomic CAS overhead
func BenchmarkAtomicCAS(b *testing.B) {
	var v atomic.Int32
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for !v.CompareAndSwap(0, 1) {
			}
			v.Store(0)
		}
	})
}

func BenchmarkAtomicStore(b *testing.B) {
	var v atomic.Int32
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			v.Store(1)
			v.Store(0)
		}
	})
}

// 6. Interface dispatch overhead
type handler interface {
	handle(v int) int
}

type concreteHandler struct{}

func (h *concreteHandler) handle(v int) int { return v + 1 }

func BenchmarkInterfaceCall(b *testing.B) {
	var h handler = &concreteHandler{}
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			h.handle(1)
		}
	})
}

func BenchmarkDirectCall(b *testing.B) {
	h := &concreteHandler{}
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			h.handle(1)
		}
	})
}

// 7. Function pointer vs interface
type handlerFunc func(int) int

func BenchmarkFuncCall(b *testing.B) {
	f := handlerFunc(func(v int) int { return v + 1 })
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			f(1)
		}
	})
}

// 8. Allocation comparison
func BenchmarkHeapAlloc(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			p := new(minimalProcessor)
			_ = p
		}
	})
}

func BenchmarkStackAlloc(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			var p minimalProcessor
			_ = p
		}
	})
}

// 9. Pool with reset vs new allocation
func BenchmarkPoolWithReset(b *testing.B) {
	pool := sync.Pool{
		New: func() any { return &minimalProcessor{} },
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			p := pool.Get().(*minimalProcessor)
			p.steps = 0
			p.done = false
			pool.Put(p)
		}
	})
}
