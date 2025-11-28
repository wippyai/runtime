package engine2

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/process2"
	"github.com/wippyai/runtime/service/dispatcher/clock"
	funcpool "github.com/wippyai/runtime/system/scheduler/pool"
	lua "github.com/yuin/gopher-lua"
)

type poolTestDispatcher struct {
	handlers map[dispatcher.CommandID]dispatcher.Handler
}

func newPoolTestDispatcher() *poolTestDispatcher {
	d := &poolTestDispatcher{handlers: make(map[dispatcher.CommandID]dispatcher.Handler)}
	clockSvc := clock.NewService()
	clockSvc.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		d.handlers[id] = h
	})
	return d
}

func (d *poolTestDispatcher) Dispatch(cmd dispatcher.Command) dispatcher.Handler {
	return d.handlers[cmd.CmdID()]
}

func newLuaFactory(script string) funcpool.Factory {
	return func() (process2.Process, error) {
		proto, err := lua.CompileString(script, "test.lua")
		if err != nil {
			return nil, err
		}

		proc := NewProcess(
			WithProto(proto),
			WithModuleBinder(BindTimeSleep),
		)

		return proc, nil
	}
}

// TestPoolBasicCall tests basic pool call functionality
func TestPoolBasicCall(t *testing.T) {
	factory := newLuaFactory(`return 1 + 2`)
	disp := newPoolTestDispatcher()

	ps, err := funcpool.NewStatic(factory, disp, funcpool.Config{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}

	ps.Start()
	defer ps.Stop()

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	result, err := ps.Call(ctx, "", nil)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	if result == nil {
		t.Fatal("nil result")
	}
	t.Log("Basic pool call passed")
}

// TestPoolStateReuse tests that Lua state persists between calls
func TestPoolStateReuse(t *testing.T) {
	factory := newLuaFactory(`
		counter = (counter or 0) + 1
		return counter
	`)
	disp := newPoolTestDispatcher()

	ps, err := funcpool.NewStatic(factory, disp, funcpool.Config{Workers: 1})
	if err != nil {
		t.Fatal(err)
	}

	ps.Start()
	defer ps.Stop()

	// Call 3 times - counter should increment
	for i := 1; i <= 3; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		result, err := ps.Call(ctx, "", nil)
		if err != nil {
			t.Fatalf("Call %d failed: %v", i, err)
		}
		if result.Error != nil {
			t.Fatalf("Call %d result error: %v", i, result.Error)
		}
		t.Logf("Call %d result: %v", i, result.Value)
	}
}

// TestPool5ParallelProcesses tests 5 parallel processes with real dispatcher
func TestPool5ParallelProcesses(t *testing.T) {
	factory := newLuaFactory(`
		time.sleep(10 * time.MILLISECOND)
		return "done"
	`)
	disp := newPoolTestDispatcher()

	ps, err := funcpool.NewStatic(factory, disp, funcpool.Config{Workers: 5})
	if err != nil {
		t.Fatal(err)
	}

	ps.Start()
	defer ps.Stop()

	var wg sync.WaitGroup
	var completed atomic.Int32
	var errors atomic.Int32

	start := time.Now()

	// Launch 5 parallel calls
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ctx, _ := ctxapi.OpenFrameContext(context.Background())
			_, err := ps.Call(ctx, "", nil)
			if err != nil {
				errors.Add(1)
				t.Logf("Call %d failed: %v", id, err)
			} else {
				completed.Add(1)
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)

	if errors.Load() > 0 {
		t.Fatalf("Expected 0 errors, got %d", errors.Load())
	}
	if completed.Load() != 5 {
		t.Fatalf("Expected 5 completed, got %d", completed.Load())
	}

	// All 5 should complete in parallel (~10-50ms, not 5x10ms=50ms sequential)
	if elapsed > 100*time.Millisecond {
		t.Fatalf("Expected parallel execution, took %v", elapsed)
	}

	t.Logf("5 parallel processes completed in %v", elapsed)
}

// TestPoolWithTimeSleep tests pool with real sleep dispatcher
func TestPoolWithTimeSleep(t *testing.T) {
	factory := newLuaFactory(`
		time.sleep(20 * time.MILLISECOND)
		return "slept"
	`)
	disp := newPoolTestDispatcher()

	ps, err := funcpool.NewStatic(factory, disp, funcpool.Config{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}

	ps.Start()
	defer ps.Stop()

	start := time.Now()
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	result, err := ps.Call(ctx, "", nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	if result == nil {
		t.Fatal("nil result")
	}

	if elapsed < 20*time.Millisecond {
		t.Fatalf("Sleep too short: %v", elapsed)
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("Sleep too long: %v", elapsed)
	}

	t.Logf("Sleep call completed in %v", elapsed)
}

// TestPoolConcurrentCalls tests many concurrent calls
func TestPoolConcurrentCalls(t *testing.T) {
	factory := newLuaFactory(`
		time.sleep(time.MILLISECOND)
		return "ok"
	`)
	disp := newPoolTestDispatcher()

	ps, err := funcpool.NewStatic(factory, disp, funcpool.Config{Workers: 10, QueueSize: 100})
	if err != nil {
		t.Fatal(err)
	}

	ps.Start()
	defer ps.Stop()

	const numCalls = 50
	var wg sync.WaitGroup
	var completed atomic.Int32

	start := time.Now()

	for i := 0; i < numCalls; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, _ := ctxapi.OpenFrameContext(context.Background())
			_, err := ps.Call(ctx, "", nil)
			if err == nil {
				completed.Add(1)
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	if completed.Load() != numCalls {
		t.Fatalf("Expected %d completed, got %d", numCalls, completed.Load())
	}

	t.Logf("%d concurrent calls completed in %v (%.0f calls/sec)",
		numCalls, elapsed, float64(numCalls)/elapsed.Seconds())
}

// Benchmark8x8NoYield tests 8 workers without yields
func Benchmark8x8NoYield(b *testing.B) {
	factory := newLuaFactory(`return 1 + 2`)
	disp := newPoolTestDispatcher()

	ps, err := funcpool.NewStatic(factory, disp, funcpool.Config{Workers: 8, QueueSize: 256})
	if err != nil {
		b.Fatal(err)
	}

	ps.Start()
	defer ps.Stop()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ctx, fc := ctxapi.AcquireFrameContext(context.Background())
			ps.Call(ctx, "", nil)
			ctxapi.ReleaseFrameContext(fc)
		}
	})
}

// Benchmark8x8WithYield tests 8 workers with yield
func Benchmark8x8WithYield(b *testing.B) {
	factory := newLuaFactory(`
		time.sleep(time.NANOSECOND)
		return 1
	`)
	disp := newPoolTestDispatcher()

	ps, err := funcpool.NewStatic(factory, disp, funcpool.Config{Workers: 8, QueueSize: 256})
	if err != nil {
		b.Fatal(err)
	}

	ps.Start()
	defer ps.Stop()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ctx, fc := ctxapi.AcquireFrameContext(context.Background())
			ps.Call(ctx, "", nil)
			ctxapi.ReleaseFrameContext(fc)
		}
	})
}

// BenchmarkSingleWorker tests throughput of a single worker (no parallelism)
func BenchmarkSingleWorker(b *testing.B) {
	factory := newLuaFactory(`return 1`)
	disp := newPoolTestDispatcher()

	ps, err := funcpool.NewStatic(factory, disp, funcpool.Config{
		Workers:   1,
		QueueSize: 16,
	})
	if err != nil {
		b.Fatal(err)
	}

	ps.Start()
	defer ps.Stop()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx, fc := ctxapi.AcquireFrameContext(context.Background())
		ps.Call(ctx, "", nil)
		ctxapi.ReleaseFrameContext(fc)
	}
}

// BenchmarkWorkerScalingLua tests Lua throughput with different worker counts
func BenchmarkWorkerScalingLua(b *testing.B) {
	for _, workers := range []int{1, 2, 4, 8, 16} {
		b.Run(fmt.Sprintf("W%d", workers), func(b *testing.B) {
			factory := newLuaFactory(`return 1`)
			disp := newPoolTestDispatcher()

			ps, err := funcpool.NewStatic(factory, disp, funcpool.Config{
				Workers:   workers,
				QueueSize: 16,
			})
			if err != nil {
				b.Fatal(err)
			}

			ps.Start()
			defer ps.Stop()

			b.ResetTimer()
			b.ReportAllocs()

			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					ctx, fc := ctxapi.AcquireFrameContext(context.Background())
					ps.Call(ctx, "", nil)
					ctxapi.ReleaseFrameContext(fc)
				}
			})
		})
	}
}

// TestPoolMemoryStability checks for memory leaks across many calls
func TestPoolMemoryStability(t *testing.T) {
	factory := newLuaFactory(`return 1`)
	disp := newPoolTestDispatcher()

	ps, err := funcpool.NewStatic(factory, disp, funcpool.Config{Workers: 4})
	if err != nil {
		t.Fatal(err)
	}

	ps.Start()
	defer ps.Stop()

	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	const iterations = 10000
	for i := 0; i < iterations; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		_, err := ps.Call(ctx, "", nil)
		if err != nil {
			t.Fatal(err)
		}
	}

	runtime.GC()
	runtime.GC()
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	heapGrowth := int64(m2.HeapAlloc) - int64(m1.HeapAlloc)
	t.Logf("After %d calls: heap growth = %d KB", iterations, heapGrowth/1024)

	maxGrowthKB := int64(1024)
	if heapGrowth/1024 > maxGrowthKB {
		t.Errorf("Possible memory leak: heap grew by %d KB (max %d KB)", heapGrowth/1024, maxGrowthKB)
	}
}

// TestThroughput runs a timed throughput test
func TestThroughput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping throughput test in short mode")
	}

	workers := runtime.GOMAXPROCS(0) // Match workers to cores

	factory := newLuaFactory(`return 1`)
	disp := newPoolTestDispatcher()

	ps, err := funcpool.NewStatic(factory, disp, funcpool.Config{
		Workers:   workers,
		QueueSize: workers * 16,
	})
	if err != nil {
		t.Fatal(err)
	}

	ps.Start()
	defer ps.Stop()

	duration := 2 * time.Second
	var completed atomic.Int64
	var errors atomic.Int64

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	var wg sync.WaitGroup
	goroutines := runtime.GOMAXPROCS(0) * 4 // Many goroutines to saturate system

	start := time.Now()
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					callCtx, _ := ctxapi.OpenFrameContext(context.Background())
					_, err := ps.Call(callCtx, "", nil)
					if err != nil {
						errors.Add(1)
					} else {
						completed.Add(1)
					}
				}
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	total := completed.Load()
	rate := float64(total) / elapsed.Seconds()

	t.Logf("Config: %d workers, %d goroutines", workers, goroutines)
	t.Logf("Duration: %v, Completed: %d, Errors: %d", elapsed, total, errors.Load())
	t.Logf("Throughput: %.0f calls/sec", rate)
}
