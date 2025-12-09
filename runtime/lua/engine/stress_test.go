package engine

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/process"
	lua "github.com/yuin/gopher-lua"
)

// skipUnlessStress skips the test unless WIPPY_STRESS_TESTS=1
func skipUnlessStress(t *testing.T) {
	if os.Getenv("WIPPY_STRESS_TESTS") != "1" {
		t.Skip("Skipping stress test (set WIPPY_STRESS_TESTS=1 to run)")
	}
}

// TestStress10KProcesses tests creating and running 10,000 processes.
func TestStress10KProcesses(t *testing.T) {
	skipUnlessStress(t)
	const processCount = 10000
	script := `
		local sum = 0
		for i = 1, 100 do
			sum = sum + i
		end
		return sum
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

	processes := make([]*Process, processCount)
	errors := make([]error, 0)

	// Create all processes
	for i := 0; i < processCount; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := NewProcess(WithProto(proto))
		if err := proc.Init(ctx, "", nil); err != nil {
			errors = append(errors, fmt.Errorf("process %d start: %w", i, err))
			continue
		}
		processes[i] = proc
	}

	if len(errors) > 0 {
		t.Fatalf("Failed to create processes: %d errors, first: %v", len(errors), errors[0])
	}

	createTime := time.Since(startTime)

	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	t.Logf("Created %d processes in %v", processCount, createTime)
	t.Logf("Memory after creation: %d MB (delta: %d MB)",
		m2.Alloc/(1024*1024), (m2.Alloc-m1.Alloc)/(1024*1024))
	t.Logf("Per-process memory: %.1f KB", float64(m2.Alloc-m1.Alloc)/float64(processCount)/1024)

	// Step all processes
	stepStart := time.Now()
	completedCount := 0
	var output process.StepOutput

	for i, proc := range processes {
		if proc == nil {
			continue
		}
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("process %d step: %v", i, err)
		}
		if output.Status() == process.StepDone {
			completedCount++
		}
	}

	stepTime := time.Since(stepStart)
	t.Logf("Stepped %d processes in %v (%.0f ops/sec)", processCount, stepTime, float64(processCount)/stepTime.Seconds())
	t.Logf("Completed: %d/%d", completedCount, processCount)

	// Close all processes
	closeStart := time.Now()
	for _, proc := range processes {
		if proc != nil {
			proc.Close()
		}
	}
	closeTime := time.Since(closeStart)

	runtime.GC()
	runtime.GC()
	var m3 runtime.MemStats
	runtime.ReadMemStats(&m3)

	t.Logf("Closed %d processes in %v", processCount, closeTime)
	t.Logf("Memory after close+GC: %d MB (initial was %d MB)",
		m3.Alloc/(1024*1024), m1.Alloc/(1024*1024))

	// Check for leaks
	leaked := int64(m3.Alloc) - int64(m1.Alloc) //#nosec G115
	leakPerProcess := float64(leaked) / float64(processCount)
	t.Logf("Memory delta after cleanup: %d bytes (%.1f bytes/process)", leaked, leakPerProcess)

	if leaked > int64(processCount*1024) {
		t.Logf("WARNING: Potential memory leak detected (>1KB per process remaining)")
	}
}

// TestStress10KWithCoroutines tests 10,000 processes each spawning coroutines.
func TestStress10KWithCoroutines(t *testing.T) {
	skipUnlessStress(t)
	const processCount = 10000
	const coroutinesPerProcess = 5
	script := `
		local results = {}
		for i = 1, 5 do
			coroutine.spawn(function()
				local sum = 0
				for j = 1, 50 do
					sum = sum + j
				end
				return sum
			end)
		end
		-- Run spawned coroutines
		for i = 1, 10 do
			coroutine.yield()
		end
		return "done"
	`
	proto, err := lua.CompileString(script, "stress_coro.lua")
	if err != nil {
		t.Fatal(err)
	}

	runtime.GC()
	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	startTime := time.Now()

	processes := make([]*Process, processCount)

	// Create and run all processes
	for i := 0; i < processCount; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := NewProcess(WithProto(proto))
		if err := proc.Init(ctx, "", nil); err != nil {
			t.Fatalf("process %d start: %v", i, err)
		}
		processes[i] = proc

		// Run until idle or done
		var output process.StepOutput
		for j := 0; j < 50; j++ {
			output.Reset()
			if err := proc.Step(nil, &output); err != nil {
				t.Fatalf("process %d step %d: %v", i, j, err)
			}
			if output.Status() == process.StepDone || output.Status() == process.StepIdle {
				break
			}
		}
	}

	elapsed := time.Since(startTime)

	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	totalCoroutines := processCount * coroutinesPerProcess
	t.Logf("Created %d processes with %d total coroutines in %v", processCount, totalCoroutines, elapsed)
	t.Logf("Memory used: %d MB (%.1f KB per process)",
		(m2.Alloc-m1.Alloc)/(1024*1024), float64(m2.Alloc-m1.Alloc)/float64(processCount)/1024)

	// Close all
	for _, proc := range processes {
		proc.Close()
	}

	runtime.GC()
	runtime.GC()
	var m3 runtime.MemStats
	runtime.ReadMemStats(&m3)

	t.Logf("After cleanup: %d MB (initial: %d MB)", m3.Alloc/(1024*1024), m1.Alloc/(1024*1024))
}

// TestStressParallelProcessCreation tests concurrent process creation.
func TestStressParallelProcessCreation(t *testing.T) {
	skipUnlessStress(t)
	const processCount = 10000
	const workers = 16

	script := `return 1 + 2`
	proto, err := lua.CompileString(script, "parallel.lua")
	if err != nil {
		t.Fatal(err)
	}

	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	startTime := time.Now()

	var wg sync.WaitGroup
	processesPerWorker := processCount / workers
	allProcesses := make([][]*Process, workers)
	errorCounts := make([]int, workers)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			procs := make([]*Process, 0, processesPerWorker)

			for i := 0; i < processesPerWorker; i++ {
				ctx, _ := ctxapi.OpenFrameContext(context.Background())
				proc := NewProcess(WithProto(proto))
				if err := proc.Init(ctx, "", nil); err != nil {
					errorCounts[workerID]++
					continue
				}
				var output process.StepOutput
				_ = proc.Step(nil, &output)
				procs = append(procs, proc)
			}
			allProcesses[workerID] = procs
		}(w)
	}

	wg.Wait()
	elapsed := time.Since(startTime)

	// Count total
	totalCreated := 0
	totalErrors := 0
	for w := 0; w < workers; w++ {
		totalCreated += len(allProcesses[w])
		totalErrors += errorCounts[w]
	}

	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	t.Logf("Parallel creation (%d workers): %d processes in %v", workers, totalCreated, elapsed)
	t.Logf("Rate: %.0f processes/sec", float64(totalCreated)/elapsed.Seconds())
	t.Logf("Errors: %d", totalErrors)
	t.Logf("Memory: %d MB", (m2.Alloc-m1.Alloc)/(1024*1024))

	// Cleanup
	for w := 0; w < workers; w++ {
		for _, proc := range allProcesses[w] {
			proc.Close()
		}
	}
}

// BenchmarkStress10K benchmarks 10K process create/step/close cycle.
func BenchmarkStress10K(b *testing.B) {
	script := `return 1`
	proto, err := lua.CompileString(script, "bench.lua")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		processes := make([]*Process, 10000)

		for j := 0; j < 10000; j++ {
			ctx, _ := ctxapi.OpenFrameContext(context.Background())
			proc := NewProcess(WithProto(proto))
			_ = proc.Init(ctx, "", nil)
			processes[j] = proc
		}

		var output process.StepOutput
		for j := 0; j < 10000; j++ {
			output.Reset()
			_ = processes[j].Step(nil, &output)
		}

		for j := 0; j < 10000; j++ {
			processes[j].Close()
		}
	}
}

// TestStressMemoryLeak runs multiple iterations to detect gradual leaks.
func TestStressMemoryLeak(t *testing.T) {
	skipUnlessStress(t)
	const iterations = 5
	const processCount = 5000

	script := `
		local data = {}
		for i = 1, 10 do
			data[i] = string.rep("x", 100)
		end
		return #data
	`
	proto, err := lua.CompileString(script, "leak.lua")
	if err != nil {
		t.Fatal(err)
	}

	// Warm up
	runtime.GC()
	runtime.GC()

	memoryAfter := make([]uint64, iterations)

	for iter := 0; iter < iterations; iter++ {
		processes := make([]*Process, processCount)

		var output process.StepOutput
		for i := 0; i < processCount; i++ {
			ctx, _ := ctxapi.OpenFrameContext(context.Background())
			proc := NewProcess(WithProto(proto))
			_ = proc.Init(ctx, "", nil)
			output.Reset()
			_ = proc.Step(nil, &output)
			processes[i] = proc
		}

		for _, proc := range processes {
			proc.Close()
		}

		runtime.GC()
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		memoryAfter[iter] = m.Alloc

		t.Logf("Iteration %d: %d MB after cleanup", iter+1, m.Alloc/(1024*1024))
	}

	// Check for growth trend
	if iterations >= 3 {
		growth := int64(memoryAfter[iterations-1]) - int64(memoryAfter[0]) //#nosec G115
		growthPerIter := float64(growth) / float64(iterations-1)
		t.Logf("Memory growth over %d iterations: %d bytes (%.0f bytes/iter)", iterations, growth, growthPerIter)

		if growthPerIter > float64(processCount*100) {
			t.Logf("WARNING: Memory appears to be growing (%.0f bytes/iteration)", growthPerIter)
		}
	}
}

// TestStressStringConcat tests string operations under load.
func TestStressStringConcat(t *testing.T) {
	skipUnlessStress(t)
	const processCount = 1000
	script := `
		local s = ""
		for i = 1, 100 do
			s = s .. tostring(i) .. ","
		end
		return #s
	`
	proto, err := lua.CompileString(script, "strings.lua")
	if err != nil {
		t.Fatal(err)
	}

	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	startTime := time.Now()

	for i := 0; i < processCount; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := NewProcess(WithProto(proto))
		if err := proc.Init(ctx, "", nil); err != nil {
			t.Fatal(err)
		}

		var output process.StepOutput
		for j := 0; j < 200; j++ {
			output.Reset()
			if err := proc.Step(nil, &output); err != nil {
				t.Fatal(err)
			}
			if output.Status() == process.StepDone {
				break
			}
		}
		proc.Close()
	}

	elapsed := time.Since(startTime)

	runtime.GC()
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	t.Logf("String concat test: %d processes in %v", processCount, elapsed)
	t.Logf("Rate: %.0f processes/sec", float64(processCount)/elapsed.Seconds())
	t.Logf("Memory after cleanup: %d MB", m2.Alloc/(1024*1024))
}

// TestStressTableOperations tests table-heavy operations.
func TestStressTableOperations(t *testing.T) {
	skipUnlessStress(t)
	const processCount = 1000
	script := `
		local tbl = {}
		for i = 1, 1000 do
			tbl[i] = {
				id = i,
				name = "item" .. tostring(i),
				value = i * 1.5
			}
		end
		local sum = 0
		for _, v in ipairs(tbl) do
			sum = sum + v.value
		end
		return sum
	`
	proto, err := lua.CompileString(script, "tables.lua")
	if err != nil {
		t.Fatal(err)
	}

	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	startTime := time.Now()

	for i := 0; i < processCount; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := NewProcess(WithProto(proto))
		if err := proc.Init(ctx, "", nil); err != nil {
			t.Fatal(err)
		}

		var output process.StepOutput
		for j := 0; j < 500; j++ {
			output.Reset()
			if err := proc.Step(nil, &output); err != nil {
				t.Fatal(err)
			}
			if output.Status() == process.StepDone {
				break
			}
		}
		proc.Close()
	}

	elapsed := time.Since(startTime)

	runtime.GC()
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	t.Logf("Table operations test: %d processes in %v", processCount, elapsed)
	t.Logf("Rate: %.0f processes/sec", float64(processCount)/elapsed.Seconds())
	t.Logf("Memory after cleanup: %d MB", m2.Alloc/(1024*1024))
}

// BenchmarkStressCreateStepClose is a combined benchmark.
func BenchmarkStressCreateStepClose(b *testing.B) {
	script := `
		local sum = 0
		for i = 1, 100 do
			sum = sum + i
		end
		return sum
	`
	proto, err := lua.CompileString(script, "bench.lua")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := NewProcess(WithProto(proto))
		_ = proc.Init(ctx, "", nil)

		var output process.StepOutput
		for {
			output.Reset()
			_ = proc.Step(nil, &output)
			if output.Status() == process.StepDone {
				break
			}
		}
		proc.Close()
	}
}

// Spawn Tests (consolidated from spawn_debug_test.go)

func TestSpawnBasic(t *testing.T) {
	script := `
		local thread = coroutine.spawn(function()
			return "child done"
		end)
		return {main = "main done", thread_type = type(thread)}
	`
	proto, err := lua.CompileString(script, "test.lua")
	if err != nil {
		t.Fatal(err)
	}

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := NewProcess(WithProto(proto))
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	maxSteps := 20
	var output process.StepOutput
	for i := 0; i < maxSteps; i++ {
		t.Logf("Step %d: threads=%d, queue=%d", i, len(proc.threads), proc.queue.Len())

		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("step %d error: %v", i, err)
		}

		t.Logf("  Status=%v", output.Status())

		if output.Status() == process.StepDone {
			t.Logf("Done at step %d! Final threads=%d", i, len(proc.threads))
			return
		}
	}
	t.Fatalf("Did not complete in %d steps", maxSteps)
}

func TestSpawnMultiple(t *testing.T) {
	script := `
		local count = 0
		for i = 1, 5 do
			coroutine.spawn(function()
				count = count + 1
			end)
		end
		-- Wait for spawns to complete
		for i = 1, 10 do
			coroutine.yield()
		end
		return count
	`
	proto, err := lua.CompileString(script, "test.lua")
	if err != nil {
		t.Fatal(err)
	}

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := NewProcess(WithProto(proto))
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	maxSteps := 100
	peakThreads := 0
	var output process.StepOutput
	for i := 0; i < maxSteps; i++ {
		if len(proc.threads) > peakThreads {
			peakThreads = len(proc.threads)
		}

		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("step %d error: %v", i, err)
		}

		if output.Status() == process.StepDone {
			t.Logf("Done at step %d, peak threads=%d", i, peakThreads)
			return
		}
	}
	t.Fatalf("Did not complete in %d steps, threads=%d", maxSteps, len(proc.threads))
}

// TestHighConcurrencyMemoryPressure tests sustained high-concurrency load
// to match production workload: 1000 concurrent workers creating processes.
func TestHighConcurrencyMemoryPressure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory pressure test in short mode")
	}

	concurrency := 1000
	duration := 5 * time.Second
	var wg sync.WaitGroup
	var created int64
	stop := make(chan struct{})

	proto, err := lua.CompileString(`return 1`, "test")
	if err != nil {
		t.Fatal(err)
	}

	runtime.GC()
	var baseline runtime.MemStats
	runtime.ReadMemStats(&baseline)

	start := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}

				ctx, _ := ctxapi.OpenFrameContext(context.Background())
				proc := NewProcess(WithProto(proto))
				_ = proc.Init(ctx, "", nil)
				var output process.StepOutput
				_ = proc.Step(nil, &output)
				proc.Close()

				atomic.AddInt64(&created, 1)
			}
		}()
	}

	time.Sleep(duration)
	close(stop)
	wg.Wait()

	elapsed := time.Since(start)
	totalCreated := atomic.LoadInt64(&created)
	rps := float64(totalCreated) / elapsed.Seconds()

	var peak runtime.MemStats
	runtime.ReadMemStats(&peak)

	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	runtime.GC()

	var afterGC runtime.MemStats
	runtime.ReadMemStats(&afterGC)

	t.Logf("Duration: %v", elapsed)
	t.Logf("Total created: %d", totalCreated)
	t.Logf("Rate: %.0f processes/sec", rps)
	t.Logf("Concurrency: %d", concurrency)
	t.Logf("")
	t.Logf("Baseline HeapAlloc: %d MB", baseline.HeapAlloc/1024/1024)
	t.Logf("Peak HeapAlloc: %d MB", peak.HeapAlloc/1024/1024)
	t.Logf("After GC HeapAlloc: %d MB", afterGC.HeapAlloc/1024/1024)
	t.Logf("Peak HeapInuse: %d MB", peak.HeapInuse/1024/1024)
	t.Logf("Peak HeapSys: %d MB", peak.HeapSys/1024/1024)
	t.Logf("Total GC cycles: %d", afterGC.NumGC-baseline.NumGC)
}

// TestHighConcurrencyWithBindings tests with core module binders like production.
func TestHighConcurrencyWithBindings(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory pressure test in short mode")
	}

	concurrency := 1000
	duration := 5 * time.Second
	var wg sync.WaitGroup
	var created int64
	stop := make(chan struct{})

	proto, err := lua.CompileString(`return 1 + 2`, "test")
	if err != nil {
		t.Fatal(err)
	}

	// Factory with core binders - like production
	factory := NewFactory(FactoryConfig{
		Proto:         proto,
		ModuleBinders: CoreBinders(),
	})

	runtime.GC()
	var baseline runtime.MemStats
	runtime.ReadMemStats(&baseline)

	start := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}

				proc, err := factory()
				if err != nil {
					continue
				}

				ctx, _ := ctxapi.OpenFrameContext(context.Background())
				_ = proc.Init(ctx, "", nil)
				var output process.StepOutput
				_ = proc.(*Process).Step(nil, &output)
				proc.Close()

				atomic.AddInt64(&created, 1)
			}
		}()
	}

	time.Sleep(duration)
	close(stop)
	wg.Wait()

	elapsed := time.Since(start)
	totalCreated := atomic.LoadInt64(&created)
	rps := float64(totalCreated) / elapsed.Seconds()

	var peak runtime.MemStats
	runtime.ReadMemStats(&peak)

	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	runtime.GC()

	var afterGC runtime.MemStats
	runtime.ReadMemStats(&afterGC)

	t.Logf("Duration: %v", elapsed)
	t.Logf("Total created: %d (with CoreBinders)", totalCreated)
	t.Logf("Rate: %.0f processes/sec", rps)
	t.Logf("Concurrency: %d", concurrency)
	t.Logf("")
	t.Logf("Baseline HeapAlloc: %d MB", baseline.HeapAlloc/1024/1024)
	t.Logf("Peak HeapAlloc: %d MB", peak.HeapAlloc/1024/1024)
	t.Logf("After GC HeapAlloc: %d MB", afterGC.HeapAlloc/1024/1024)
	t.Logf("Peak HeapInuse: %d MB", peak.HeapInuse/1024/1024)
	t.Logf("Peak HeapSys: %d MB", peak.HeapSys/1024/1024)
	t.Logf("Total GC cycles: %d", afterGC.NumGC-baseline.NumGC)
}

func TestSpawnWithCompute(t *testing.T) {
	script := `
		local results = {}
		for i = 1, 10 do
			coroutine.spawn(function()
				local sum = 0
				for j = 1, 100 do
					sum = sum + j
				end
				results[i] = sum
			end)
		end
		-- Yield to let spawned coroutines run
		for i = 1, 20 do
			coroutine.yield()
		end
		-- Verify all spawns completed: sum of 1..100 = 5050
		local valid = 0
		for i = 1, 10 do
			if results[i] == 5050 then
				valid = valid + 1
			end
		end
		return valid
	`
	proto, err := lua.CompileString(script, "test.lua")
	if err != nil {
		t.Fatal(err)
	}

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := NewProcess(WithProto(proto))
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	maxSteps := 100
	var output process.StepOutput
	for i := 0; i < maxSteps; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("step %d error: %v", i, err)
		}

		if output.Status() == process.StepDone {
			if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
				if n, ok := proc.mainTask.Yielded[0].(lua.LNumber); ok {
					if int(n) != 10 {
						t.Errorf("Expected 10 valid results, got %d", int(n))
					} else {
						t.Logf("All 10 spawned coroutines computed correctly")
					}
				}
			}
			return
		}
	}
	t.Fatalf("Did not complete in %d steps", maxSteps)
}
