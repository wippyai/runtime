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
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/runtime/resource"
	scheduler "github.com/wippyai/runtime/system/scheduler/actor"
	lua "github.com/yuin/gopher-lua"
)

// BenchmarkProcessCreate measures process creation overhead.
func BenchmarkProcessCreate(b *testing.B) {
	script := `return 1`

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := NewProcess(WithScript(script, "test.lua"))
		if err := proc.Init(ctx, "", nil); err != nil {
			b.Fatal(err)
		}
		proc.Close()
	}
}

// BenchmarkProcessStep measures single step overhead.
func BenchmarkProcessStep(b *testing.B) {
	script := `return 1`

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := NewProcess(WithScript(script, "test.lua"))
		if err := proc.Init(ctx, "", nil); err != nil {
			b.Fatal(err)
		}
		var output process.StepOutput
		_ = proc.Step(nil, &output)
		proc.Close()
	}
}

// BenchmarkCoroutineSpawn measures spawning coroutines.
func BenchmarkCoroutineSpawn(b *testing.B) {
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
		return "done"
	`

	// Pre-compile outside the benchmark loop
	proto, err := lua.CompileString(script, "test.lua")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := NewProcess(WithProto(proto))
		if err := proc.Init(ctx, "", nil); err != nil {
			b.Fatal(err)
		}

		var output process.StepOutput
		for {
			output.Reset()
			if err := proc.Step(nil, &output); err != nil {
				b.Fatal(err)
			}
			if output.Status() == process.StepDone {
				break
			}
		}
		proc.Close()
	}
}

// BenchmarkMemoryPerProcess measures memory per idle process.
func BenchmarkMemoryPerProcess(b *testing.B) {
	script := `return 1`

	processes := make([]*Process, 0, b.N)

	goruntime.GC()
	var m1 goruntime.MemStats
	goruntime.ReadMemStats(&m1)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := NewProcess(WithScript(script, "test.lua"))
		if err := proc.Init(ctx, "", nil); err != nil {
			b.Fatal(err)
		}
		var output process.StepOutput
		_ = proc.Step(nil, &output)
		processes = append(processes, proc)
	}

	b.StopTimer()

	goruntime.GC()
	var m2 goruntime.MemStats
	goruntime.ReadMemStats(&m2)

	bytesPerProcess := float64(m2.Alloc-m1.Alloc) / float64(b.N)
	b.ReportMetric(bytesPerProcess, "bytes/process")

	// Cleanup
	for _, proc := range processes {
		proc.Close()
	}
}

// BenchmarkYieldResume measures yield/resume cycle.
func BenchmarkYieldResume(b *testing.B) {
	script := `
		for i = 1, 10 do
			coroutine.yield(i)
		end
		return "done"
	`

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := NewProcess(WithScript(script, "test.lua"))
		if err := proc.Init(ctx, "", nil); err != nil {
			b.Fatal(err)
		}

		var output process.StepOutput
		for {
			output.Reset()
			if err := proc.Step(nil, &output); err != nil {
				b.Fatal(err)
			}
			if output.Status() == process.StepDone {
				break
			}
		}
		proc.Close()
	}
}

// BenchmarkManyProcesses tests scaling with many processes.
func BenchmarkManyProcesses(b *testing.B) {
	counts := []int{100, 1000, 10000}

	for _, count := range counts {
		b.Run(string(rune('0'+count/100))+"00", func(b *testing.B) {
			script := `return 1 + 2`

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				processes := make([]*Process, count)

				// Create all
				for j := 0; j < count; j++ {
					ctx, _ := ctxapi.OpenFrameContext(context.Background())
					proc := NewProcess(WithScript(script, "test.lua"))
					if err := proc.Init(ctx, "", nil); err != nil {
						b.Fatal(err)
					}
					processes[j] = proc
				}

				// Step all
				var output process.StepOutput
				for j := 0; j < count; j++ {
					output.Reset()
					_ = processes[j].Step(nil, &output)
				}

				// Close all
				for j := 0; j < count; j++ {
					processes[j].Close()
				}
			}
		})
	}
}

// BenchmarkSendMessage measures message sending to running process (comparable to old CVM).
func BenchmarkSendMessage(b *testing.B) {
	script := `
		function echo()
			while true do
				local msg = coroutine.yield("ready")
			end
		end

		coroutine.spawn(echo)
	`

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := NewProcess(WithScript(script, "bench.lua"))
	if err := proc.Init(ctx, "", nil); err != nil {
		b.Fatal(err)
	}
	defer proc.Close()

	// Initial step to get to yield
	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Re-queue and resume the yielded task
		proc.queue.Push(proc.mainTask)
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			b.Fatal(err)
		}
	}
}

// NOTE: BenchmarkActorReceive and TestActorPattern were removed because they
// relied on process.NewMessageInbox() and process.SetInbox() which don't exist.
// Actor message delivery is tested in channel_test.go via sendMessage helper.

// TestMemoryProfile creates many processes and reports memory stats.
func TestMemoryProfile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	script := `
		local data = {}
		for i = 1, 10 do
			data[i] = string.rep("x", 100)
		end
		return #data
	`

	counts := []int{100, 500, 1000, 5000}

	for _, count := range counts {
		goruntime.GC()
		var m1 goruntime.MemStats
		goruntime.ReadMemStats(&m1)

		processes := make([]*Process, count)
		var output process.StepOutput
		for i := 0; i < count; i++ {
			ctx, _ := ctxapi.OpenFrameContext(context.Background())
			proc := NewProcess(WithScript(script, "test.lua"))
			if err := proc.Init(ctx, "", nil); err != nil {
				t.Fatal(err)
			}
			output.Reset()
			_ = proc.Step(nil, &output)
			processes[i] = proc
		}

		goruntime.GC()
		var m2 goruntime.MemStats
		goruntime.ReadMemStats(&m2)

		bytesUsed := m2.Alloc - m1.Alloc
		bytesPerProcess := bytesUsed / uint64(count) //#nosec G115

		t.Logf("%d processes: total=%dKB, per-process=%dKB",
			count, bytesUsed/1024, bytesPerProcess/1024)

		// Calculate how many fit in 1GB
		processesIn1GB := (1024 * 1024 * 1024) / bytesPerProcess
		t.Logf("  -> estimated %d processes in 1GB RAM", processesIn1GB)

		// Cleanup
		for _, proc := range processes {
			proc.Close()
		}
	}
}

// BenchmarkProcessCreatePrecompiled measures process creation with precompiled bytecode.
func BenchmarkProcessCreatePrecompiled(b *testing.B) {
	script := `return 1`
	proto, err := lua.CompileString(script, "test.lua")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := NewProcess(WithProto(proto))
		if err := proc.Init(ctx, "", nil); err != nil {
			b.Fatal(err)
		}
		proc.Close()
	}
}

// BenchmarkProcessStepPrecompiled measures step with precompiled bytecode.
func BenchmarkProcessStepPrecompiled(b *testing.B) {
	script := `return 1`
	proto, err := lua.CompileString(script, "test.lua")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := NewProcess(WithProto(proto))
		if err := proc.Init(ctx, "", nil); err != nil {
			b.Fatal(err)
		}
		var output process.StepOutput
		_ = proc.Step(nil, &output)
		proc.Close()
	}
}

// BenchmarkYieldResumePrecompiled measures yield/resume with precompiled bytecode.
func BenchmarkYieldResumePrecompiled(b *testing.B) {
	script := `
		for i = 1, 10 do
			coroutine.yield(i)
		end
		return "done"
	`
	proto, err := lua.CompileString(script, "test.lua")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := NewProcess(WithProto(proto))
		if err := proc.Init(ctx, "", nil); err != nil {
			b.Fatal(err)
		}

		var output process.StepOutput
		for {
			output.Reset()
			if err := proc.Step(nil, &output); err != nil {
				b.Fatal(err)
			}
			if output.Status() == process.StepDone {
				break
			}
		}
		proc.Close()
	}
}

// BenchmarkHotPathYield measures just the yield/resume hot path (no process creation).
func BenchmarkHotPathYield(b *testing.B) {
	// Cache yield function to avoid string lookups
	script := `
		local yield = coroutine.yield
		while true do
			yield()
		end
	`
	proto, err := lua.CompileString(script, "test.lua")
	if err != nil {
		b.Fatal(err)
	}

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := NewProcess(WithProto(proto))
	if err := proc.Init(ctx, "", nil); err != nil {
		b.Fatal(err)
	}
	defer proc.Close()

	// Warm up
	var output process.StepOutput
	_ = proc.Step(nil, &output)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		proc.queue.Push(proc.mainTask)
		output.Reset()
		_ = proc.Step(nil, &output)
	}
}

// BenchmarkRawVMYield measures raw VM yield/resume (no layers, minimal overhead).
func BenchmarkRawVMYield(b *testing.B) {
	script := `
		while true do
			coroutine.yield()
		end
	`
	proto, err := lua.CompileString(script, "test.lua")
	if err != nil {
		b.Fatal(err)
	}

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := NewProcess(WithProto(proto))
	if err := proc.Init(ctx, "", nil); err != nil {
		b.Fatal(err)
	}
	defer proc.Close()

	task := proc.mainTask

	// Warm up - execute first resume
	_, _, _ = proc.state.ResumeInto(task.Thread(), task.Function(), task.retBuf, task.Resumed...) //nolint:dogsled

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _, _ = proc.state.ResumeInto(task.Thread(), task.Function(), task.retBuf)
	}
}

// Channel Benchmarks (consolidated from channel_bench_test.go)

func setupChannelProc(b *testing.B, script string) *Process {
	proto, _ := lua.CompileString(script, "bench.lua")
	proc := NewProcess(
		WithProto(proto),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		b.Fatal(err)
	}

	ChannelModule.Load(proc.State())
	return proc
}

func runProcToCompletion(b *testing.B, proc *Process, maxSteps int) {
	var output process.StepOutput
	for i := 0; i < maxSteps; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			b.Fatal(err)
		}
		if output.Status() == process.StepDone {
			return
		}
	}
	b.Fatal("did not complete")
}

func BenchmarkChannelCreate(b *testing.B) {
	script := `
		for i = 1, 1000 do
			local ch = channel.new(0)
		end
	`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		proc := setupChannelProc(b, script)
		runProcToCompletion(b, proc, 100)
		proc.Close()
	}
}

func BenchmarkChannelBufferedSendRecv(b *testing.B) {
	script := `
		local ch = channel.new(100)
		for i = 1, 100 do
			ch:send(i)
		end
		for i = 1, 100 do
			ch:receive()
		end
	`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		proc := setupChannelProc(b, script)
		runProcToCompletion(b, proc, 500)
		proc.Close()
	}
}

func BenchmarkChannelUnbufferedPingPong(b *testing.B) {
	script := `
		local ch1 = channel.new(0)
		local ch2 = channel.new(0)
		local count = 0

		coroutine.spawn(function()
			for i = 1, 50 do
				ch1:receive()
				ch2:send(true)
			end
		end)

		for i = 1, 50 do
			ch1:send(true)
			ch2:receive()
			count = count + 1
		end
	`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		proc := setupChannelProc(b, script)
		runProcToCompletion(b, proc, 500)
		proc.Close()
	}
}

func BenchmarkChannelSelect(b *testing.B) {
	script := `
		local ch1 = channel.new(1)
		local ch2 = channel.new(1)

		for i = 1, 100 do
			ch1:send(i)
			local result = channel.select{ch1:case_receive(), ch2:case_receive()}
		end
	`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		proc := setupChannelProc(b, script)
		runProcToCompletion(b, proc, 500)
		proc.Close()
	}
}

func BenchmarkChannelMultipleSpawns(b *testing.B) {
	script := `
		local ch = channel.new(10)

		for i = 1, 10 do
			coroutine.spawn(function()
				ch:send(i)
			end)
		end

		for i = 1, 20 do coroutine.yield() end

		local sum = 0
		for i = 1, 10 do
			sum = sum + ch:receive()
		end
	`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		proc := setupChannelProc(b, script)
		runProcToCompletion(b, proc, 200)
		proc.Close()
	}
}

func BenchmarkChannelProducerConsumer(b *testing.B) {
	script := `
		local ch = channel.new(10)
		local done = 0

		coroutine.spawn(function()
			for i = 1, 100 do
				ch:send(i)
			end
			ch:close()
		end)

		coroutine.spawn(function()
			while true do
				local v, ok = ch:receive()
				if not ok then break end
				done = done + 1
			end
		end)

		for i = 1, 200 do coroutine.yield() end
	`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		proc := setupChannelProc(b, script)
		runProcToCompletion(b, proc, 500)
		proc.Close()
	}
}

func BenchmarkChannelMemory(b *testing.B) {
	script := `
		local ch = channel.new(0)
		coroutine.spawn(function()
			ch:send("value")
		end)
		local v = ch:receive()
	`

	b.ResetTimer()
	var m1, m2 goruntime.MemStats
	goruntime.GC()
	goruntime.ReadMemStats(&m1)

	for i := 0; i < b.N; i++ {
		proc := setupChannelProc(b, script)
		runProcToCompletion(b, proc, 100)
		proc.Close()
	}

	goruntime.GC()
	goruntime.ReadMemStats(&m2)

	b.ReportMetric(float64(m2.TotalAlloc-m1.TotalAlloc)/float64(b.N), "bytes/op")
}

func BenchmarkSelectCases2(b *testing.B) {
	script := `
		local ch1 = channel.new(1)
		local ch2 = channel.new(1)
		ch1:send(1)
		for i = 1, 50 do
			channel.select{ch1:case_receive(), ch2:case_receive()}
			ch1:send(1)
		end
	`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		proc := setupChannelProc(b, script)
		runProcToCompletion(b, proc, 300)
		proc.Close()
	}
}

func BenchmarkPureChannelOps(b *testing.B) {
	ch := NewChannel(100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 100; j++ {
			r := ch.Send(nil, lua.LNumber(j), nil)
			ReleaseResult(r)
		}
		for j := 0; j < 100; j++ {
			r := ch.Receive(nil, nil)
			ReleaseResult(r)
		}
	}
}

func BenchmarkPureChannelSendRecv(b *testing.B) {
	ch := NewChannel(1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := ch.Send(nil, lua.LNumber(i), nil)
		ReleaseResult(r)
		r = ch.Receive(nil, nil)
		ReleaseResult(r)
	}
}

func BenchmarkPureChannelZeroAlloc(b *testing.B) {
	ch := NewChannel(100)
	val := lua.LNumber(42)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 100; j++ {
			r := ch.Send(nil, val, nil)
			ReleaseResult(r)
		}
		for j := 0; j < 100; j++ {
			r := ch.Receive(nil, nil)
			ReleaseResult(r)
		}
	}
}

func BenchmarkSelectCases4(b *testing.B) {
	script := `
		local ch1 = channel.new(1)
		local ch2 = channel.new(1)
		local ch3 = channel.new(1)
		local ch4 = channel.new(1)
		ch1:send(1)
		for i = 1, 50 do
			channel.select{ch1:case_receive(), ch2:case_receive(), ch3:case_receive(), ch4:case_receive()}
			ch1:send(1)
		end
	`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		proc := setupChannelProc(b, script)
		runProcToCompletion(b, proc, 300)
		proc.Close()
	}
}

// Memory Benchmarks (consolidated from memory_bench_test.go)

func measureMemory() uint64 {
	goruntime.GC()
	goruntime.GC()
	var m goruntime.MemStats
	goruntime.ReadMemStats(&m)
	return m.Alloc
}

func BenchmarkMemoryBaseline(b *testing.B) {
	script := `return 1`
	proto, err := lua.CompileString(script, "test.lua")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := NewProcess(WithProto(proto))
		if err := proc.Init(ctx, "", nil); err != nil {
			b.Fatal(err)
		}
		proc.Close()
	}
}

func TestMemoryPerProcessDetailed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	script := `return 1`
	proto, err := lua.CompileString(script, "test.lua")
	if err != nil {
		t.Fatal(err)
	}

	const count = 100
	processes := make([]*Process, count)

	before := measureMemory()

	for i := 0; i < count; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := NewProcess(WithProto(proto))
		if err := proc.Init(ctx, "", nil); err != nil {
			t.Fatal(err)
		}
		processes[i] = proc
	}

	after := measureMemory()

	perProcess := (after - before) / count
	t.Logf("Memory per process (baseline): %d bytes (~%.1f KB)", perProcess, float64(perProcess)/1024)

	for _, p := range processes {
		p.Close()
	}
}

// Test100CoroutinesMemory measures memory for 100 spawned coroutines.
func Test100CoroutinesMemory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	script := `
		for i = 1, 100 do
			coroutine.spawn(function()
				while true do
					coroutine.yield(i)
				end
			end)
		end
		return "spawned"
	`
	proto, err := lua.CompileString(script, "test.lua")
	if err != nil {
		t.Fatal(err)
	}

	goruntime.GC()
	goruntime.GC()
	var m1 goruntime.MemStats
	goruntime.ReadMemStats(&m1)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := NewProcess(WithProto(proto))
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}

	// Run until all coroutines spawned
	var output process.StepOutput
	for i := 0; i < 150; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatal(err)
		}
		if output.Status() == process.StepIdle {
			break
		}
	}

	var m2 goruntime.MemStats
	goruntime.ReadMemStats(&m2)

	// Handle case where GC may have freed memory
	var bytesUsed uint64
	if m2.Alloc > m1.Alloc {
		bytesUsed = m2.Alloc - m1.Alloc
	} else {
		bytesUsed = m2.Alloc
	}
	t.Logf("100 coroutines: threads=%d, memory=%dKB", len(proc.threads), bytesUsed/1024)
	if len(proc.threads) > 0 {
		t.Logf("Per coroutine overhead: ~%d bytes", bytesUsed/uint64(len(proc.threads)))
	}

	proc.Close()
}

// Integration benchmarks (from integration_bench_test.go)

type mockRegistry struct {
	handlers map[dispatcher.CommandID]dispatcher.Handler
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{
		handlers: make(map[dispatcher.CommandID]dispatcher.Handler),
	}
}

func (r *mockRegistry) Register(id dispatcher.CommandID, h dispatcher.Handler) {
	r.handlers[id] = h
}

func (r *mockRegistry) Get(id dispatcher.CommandID) dispatcher.Handler {
	return r.handlers[id]
}

func (r *mockRegistry) Has(id dispatcher.CommandID) bool {
	_, ok := r.handlers[id]
	return ok
}

type mockProcess struct {
	*Process
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

func (m *mockProcess) Send(_ *pid.Package) error {
	return nil
}

func newTestPID(id string) pid.PID {
	return pid.PID{
		Host:   "test",
		UniqID: id,
	}
}

type benchExecutor struct {
	sched   *scheduler.Scheduler
	mu      sync.Mutex
	pending map[string]chan *runtime.Result
}

type benchLifecycle struct {
	executor *benchExecutor
}

func (l *benchLifecycle) OnStart(_ context.Context, _ pid.PID, _ scheduler.Process) {}

func (l *benchLifecycle) OnComplete(_ context.Context, p pid.PID, result *runtime.Result) {
	l.executor.mu.Lock()
	if ch, ok := l.executor.pending[p.UniqID]; ok {
		delete(l.executor.pending, p.UniqID)
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
func (be *benchExecutor) Stop()                    { be.sched.Stop(context.Background()) }
func (be *benchExecutor) Stats() map[string]uint64 { return be.sched.Stats() }

func (be *benchExecutor) Execute(ctx context.Context, p pid.PID, proc scheduler.Process, method string, input payload.Payloads) (*runtime.Result, error) {
	resultCh := make(chan *runtime.Result, 1)

	be.mu.Lock()
	be.pending[p.UniqID] = resultCh
	be.mu.Unlock()

	_, err := be.sched.Submit(ctx, p, proc, method, input)
	if err != nil {
		be.mu.Lock()
		delete(be.pending, p.UniqID)
		be.mu.Unlock()
		return nil, err
	}

	select {
	case result := <-resultCh:
		return result, nil
	case <-ctx.Done():
		be.mu.Lock()
		delete(be.pending, p.UniqID)
		be.mu.Unlock()
		return nil, ctx.Err()
	}
}

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

func TestIntegration1000ConcurrentGoroutines(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
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

func TestIntegrationMemoryStability(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
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

	first := heapAfterIterations[0]
	last := heapAfterIterations[iterations-1]
	if last > first*2 && last > baseline.HeapAlloc+50*1024*1024 {
		t.Errorf("Memory grew significantly: %d MB -> %d MB", first/1024/1024, last/1024/1024)
	}
}

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
