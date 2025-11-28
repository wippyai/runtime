package engine2

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/low-engine-v2/scheduler"
	lua "github.com/yuin/gopher-lua"
)

func TestSpawnBasic(t *testing.T) {
	script := `
		local thread = coroutine.spawn(function()
			return "child done"
		end)
		return "main done", type(thread)
	`
	proto, err := lua.CompileString(script, "test.lua")
	if err != nil {
		t.Fatal(err)
	}

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc := NewProcess(WithProto(proto))
	if err := proc.Start(ctx, nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	maxSteps := 20
	for i := 0; i < maxSteps; i++ {
		t.Logf("Step %d: threads=%d, queue=%d", i, len(proc.threads), proc.queue.Len())

		result, err := proc.Step(nil)
		if err != nil {
			t.Fatalf("step %d error: %v", i, err)
		}

		t.Logf("  Status=%v, YieldCount=%d", result.Status, result.YieldCount)

		if result.Status == scheduler.StepDone {
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
	if err := proc.Start(ctx, nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	maxSteps := 100
	peakThreads := 0
	for i := 0; i < maxSteps; i++ {
		if len(proc.threads) > peakThreads {
			peakThreads = len(proc.threads)
		}

		result, err := proc.Step(nil)
		if err != nil {
			t.Fatalf("step %d error: %v", i, err)
		}

		if result.Status == scheduler.StepDone {
			t.Logf("Done at step %d, peak threads=%d", i, peakThreads)
			return
		}
	}
	t.Fatalf("Did not complete in %d steps, threads=%d", maxSteps, len(proc.threads))
}

func TestSpawnWithCompute(t *testing.T) {
	// Test spawn with non-blocking work
	// Spawned coroutines complete within same Step(), so peak threads=1 is expected
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
	if err := proc.Start(ctx, nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	maxSteps := 100
	for i := 0; i < maxSteps; i++ {
		result, err := proc.Step(nil)
		if err != nil {
			t.Fatalf("step %d error: %v", i, err)
		}

		if result.Status == scheduler.StepDone {
			// Check return value from main task
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
