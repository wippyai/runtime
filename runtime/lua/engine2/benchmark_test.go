package engine2

import (
	"context"
	"runtime"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	clockapi "github.com/wippyai/runtime/api/dispatcher/clock"
	"github.com/wippyai/runtime/api/relay"
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
		if err := proc.Execute(ctx, "", nil); err != nil {
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
		if err := proc.Execute(ctx, "", nil); err != nil {
			b.Fatal(err)
		}
		proc.Step(nil)
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
		if err := proc.Execute(ctx, "", nil); err != nil {
			b.Fatal(err)
		}

		for {
			result, err := proc.Step(nil)
			if err != nil {
				b.Fatal(err)
			}
			if result.Status == scheduler.StepDone {
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
		if err := proc.Execute(ctx, "", nil); err != nil {
			b.Fatal(err)
		}
		proc.Step(nil)
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
		if err := proc.Execute(ctx, "", nil); err != nil {
			b.Fatal(err)
		}

		for {
			result, err := proc.Step(nil)
			if err != nil {
				b.Fatal(err)
			}
			if result.Status == scheduler.StepDone {
				break
			}
		}
		proc.Close()
	}
}

// BenchmarkTimeSleep measures sleep yield overhead.
func BenchmarkTimeSleep(b *testing.B) {
	script := `
		time.sleep(time.NANOSECOND)
		return "done"
	`

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := NewProcess(WithScript(script, "test.lua"))
		if err := proc.Execute(ctx, "", nil); err != nil {
			b.Fatal(err)
		}
		BindTimeSleep(proc.State())

		result, err := proc.Step(nil)
		if err != nil {
			b.Fatal(err)
		}
		if result.Status != scheduler.StepContinue {
			b.Fatalf("expected StepContinue, got %v", result.Status)
		}

		// Resume after sleep
		result, err = proc.Step(&scheduler.YieldResults{})
		if err != nil {
			b.Fatal(err)
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
					if err := proc.Execute(ctx, "", nil); err != nil {
						b.Fatal(err)
					}
					processes[j] = proc
				}

				// Step all
				for j := 0; j < count; j++ {
					processes[j].Step(nil)
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
	if err := proc.Execute(ctx, "", nil); err != nil {
		b.Fatal(err)
	}
	defer proc.Close()

	// Initial step to get to yield
	_, err := proc.Step(nil)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Re-queue and resume the yielded task
		proc.queue.Push(proc.mainTask)
		_, err := proc.Step(nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkActorReceive measures actor-style message receive (external → process).
func BenchmarkActorReceive(b *testing.B) {
	script := `
		-- Subscribe to inbox
		local inbox = channel.new(10)
		subscribe("inbox", inbox)

		-- Actor loop: receive messages
		local count = 0
		while true do
			local msg = inbox:recv()
			count = count + 1
			if msg == "stop" then
				return count
			end
		end
	`

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := NewProcess(
		WithScript(script, "actor.lua"),
		WithLayer(NewChannelLayer()),
		WithLayer(NewSubscribeLayer()),
	)

	if err := proc.Execute(ctx, "", nil); err != nil {
		b.Fatal(err)
	}
	defer proc.Close()

	BindChannelFunctions(proc.State(), proc)
	BindSubscribeFunctions(proc.State(), proc)

	// Run until subscribed and waiting on channel
	for i := 0; i < 20; i++ {
		result, err := proc.Step(nil)
		if err != nil {
			b.Fatal(err)
		}
		if result.Status == scheduler.StepIdle {
			break
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		proc.Send(&relay.Package{
			Messages: []*relay.Message{{Topic: "inbox", Payloads: nil}},
		})

		_, err := proc.Step(nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// TestActorPattern tests the full actor message flow.
func TestActorPattern(t *testing.T) {
	script := `
		local inbox = channel.new(10)
		subscribe("inbox", inbox)

		local messages = {}
		for i = 1, 3 do
			local msg = inbox:recv()
			table.insert(messages, msg)
		end
		return #messages
	`

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := NewProcess(
		WithScript(script, "actor.lua"),
		WithLayer(NewChannelLayer()),
		WithLayer(NewSubscribeLayer()),
	)

	if err := proc.Execute(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	BindChannelFunctions(proc.State(), proc)
	BindSubscribeFunctions(proc.State(), proc)

	// Run until waiting for messages
	var result scheduler.StepResult
	var err error
	for i := 0; i < 10; i++ {
		result, err = proc.Step(nil)
		if err != nil {
			t.Fatal(err)
		}
		if result.Status == scheduler.StepIdle {
			break
		}
	}

	if result.Status != scheduler.StepIdle {
		t.Fatalf("expected StepIdle, got %v", result.Status)
	}

	// Send 3 messages
	for i := 0; i < 3; i++ {
		proc.Send(&relay.Package{
			Messages: []*relay.Message{{Topic: "inbox", Payloads: nil}},
		})

		result, err = proc.Step(nil)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Should complete
	for result.Status != scheduler.StepDone {
		result, err = proc.Step(nil)
		if err != nil {
			t.Fatal(err)
		}
	}

	t.Log("Actor pattern test passed")
}

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
		for i := 0; i < count; i++ {
			ctx, _ := ctxapi.OpenFrameContext(context.Background())
			proc := NewProcess(WithScript(script, "test.lua"))
			if err := proc.Execute(ctx, "", nil); err != nil {
				t.Fatal(err)
			}
			proc.Step(nil)
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
		if err := proc.Execute(ctx, "", nil); err != nil {
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
		if err := proc.Execute(ctx, "", nil); err != nil {
			b.Fatal(err)
		}
		proc.Step(nil)
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
		if err := proc.Execute(ctx, "", nil); err != nil {
			b.Fatal(err)
		}

		for {
			result, err := proc.Step(nil)
			if err != nil {
				b.Fatal(err)
			}
			if result.Status == scheduler.StepDone {
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
	if err := proc.Execute(ctx, "", nil); err != nil {
		b.Fatal(err)
	}
	defer proc.Close()

	// Warm up
	proc.Step(nil)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		proc.queue.Push(proc.mainTask)
		proc.Step(nil)
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
	if err := proc.Execute(ctx, "", nil); err != nil {
		b.Fatal(err)
	}
	defer proc.Close()

	task := proc.mainTask

	// Warm up - execute first resume
	proc.state.ResumeInto(task.Thread(), task.Function(), task.retBuf, task.Resumed...)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		proc.state.ResumeInto(task.Thread(), task.Function(), task.retBuf)
	}
}

// TestStressCoroutinesWithTimers tests many coroutines with timer yields.
func TestStressCoroutinesWithTimers(t *testing.T) {
	script := `
		local done = 0
		local total = 50
		for i = 1, total do
			coroutine.spawn(function()
				for j = 1, 5 do
					time.sleep(time.MICROSECOND)
				end
				done = done + 1
			end)
		end
		-- Wait for all coroutines to finish
		while done < total do
			coroutine.yield()
		end
		return done
	`

	proto, err := lua.CompileString(script, "stress.lua")
	if err != nil {
		t.Fatal(err)
	}

	runtime.GC()
	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	startTime := time.Now()

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := NewProcess(
		WithProto(proto),
		WithModuleBinder(BindTimeSleep),
	)
	if err := proc.Execute(ctx, "", nil); err != nil {
		t.Fatal(err)
	}

	// Run until done
	maxSteps := 5000
	stepCount := 0
	sleepYields := 0
	peakThreads := 0

	for stepCount < maxSteps {
		result, err := proc.Step(nil)
		if err != nil {
			t.Fatal(err)
		}
		stepCount++

		if len(proc.threads) > peakThreads {
			peakThreads = len(proc.threads)
		}

		if result.Status == scheduler.StepDone {
			break
		}

		// Count sleep yields
		yields := result.GetYields()
		for _, y := range yields {
			if _, ok := y.(clockapi.SleepCmd); ok {
				sleepYields++
			}
		}

		// Resume after yields
		if result.YieldCount() > 0 {
			proc.Step(&scheduler.YieldResults{})
			stepCount++
		}
	}

	elapsed := time.Since(startTime)
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	t.Logf("Stress test: 50 coroutines x 5 timer iterations")
	t.Logf("  Steps: %d, Sleep yields: %d", stepCount, sleepYields)
	t.Logf("  Time: %v, Peak threads: %d", elapsed, peakThreads)
	t.Logf("  Memory: before=%dKB, after=%dKB", m1.Alloc/1024, m2.Alloc/1024)

	proc.Close()
}

// BenchmarkStressCoroutinesTimers benchmarks many coroutines with timers.
func BenchmarkStressCoroutinesTimers(b *testing.B) {
	script := `
		for i = 1, 20 do
			coroutine.spawn(function()
				for j = 1, 5 do
					time.sleep(time.MICROSECOND)
				end
			end)
		end
		return "done"
	`

	proto, err := lua.CompileString(script, "bench.lua")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := NewProcess(
			WithProto(proto),
			WithModuleBinder(BindTimeSleep),
		)
		if err := proc.Execute(ctx, "", nil); err != nil {
			b.Fatal(err)
		}

		for j := 0; j < 500; j++ {
			result, err := proc.Step(nil)
			if err != nil {
				b.Fatal(err)
			}
			if result.Status == scheduler.StepDone {
				break
			}
			// Handle yields
			if result.YieldCount() > 0 {
				proc.Step(&scheduler.YieldResults{})
			}
		}
		proc.Close()
	}
}

// BenchmarkTimeSleepHotPath measures the real yield hot path via time.sleep (return -1).
// This isolates just yield+resume cycles without process creation overhead.
func BenchmarkTimeSleepHotPath(b *testing.B) {
	script := `
		while true do
			time.sleep(time.NANOSECOND)
		end
	`
	proto, err := lua.CompileString(script, "hotpath.lua")
	if err != nil {
		b.Fatal(err)
	}

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := NewProcess(
		WithProto(proto),
		WithModuleBinder(BindTimeSleep),
	)
	if err := proc.Execute(ctx, "", nil); err != nil {
		b.Fatal(err)
	}
	defer proc.Close()

	// Warm up - first step to get into the loop
	result, _ := proc.Step(nil)
	if result.YieldCount() == 0 {
		b.Fatal("expected yield")
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Resume from sleep
		proc.Step(&scheduler.YieldResults{})
		// Step again to hit next sleep
		proc.Step(nil)
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
	if err := proc.Execute(ctx, "", nil); err != nil {
		t.Fatal(err)
	}

	// Run until all coroutines spawned
	for i := 0; i < 150; i++ {
		result, err := proc.Step(nil)
		if err != nil {
			t.Fatal(err)
		}
		if result.Status == scheduler.StepIdle {
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
