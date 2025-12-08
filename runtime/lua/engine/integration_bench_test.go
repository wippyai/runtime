package engine

import (
	"context"
	"fmt"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/runtime/resource"
	scheduler "github.com/wippyai/runtime/system/scheduler/actor"
	lua "github.com/yuin/gopher-lua"
)

// mockHandler implements process.Handler for testing yields.
type mockHandler struct {
	delay time.Duration
}

func (h *mockHandler) Handle(ctx context.Context, cmd process.Command) (any, error) {
	if h.delay > 0 {
		time.Sleep(h.delay)
	}
	return map[string]any{"status": "ok"}, nil
}

// mockRegistry implements process.Registry for testing.
type mockRegistry struct {
	handlers map[process.CommandID]process.Handler
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{
		handlers: make(map[process.CommandID]process.Handler),
	}
}

func (r *mockRegistry) Register(id process.CommandID, h process.Handler) {
	r.handlers[id] = h
}

func (r *mockRegistry) Get(id process.CommandID) process.Handler {
	return r.handlers[id]
}

func (r *mockRegistry) Has(id process.CommandID) bool {
	_, ok := r.handlers[id]
	return ok
}

// mockProcess wraps engine.Process for scheduler compatibility.
type mockProcess struct {
	*Process
	output process.StepOutput
}

func (m *mockProcess) Init(ctx context.Context, method string, input payload.Payloads) error {
	return m.Process.Init(ctx, method, input)
}

func (m *mockProcess) Step(events []process.Event, out *process.StepOutput) error {
	return m.Process.Step(events, out)
}

func (m *mockProcess) Close() {
	m.Process.Close()
}

func (m *mockProcess) Send(_ *relay.Package) error {
	return nil
}

// newTestPID creates a PID for testing.
func newTestPID(id string) relay.PID {
	return relay.PID{
		Host:   "test",
		UniqID: id,
	}
}

// benchExecutor provides synchronous execution for benchmarks
type benchExecutor struct {
	sched   *scheduler.Scheduler
	mu      sync.Mutex
	pending map[string]chan *runtime.Result
}

type benchLifecycle struct {
	executor *benchExecutor
}

func (l *benchLifecycle) OnStart(ctx context.Context, pid relay.PID, proc scheduler.Process) {}

func (l *benchLifecycle) OnComplete(ctx context.Context, pid relay.PID, result *runtime.Result) {
	l.executor.mu.Lock()
	if ch, ok := l.executor.pending[pid.UniqID]; ok {
		delete(l.executor.pending, pid.UniqID)
		l.executor.mu.Unlock()
		ch <- result
	} else {
		l.executor.mu.Unlock()
	}
}

func newBenchExecutor(registry *mockRegistry, workers int) *benchExecutor {
	return newBenchExecutorWithOptions(registry, scheduler.WithWorkers(workers))
}

func newBenchExecutorWithOptions(registry *mockRegistry, opts ...scheduler.Option) *benchExecutor {
	be := &benchExecutor{
		pending: make(map[string]chan *runtime.Result),
	}
	lc := &benchLifecycle{executor: be}
	opts = append(opts, scheduler.WithLifecycle(lc))
	be.sched = scheduler.NewScheduler(registry, opts...)
	return be
}

func (be *benchExecutor) Start()                   { be.sched.Start() }
func (be *benchExecutor) Stop()                    { be.sched.Stop() }
func (be *benchExecutor) Stats() map[string]uint64 { return be.sched.Stats() }

func (be *benchExecutor) Execute(ctx context.Context, pid relay.PID, p scheduler.Process, method string, input payload.Payloads) (*runtime.Result, error) {
	resultCh := make(chan *runtime.Result, 1)

	be.mu.Lock()
	be.pending[pid.UniqID] = resultCh
	be.mu.Unlock()

	_, err := be.sched.Submit(ctx, pid, p, method, input)
	if err != nil {
		be.mu.Lock()
		delete(be.pending, pid.UniqID)
		be.mu.Unlock()
		return nil, err
	}

	select {
	case result := <-resultCh:
		return result, nil
	case <-ctx.Done():
		be.mu.Lock()
		delete(be.pending, pid.UniqID)
		be.mu.Unlock()
		return nil, ctx.Err()
	}
}

// BenchmarkIntegrationFullPath benchmarks the complete path:
// Lua process creation -> actor scheduler -> completion
func BenchmarkIntegrationFullPath(b *testing.B) {
	script := `return 1 + 2`
	proto, err := lua.CompileString(script, "bench.lua")
	if err != nil {
		b.Fatal(err)
	}

	registry := newMockRegistry()
	executor := newBenchExecutor(registry, goruntime.GOMAXPROCS(0))
	executor.Start()
	defer executor.Stop()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx, fc := ctxapi.AcquireFrameContext(context.Background())
		store := resource.NewStore()
		_ = resource.SetStore(ctx, store)

		proc := NewProcess(WithProto(proto))
		mp := &mockProcess{Process: proc}

		pid := newTestPID(fmt.Sprintf("bench-%d", i))
		result, err := executor.Execute(ctx, pid, mp, "", nil)
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
		if result.Error != nil {
			b.Fatalf("Process error: %v", result.Error)
		}

		store.Close()
		ctxapi.ReleaseFrameContext(fc)
	}
}

// BenchmarkIntegrationWithCoroutines benchmarks with coroutine yields.
func BenchmarkIntegrationWithCoroutines(b *testing.B) {
	script := `
		local sum = 0
		for i = 1, 10 do
			coroutine.yield(i)
			sum = sum + i
		end
		return sum
	`
	proto, err := lua.CompileString(script, "coro.lua")
	if err != nil {
		b.Fatal(err)
	}

	registry := newMockRegistry()
	executor := newBenchExecutor(registry, goruntime.GOMAXPROCS(0))
	executor.Start()
	defer executor.Stop()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx, fc := ctxapi.AcquireFrameContext(context.Background())
		store := resource.NewStore()
		_ = resource.SetStore(ctx, store)

		proc := NewProcess(WithProto(proto))
		mp := &mockProcess{Process: proc}

		pid := newTestPID(fmt.Sprintf("bench-%d", i))
		result, err := executor.Execute(ctx, pid, mp, "", nil)
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
		if result.Error != nil {
			b.Fatalf("Process error: %v", result.Error)
		}

		store.Close()
		ctxapi.ReleaseFrameContext(fc)
	}
}

// BenchmarkIntegrationConcurrent benchmarks concurrent process execution.
func BenchmarkIntegrationConcurrent(b *testing.B) {
	script := `return 1 + 2`
	proto, err := lua.CompileString(script, "bench.lua")
	if err != nil {
		b.Fatal(err)
	}

	registry := newMockRegistry()
	executor := newBenchExecutor(registry, goruntime.GOMAXPROCS(0))
	executor.Start()
	defer executor.Stop()

	var counter atomic.Int64

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			i := counter.Add(1)
			ctx, fc := ctxapi.AcquireFrameContext(context.Background())
			store := resource.NewStore()
			_ = resource.SetStore(ctx, store)

			proc := NewProcess(WithProto(proto))
			mp := &mockProcess{Process: proc}

			pid := newTestPID(fmt.Sprintf("bench-%d", i))
			result, err := executor.Execute(ctx, pid, mp, "", nil)
			if err != nil {
				b.Fatalf("Execute failed: %v", err)
			}
			if result.Error != nil {
				b.Fatalf("Process error: %v", result.Error)
			}

			store.Close()
			ctxapi.ReleaseFrameContext(fc)
		}
	})
}

// TestIntegration1000ConcurrentGoroutines tests 1000 goroutines launching processes.
func TestIntegration1000ConcurrentGoroutines(t *testing.T) {
	const goroutines = 1000
	const processesPerGoroutine = 100

	script := `
		local sum = 0
		for i = 1, 100 do
			sum = sum + i
		end
		return sum
	`
	proto, err := lua.CompileString(script, "concurrent.lua")
	if err != nil {
		t.Fatal(err)
	}

	registry := newMockRegistry()
	executor := newBenchExecutor(registry, goruntime.GOMAXPROCS(0)*2)
	executor.Start()
	defer executor.Stop()

	goruntime.GC()
	var baseline goruntime.MemStats
	goruntime.ReadMemStats(&baseline)

	var wg sync.WaitGroup
	var completed atomic.Int64
	var errors atomic.Int64
	start := time.Now()

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for p := 0; p < processesPerGoroutine; p++ {
				ctx, fc := ctxapi.AcquireFrameContext(context.Background())
				store := resource.NewStore()
				_ = resource.SetStore(ctx, store)

				proc := NewProcess(WithProto(proto))
				mp := &mockProcess{Process: proc}

				pid := newTestPID(fmt.Sprintf("g%d-p%d", gid, p))
				result, err := executor.Execute(ctx, pid, mp, "", nil)
				if err != nil || result.Error != nil {
					errors.Add(1)
				} else {
					completed.Add(1)
				}

				store.Close()
				ctxapi.ReleaseFrameContext(fc)
			}
		}(g)
	}

	wg.Wait()
	elapsed := time.Since(start)

	var peak goruntime.MemStats
	goruntime.ReadMemStats(&peak)

	goruntime.GC()
	goruntime.GC()
	var afterGC goruntime.MemStats
	goruntime.ReadMemStats(&afterGC)

	totalProcesses := goroutines * processesPerGoroutine
	rps := float64(completed.Load()) / elapsed.Seconds()

	t.Logf("=== Integration Test: 1000 Concurrent Goroutines ===")
	t.Logf("Goroutines: %d", goroutines)
	t.Logf("Processes per goroutine: %d", processesPerGoroutine)
	t.Logf("Total processes: %d", totalProcesses)
	t.Logf("")
	t.Logf("Completed: %d", completed.Load())
	t.Logf("Errors: %d", errors.Load())
	t.Logf("Duration: %v", elapsed)
	t.Logf("Rate: %.0f processes/sec", rps)
	t.Logf("")
	t.Logf("Baseline HeapAlloc: %d MB", baseline.HeapAlloc/1024/1024)
	t.Logf("Peak HeapAlloc: %d MB", peak.HeapAlloc/1024/1024)
	t.Logf("After GC HeapAlloc: %d MB", afterGC.HeapAlloc/1024/1024)
	t.Logf("GC cycles during test: %d", afterGC.NumGC-baseline.NumGC)

	stats := executor.Stats()
	t.Logf("")
	t.Logf("Scheduler stats: executed=%d, queue=%d", stats["executed"], stats["global_queue"])

	if errors.Load() > 0 {
		t.Errorf("Had %d errors", errors.Load())
	}
}

// TestIntegrationProfileHotPath runs a profiling test for the hot path.
func TestIntegrationProfileHotPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping profiling test in short mode")
	}

	const duration = 10 * time.Second
	const concurrency = 1000

	script := `return 42`
	proto, err := lua.CompileString(script, "profile.lua")
	if err != nil {
		t.Fatal(err)
	}

	registry := newMockRegistry()
	executor := newBenchExecutor(registry, goruntime.GOMAXPROCS(0)*2)
	executor.Start()
	defer executor.Stop()

	var wg sync.WaitGroup
	var completed atomic.Int64
	stop := make(chan struct{})

	goruntime.GC()
	var baseline goruntime.MemStats
	goruntime.ReadMemStats(&baseline)

	start := time.Now()

	// Track memory during the run
	var peakHeap uint64
	memTicker := time.NewTicker(100 * time.Millisecond)
	go func() {
		for {
			select {
			case <-memTicker.C:
				var m goruntime.MemStats
				goruntime.ReadMemStats(&m)
				if m.HeapAlloc > peakHeap {
					peakHeap = m.HeapAlloc
				}
			case <-stop:
				memTicker.Stop()
				return
			}
		}
	}()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			localCount := 0
			for {
				select {
				case <-stop:
					completed.Add(int64(localCount))
					return
				default:
				}

				ctx, fc := ctxapi.AcquireFrameContext(context.Background())
				store := resource.NewStore()
				_ = resource.SetStore(ctx, store)

				proc := NewProcess(WithProto(proto))
				mp := &mockProcess{Process: proc}

				pid := newTestPID(fmt.Sprintf("profile-%d-%d", id, localCount))
				_, _ = executor.Execute(ctx, pid, mp, "", nil)

				store.Close()
				ctxapi.ReleaseFrameContext(fc)
				localCount++
			}
		}(i)
	}

	time.Sleep(duration)
	close(stop)
	wg.Wait()

	elapsed := time.Since(start)
	total := completed.Load()
	rps := float64(total) / elapsed.Seconds()

	goruntime.GC()
	goruntime.GC()
	var afterGC goruntime.MemStats
	goruntime.ReadMemStats(&afterGC)

	t.Logf("=== Hot Path Profiling Test ===")
	t.Logf("Duration: %v", elapsed)
	t.Logf("Concurrency: %d", concurrency)
	t.Logf("")
	t.Logf("Total processes: %d", total)
	t.Logf("Rate: %.0f processes/sec", rps)
	t.Logf("")
	t.Logf("Baseline HeapAlloc: %d MB", baseline.HeapAlloc/1024/1024)
	t.Logf("Peak HeapAlloc: %d MB", peakHeap/1024/1024)
	t.Logf("After GC HeapAlloc: %d MB", afterGC.HeapAlloc/1024/1024)
	t.Logf("GC cycles: %d", afterGC.NumGC-baseline.NumGC)

	stats := executor.Stats()
	t.Logf("")
	t.Logf("Scheduler stats: executed=%d", stats["executed"])
}

// BenchmarkIntegrationWithCoreBinders uses production-like module binders.
func BenchmarkIntegrationWithCoreBinders(b *testing.B) {
	script := `return 1 + 2`
	proto, err := lua.CompileString(script, "bench.lua")
	if err != nil {
		b.Fatal(err)
	}

	factory := NewFactory(FactoryConfig{
		Proto:         proto,
		ModuleBinders: CoreBinders(),
	})

	registry := newMockRegistry()
	exec := newBenchExecutor(registry, goruntime.GOMAXPROCS(0))
	exec.Start()
	defer exec.Stop()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx, fc := ctxapi.AcquireFrameContext(context.Background())
		store := resource.NewStore()
		_ = resource.SetStore(ctx, store)

		proc, err := factory()
		if err != nil {
			b.Fatalf("Factory failed: %v", err)
		}
		mp := &mockProcess{Process: proc.(*Process)}

		pid := newTestPID(fmt.Sprintf("bench-%d", i))
		result, err := exec.Execute(ctx, pid, mp, "", nil)
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
		if result.Error != nil {
			b.Fatalf("Process error: %v", result.Error)
		}

		store.Close()
		ctxapi.ReleaseFrameContext(fc)
	}
}

// BenchmarkIntegrationManyCoroutineYields benchmarks multiple coroutine yield cycles.
func BenchmarkIntegrationManyCoroutineYields(b *testing.B) {
	script := `
		local sum = 0
		for i = 1, 50 do
			coroutine.yield(i)
			sum = sum + i
		end
		return sum
	`
	proto, err := lua.CompileString(script, "many_yields.lua")
	if err != nil {
		b.Fatal(err)
	}

	registry := newMockRegistry()
	exec := newBenchExecutor(registry, goruntime.GOMAXPROCS(0))
	exec.Start()
	defer exec.Stop()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx, fc := ctxapi.AcquireFrameContext(context.Background())
		store := resource.NewStore()
		_ = resource.SetStore(ctx, store)

		proc := NewProcess(WithProto(proto))
		mp := &mockProcess{Process: proc}

		pid := newTestPID(fmt.Sprintf("bench-%d", i))
		result, err := exec.Execute(ctx, pid, mp, "", nil)
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
		if result.Error != nil {
			b.Fatalf("Process error: %v", result.Error)
		}

		store.Close()
		ctxapi.ReleaseFrameContext(fc)
	}
}

// TestIntegrationMemoryStability tests memory stability under sustained load.
func TestIntegrationMemoryStability(t *testing.T) {
	const iterations = 10
	const processesPerIteration = 10000

	script := `return 42`
	proto, err := lua.CompileString(script, "stable.lua")
	if err != nil {
		t.Fatal(err)
	}

	registry := newMockRegistry()
	exec := newBenchExecutorWithOptions(registry,
		scheduler.WithWorkers(goruntime.GOMAXPROCS(0)),
		scheduler.WithQueueSize(4096),
	)
	exec.Start()
	defer exec.Stop()

	goruntime.GC()
	goruntime.GC()
	var baseline goruntime.MemStats
	goruntime.ReadMemStats(&baseline)

	heapAfterIterations := make([]uint64, iterations)

	for iter := 0; iter < iterations; iter++ {
		for p := 0; p < processesPerIteration; p++ {
			ctx, fc := ctxapi.AcquireFrameContext(context.Background())
			store := resource.NewStore()
			_ = resource.SetStore(ctx, store)

			proc := NewProcess(WithProto(proto))
			mp := &mockProcess{Process: proc}

			pid := newTestPID(fmt.Sprintf("iter%d-p%d", iter, p))
			_, _ = exec.Execute(ctx, pid, mp, "", nil)

			store.Close()
			ctxapi.ReleaseFrameContext(fc)
		}

		goruntime.GC()
		var m goruntime.MemStats
		goruntime.ReadMemStats(&m)
		heapAfterIterations[iter] = m.HeapAlloc
	}

	t.Logf("=== Memory Stability Test ===")
	t.Logf("Iterations: %d", iterations)
	t.Logf("Processes per iteration: %d", processesPerIteration)
	t.Logf("Total processes: %d", iterations*processesPerIteration)
	t.Logf("")
	t.Logf("Baseline HeapAlloc: %d MB", baseline.HeapAlloc/1024/1024)

	for i, heap := range heapAfterIterations {
		t.Logf("After iteration %d: %d MB", i+1, heap/1024/1024)
	}

	// Check for memory growth
	first := heapAfterIterations[0]
	last := heapAfterIterations[iterations-1]
	if last > first*2 && last > baseline.HeapAlloc+50*1024*1024 {
		t.Errorf("Memory grew significantly: %d MB -> %d MB", first/1024/1024, last/1024/1024)
	}
}

// BenchmarkIntegrationSpawnCoroutines benchmarks spawning multiple coroutines.
func BenchmarkIntegrationSpawnCoroutines(b *testing.B) {
	script := `
		for i = 1, 10 do
			coroutine.spawn(function()
				local x = 0
				for j = 1, 100 do
					x = x + j
				end
				return x
			end)
		end
		-- Let spawns run
		for i = 1, 20 do
			coroutine.yield()
		end
		return "done"
	`
	proto, err := lua.CompileString(script, "spawn.lua")
	if err != nil {
		b.Fatal(err)
	}

	registry := newMockRegistry()
	exec := newBenchExecutor(registry, goruntime.GOMAXPROCS(0))
	exec.Start()
	defer exec.Stop()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx, fc := ctxapi.AcquireFrameContext(context.Background())
		store := resource.NewStore()
		_ = resource.SetStore(ctx, store)

		proc := NewProcess(WithProto(proto))
		mp := &mockProcess{Process: proc}

		pid := newTestPID(fmt.Sprintf("bench-%d", i))
		result, err := exec.Execute(ctx, pid, mp, "", nil)
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
		if result.Error != nil {
			b.Fatalf("Process error: %v", result.Error)
		}

		store.Close()
		ctxapi.ReleaseFrameContext(fc)
	}
}
