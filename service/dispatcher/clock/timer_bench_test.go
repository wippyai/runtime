package clock

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
)

// Benchmarks comparing standard TimerRegistry vs WheelTimerRegistry

func BenchmarkWheelTimerStart(b *testing.B) {
	r := NewWheelTimerRegistry()
	defer r.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		id := r.Start(time.Hour)
		r.Stop(id)
	}
}

func BenchmarkWheelTimerStartParallel(b *testing.B) {
	r := NewWheelTimerRegistry()
	defer r.Close()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			id := r.Start(time.Hour)
			r.Stop(id)
		}
	})
}

func TestWheelTimerMemoryProfile(t *testing.T) {
	for _, count := range []int{1000, 10000, 100000, 500000} {
		t.Run(itoa(count), func(t *testing.T) {
			runtime.GC()
			var m1 runtime.MemStats
			runtime.ReadMemStats(&m1)

			r := NewWheelTimerRegistry()

			start := time.Now()
			ids := make([]uint64, count)
			for i := 0; i < count; i++ {
				ids[i] = r.Start(time.Hour)
			}
			createTime := time.Since(start)

			runtime.GC()
			var m2 runtime.MemStats
			runtime.ReadMemStats(&m2)

			var heapUsed uint64
			if m2.HeapAlloc > m1.HeapAlloc {
				heapUsed = m2.HeapAlloc - m1.HeapAlloc
			}
			perTimer := heapUsed / uint64(count)

			t.Logf("WHEEL %d timers: create=%v, heap=%dMB, per-timer=%d bytes",
				count, createTime, heapUsed/1024/1024, perTimer)

			// Cleanup
			start = time.Now()
			r.Close()
			cleanupTime := time.Since(start)
			t.Logf("cleanup=%v", cleanupTime)
		})
	}
}

func TestWheelTimerConcurrentScalability(t *testing.T) {
	const numTimers = 100000
	const numGoroutines = 100

	r := NewWheelTimerRegistry()
	defer r.Close()

	var wg sync.WaitGroup
	timersPerGoroutine := numTimers / numGoroutines

	start := time.Now()

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids := make([]uint64, timersPerGoroutine)
			for i := 0; i < timersPerGoroutine; i++ {
				ids[i] = r.Start(time.Hour)
			}
			for _, id := range ids {
				r.Stop(id)
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	t.Logf("WHEEL %d timers across %d goroutines: %v (%.0f timers/sec)",
		numTimers, numGoroutines, elapsed, float64(numTimers*2)/elapsed.Seconds())
}

func BenchmarkManyWheelTimersCreateOnly(b *testing.B) {
	for _, count := range []int{1000, 10000, 100000, 500000} {
		b.Run(itoa(count), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				r := NewWheelTimerRegistry()

				for j := 0; j < count; j++ {
					r.Start(time.Hour)
				}

				r.Close()
			}
		})
	}
}

func BenchmarkTimerStart(b *testing.B) {
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer fc.Close()

	registry := GetOrCreateTimerRegistry(ctx)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		id := registry.Start(time.Hour)
		registry.Stop(id)
	}
}

func BenchmarkTimerStartStop(b *testing.B) {
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer fc.Close()

	registry := GetOrCreateTimerRegistry(ctx)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		id := registry.Start(time.Hour)
		registry.Stop(id)
	}
}

func BenchmarkTimerStartParallel(b *testing.B) {
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer fc.Close()

	registry := GetOrCreateTimerRegistry(ctx)

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			id := registry.Start(time.Hour)
			registry.Stop(id)
		}
	})
}

func BenchmarkManyTimersCreate(b *testing.B) {
	for _, count := range []int{1000, 10000, 100000} {
		b.Run(itoa(count), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				ctx, fc := ctxapi.OpenFrameContext(context.Background())
				registry := GetOrCreateTimerRegistry(ctx)

				ids := make([]uint64, count)
				for j := 0; j < count; j++ {
					ids[j] = registry.Start(time.Hour)
				}

				for _, id := range ids {
					registry.Stop(id)
				}

				fc.Close()
			}
		})
	}
}

func BenchmarkManyTimersCreateOnly(b *testing.B) {
	for _, count := range []int{1000, 10000, 100000, 500000} {
		b.Run(itoa(count), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				ctx, fc := ctxapi.OpenFrameContext(context.Background())
				registry := GetOrCreateTimerRegistry(ctx)

				for j := 0; j < count; j++ {
					registry.Start(time.Hour)
				}

				fc.Close()
			}
		})
	}
}

func itoa(n int) string {
	switch n {
	case 1000:
		return "1k"
	case 10000:
		return "10k"
	case 100000:
		return "100k"
	case 500000:
		return "500k"
	default:
		return "unknown"
	}
}

func TestTimerMemoryProfile(t *testing.T) {
	for _, count := range []int{1000, 10000, 100000, 500000} {
		t.Run(itoa(count), func(t *testing.T) {
			runtime.GC()
			var m1 runtime.MemStats
			runtime.ReadMemStats(&m1)

			ctx, fc := ctxapi.OpenFrameContext(context.Background())
			registry := GetOrCreateTimerRegistry(ctx)

			start := time.Now()
			for i := 0; i < count; i++ {
				registry.Start(time.Hour)
			}
			createTime := time.Since(start)

			runtime.GC()
			var m2 runtime.MemStats
			runtime.ReadMemStats(&m2)

			var heapUsed uint64
			if m2.HeapAlloc > m1.HeapAlloc {
				heapUsed = m2.HeapAlloc - m1.HeapAlloc
			}
			perTimer := heapUsed / uint64(count)

			t.Logf("%d timers: create=%v, heap=%dMB, per-timer=%d bytes",
				count, createTime, heapUsed/1024/1024, perTimer)

			// Cleanup
			start = time.Now()
			fc.Close()
			cleanupTime := time.Since(start)
			t.Logf("cleanup=%v", cleanupTime)
		})
	}
}

func TestTimerConcurrentScalability(t *testing.T) {
	const numTimers = 100000
	const numGoroutines = 100

	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer fc.Close()

	registry := GetOrCreateTimerRegistry(ctx)

	var wg sync.WaitGroup
	timersPerGoroutine := numTimers / numGoroutines

	start := time.Now()

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids := make([]uint64, timersPerGoroutine)
			for i := 0; i < timersPerGoroutine; i++ {
				ids[i] = registry.Start(time.Hour)
			}
			for _, id := range ids {
				registry.Stop(id)
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	t.Logf("%d timers across %d goroutines: %v (%.0f timers/sec)",
		numTimers, numGoroutines, elapsed, float64(numTimers*2)/elapsed.Seconds())
}
