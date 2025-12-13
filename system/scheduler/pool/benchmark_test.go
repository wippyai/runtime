package pool

import (
	"testing"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

func BenchmarkInlineCall(b *testing.B) {
	pool, _ := NewInline(newMockFactory(0), &mockDispatcher{})
	defer pool.Stop()
	pool.Start()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = pool.Call(testContext(), "test", nil)
	}
}

func BenchmarkStaticCall(b *testing.B) {
	pool, _ := NewStatic(newMockFactory(0), &mockDispatcher{}, Config{Workers: 4})
	defer pool.Stop()
	pool.Start()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = pool.Call(testContext(), "test", nil)
		}
	})
}

func BenchmarkLazyCall(b *testing.B) {
	pool, _ := NewLazy(newMockFactory(0), &mockDispatcher{}, LazyConfig{MaxWorkers: 4})
	defer pool.Stop()
	pool.Start()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = pool.Call(testContext(), "test", nil)
		}
	})
}

func BenchmarkExecutorSend(b *testing.B) {
	executor := NewExecutor(&mockDispatcher{})
	executor.active.Store(true)
	executor.queue.Reset()
	executor.gen.Store(executor.queue.Generation())
	pkg := &relay.Package{Target: pid.PID{UniqID: "1"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = executor.Send(pkg)
	}
}

func BenchmarkStaticSendLookup(b *testing.B) {
	pool, _ := NewStatic(newMockFactory(0), &mockDispatcher{}, Config{Workers: 4})
	defer pool.Stop()
	pool.Start()

	// Register a fake executor in active map
	executor := NewExecutor(&mockDispatcher{})
	executor.active.Store(true)
	executor.queue.Reset()
	executor.gen.Store(executor.queue.Generation())
	pool.active.Store("bench-1", executor)
	pkg := &relay.Package{Target: pid.PID{UniqID: "bench-1"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pool.Send(pkg)
	}
}

func BenchmarkLazySendLookup(b *testing.B) {
	pool, _ := NewLazy(newMockFactory(0), &mockDispatcher{}, LazyConfig{MaxWorkers: 4})
	defer pool.Stop()
	pool.Start()

	// Register a fake executor in active map
	executor := NewExecutor(&mockDispatcher{})
	executor.active.Store(true)
	executor.queue.Reset()
	executor.gen.Store(executor.queue.Generation())
	pool.activeExec.Store("bench-1", executor)
	pkg := &relay.Package{Target: pid.PID{UniqID: "bench-1"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pool.Send(pkg)
	}
}

func BenchmarkInlineSendLookup(b *testing.B) {
	pool, _ := NewInline(newMockFactory(0), &mockDispatcher{})
	defer pool.Stop()
	pool.Start()

	// Register a fake executor in active map
	executor := NewExecutor(&mockDispatcher{})
	executor.active.Store(true)
	executor.queue.Reset()
	executor.gen.Store(executor.queue.Generation())
	pool.active.Store("bench-1", executor)
	pkg := &relay.Package{Target: pid.PID{UniqID: "bench-1"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pool.Send(pkg)
	}
}
