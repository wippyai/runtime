package engine

import (
	"context"
	"runtime"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/process"
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
	contexts := make([]context.Context, 0, b.N)

	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

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
		contexts = append(contexts, ctx)
	}

	b.StopTimer()

	runtime.GC()
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

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
	script := `
		local data = {}
		for i = 1, 10 do
			data[i] = string.rep("x", 100)
		end
		return #data
	`

	counts := []int{100, 500, 1000, 5000}

	for _, count := range counts {
		runtime.GC()
		var m1 runtime.MemStats
		runtime.ReadMemStats(&m1)

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

		runtime.GC()
		var m2 runtime.MemStats
		runtime.ReadMemStats(&m2)

		bytesUsed := m2.Alloc - m1.Alloc
		bytesPerProcess := bytesUsed / uint64(count)

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
	_, _, _ = proc.state.ResumeInto(task.Thread(), task.Function(), task.retBuf, task.Resumed...)

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

	BindChannelFunctions(proc.State())
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
	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	for i := 0; i < b.N; i++ {
		proc := setupChannelProc(b, script)
		runProcToCompletion(b, proc, 100)
		proc.Close()
	}

	runtime.GC()
	runtime.ReadMemStats(&m2)

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
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
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

	runtime.GC()
	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

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

	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

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
