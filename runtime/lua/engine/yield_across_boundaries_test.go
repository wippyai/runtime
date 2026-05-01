// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/process"
)

// completeYields drains all pending yields from the output by completing each one
// immediately. After resuming, if the process yields again, those yields are also
// completed. Returns when there are no more pending yields.
func completeYields(t *testing.T, proc *Process, output *process.StepOutput) {
	t.Helper()
	for output.Count() > 0 {
		var events []process.Event
		for _, y := range output.Yields() {
			events = append(events, process.Event{
				Type: process.EventYieldComplete,
				Tag:  y.Tag,
			})
		}
		output.Reset()
		if err := proc.Step(events, output); err != nil {
			t.Fatalf("Step (resume) failed: %v", err)
		}
		if output.Status() == process.StepDone {
			return
		}
	}
}

// runYieldScript creates a process with test_yield bound, runs the script
// stepping through all yields until done. Returns the final output.
func runYieldScript(t *testing.T, script string, maxSteps int) process.StepOutput {
	t.Helper()
	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(bindTestYield),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	var output process.StepOutput
	for i := 0; i < maxSteps; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step %d failed: %v", i, err)
		}
		if output.Status() == process.StepDone {
			return output
		}
		completeYields(t, proc, &output)
		if output.Status() == process.StepDone {
			return output
		}
	}
	t.Fatalf("did not complete in %d steps", maxSteps)
	return output
}

// TestYieldInForInLoopBody verifies that a system yield (Go function returning -1)
// inside a for...in loop body works correctly across yield/resume boundaries.
func TestYieldInForInLoopBody(t *testing.T) {
	script := `
		local sum = 0
		local items = {10, 20, 30}
		for i, v in ipairs(items) do
			test_yield(i)
			sum = sum + v
		end
		return sum
	`
	output := runYieldScript(t, script, 20)
	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}
}

// TestYieldInIteratorFunction verifies that a system yield inside the iterator
// function itself (called via TFORCALL) works correctly. The yield occurs during
// the iterator call, not in the loop body.
func TestYieldInIteratorFunction(t *testing.T) {
	script := `
		local function yielding_iter(t, i)
			i = i + 1
			if i > #t then return nil end
			test_yield(i)
			return i, t[i]
		end

		local sum = 0
		for i, v in yielding_iter, {10, 20, 30}, 0 do
			sum = sum + v
		end
		return sum
	`
	output := runYieldScript(t, script, 20)
	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}
}

// TestYieldInPcallInsideForLoop verifies that pcall(yield) inside a for loop
// correctly handles the yield/resume cycle without corrupting the call stack.
func TestYieldInPcallInsideForLoop(t *testing.T) {
	script := `
		local sum = 0
		for i = 1, 3 do
			local ok = pcall(function()
				test_yield(i)
			end)
			if ok then
				sum = sum + i
			end
		end
		return sum
	`
	output := runYieldScript(t, script, 20)
	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}
}

// TestYieldInNestedForLoops verifies that system yields inside nested for loops
// work correctly, with yields at both nesting levels.
func TestYieldInNestedForLoops(t *testing.T) {
	script := `
		local count = 0
		local outer = {1, 2, 3}
		local inner = {10, 20}
		for _, i in ipairs(outer) do
			test_yield(i)
			for _, j in ipairs(inner) do
				test_yield(j)
				count = count + 1
			end
		end
		return count
	`
	output := runYieldScript(t, script, 40)
	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}
}

// TestYieldInCoroutineResumeInsideForLoop verifies that a coroutine that system-yields
// can be resumed inside a for loop without stack corruption.
func TestYieldInCoroutineResumeInsideForLoop(t *testing.T) {
	script := `
		local co = coroutine.create(function()
			for i = 1, 3 do
				test_yield(i)
				coroutine.yield(i)
			end
		end)

		local sum = 0
		for i = 1, 3 do
			local ok, val = coroutine.resume(co)
			if ok then
				sum = sum + val
			end
		end
		return sum
	`
	output := runYieldScript(t, script, 30)
	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}
}

// TestYieldInXpcallInsideForLoop verifies xpcall with yield inside a for loop.
func TestYieldInXpcallInsideForLoop(t *testing.T) {
	script := `
		local sum = 0
		for i = 1, 3 do
			local ok, val = xpcall(function()
				test_yield(i)
				return i * 10
			end, function(err)
				return "error: " .. tostring(err)
			end)
			if ok then
				sum = sum + val
			end
		end
		return sum
	`
	output := runYieldScript(t, script, 20)
	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}
}

// TestYieldFromWrappedCoroutineIterator verifies that coroutine.wrap used as
// an iterator (common Lua pattern) works correctly with system yields inside.
func TestYieldFromWrappedCoroutineIterator(t *testing.T) {
	script := `
		local function items(list)
			return coroutine.wrap(function()
				for i, v in ipairs(list) do
					test_yield(i)
					coroutine.yield(v)
				end
			end)
		end

		local sum = 0
		for v in items({10, 20, 30}) do
			sum = sum + v
		end
		return sum
	`
	output := runYieldScript(t, script, 30)
	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}
}

// TestYieldInMetamethodCall verifies that a system yield inside a __call
// metamethod works correctly when called from a for loop.
func TestYieldInMetamethodCall(t *testing.T) {
	script := `
		local fetcher = setmetatable({}, {
			__call = function(self, id)
				test_yield(id)
				return id * 100
			end,
		})

		local sum = 0
		for i = 1, 3 do
			sum = sum + fetcher(i)
		end
		return sum
	`
	output := runYieldScript(t, script, 20)
	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}
}

// TestMultipleYieldsPerIteration verifies that multiple system yields in a single
// loop iteration work correctly.
func TestMultipleYieldsPerIteration(t *testing.T) {
	script := `
		local count = 0
		for i = 1, 3 do
			test_yield(i)
			test_yield(i * 10)
			count = count + 1
		end
		return count
	`
	output := runYieldScript(t, script, 30)
	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}
}

// TestYieldInGenericForWithPcall combines for...in iterator, pcall, and system yields.
func TestYieldInGenericForWithPcall(t *testing.T) {
	script := `
		local function yielding_iter(t, i)
			i = i + 1
			if i > #t then return nil end
			local ok = pcall(function()
				test_yield(i)
			end)
			if not ok then return nil end
			return i, t[i]
		end

		local sum = 0
		for i, v in yielding_iter, {10, 20, 30}, 0 do
			sum = sum + v
		end
		return sum
	`
	output := runYieldScript(t, script, 30)
	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}
}

// TestYieldInSpawnedCoroutineForLoop verifies that a spawned coroutine running
// a for loop with system yields works correctly in the scheduler.
func TestYieldInSpawnedCoroutineForLoop(t *testing.T) {
	script := `
		local result = 0

		coroutine.spawn(function()
			for i = 1, 3 do
				test_yield(i)
				result = result + i
			end
		end)

		-- Main thread also yields to let spawned coroutine run
		test_yield(100)

		return result
	`
	output := runYieldScript(t, script, 30)
	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}
}
