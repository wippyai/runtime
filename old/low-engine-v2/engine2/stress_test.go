package engine2

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/low-engine-v2/scheduler"
	lua "github.com/yuin/gopher-lua"
)

// TestStress10KProcesses tests creating and running 10,000 processes.
func TestStress10KProcesses(t *testing.T) {
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
		if err := proc.Start(ctx, nil); err != nil {
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

	for i, proc := range processes {
		if proc == nil {
			continue
		}
		result, err := proc.Step(nil)
		if err != nil {
			t.Fatalf("process %d step: %v", i, err)
		}
		if result.Status == scheduler.StepDone {
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
	leaked := int64(m3.Alloc) - int64(m1.Alloc)
	leakPerProcess := float64(leaked) / float64(processCount)
	t.Logf("Memory delta after cleanup: %d bytes (%.1f bytes/process)", leaked, leakPerProcess)

	if leaked > int64(processCount*1024) {
		t.Logf("WARNING: Potential memory leak detected (>1KB per process remaining)")
	}
}

// TestStress10KWithCoroutines tests 10,000 processes each spawning coroutines.
func TestStress10KWithCoroutines(t *testing.T) {
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
		if err := proc.Start(ctx, nil); err != nil {
			t.Fatalf("process %d start: %v", i, err)
		}
		processes[i] = proc

		// Run until idle or done
		for j := 0; j < 50; j++ {
			result, err := proc.Step(nil)
			if err != nil {
				t.Fatalf("process %d step %d: %v", i, j, err)
			}
			if result.Status == scheduler.StepDone || result.Status == scheduler.StepIdle {
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
				if err := proc.Start(ctx, nil); err != nil {
					errorCounts[workerID]++
					continue
				}
				proc.Step(nil)
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
			proc.Start(ctx, nil)
			processes[j] = proc
		}

		for j := 0; j < 10000; j++ {
			processes[j].Step(nil)
		}

		for j := 0; j < 10000; j++ {
			processes[j].Close()
		}
	}
}

// TestStressMemoryLeak runs multiple iterations to detect gradual leaks.
func TestStressMemoryLeak(t *testing.T) {
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

		for i := 0; i < processCount; i++ {
			ctx, _ := ctxapi.OpenFrameContext(context.Background())
			proc := NewProcess(WithProto(proto))
			proc.Start(ctx, nil)
			proc.Step(nil)
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
		growth := int64(memoryAfter[iterations-1]) - int64(memoryAfter[0])
		growthPerIter := float64(growth) / float64(iterations-1)
		t.Logf("Memory growth over %d iterations: %d bytes (%.0f bytes/iter)", iterations, growth, growthPerIter)

		if growthPerIter > float64(processCount*100) {
			t.Logf("WARNING: Memory appears to be growing (%.0f bytes/iteration)", growthPerIter)
		}
	}
}

// TestStressStringConcat tests string operations under load.
func TestStressStringConcat(t *testing.T) {
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
		if err := proc.Start(ctx, nil); err != nil {
			t.Fatal(err)
		}

		for j := 0; j < 200; j++ {
			result, err := proc.Step(nil)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status == scheduler.StepDone {
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
		if err := proc.Start(ctx, nil); err != nil {
			t.Fatal(err)
		}

		for j := 0; j < 500; j++ {
			result, err := proc.Step(nil)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status == scheduler.StepDone {
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
		proc.Start(ctx, nil)

		for {
			result, _ := proc.Step(nil)
			if result.Status == scheduler.StepDone {
				break
			}
		}
		proc.Close()
	}
}
