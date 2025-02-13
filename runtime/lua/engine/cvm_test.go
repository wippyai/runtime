package engine

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine/errors"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/yuin/gopher-lua/parse"

	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func TestCoroutineVM_Basic(t *testing.T) {
	logger := zap.NewNop()

	t.Run("spawn and step simple coroutine", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		// Load and run script that spawns a coroutine
		err = vm.StartString(context.Background(), `
			local function co()
				coroutine.yield("first")
				coroutine.yield("second")
				return "done"
			end

			coroutine.spawn(co)
		`, "test")
		if err != nil {
			t.Fatal(err)
		}

		// GetField yielded tasks
		tasks, _ := vm.Step()
		if len(tasks) != 1 {
			t.Fatalf("expected 1 yielded task, got %d", len(tasks))
		}

		// Check first yield
		task := tasks[0]
		if task.State != lua.ResumeYield {
			t.Fatal("expected task to be yielded")
		}
		vals := task.Yielded
		if len(vals) != 1 || vals[0].String() != "first" {
			t.Fatalf("unexpected yield values: %v", vals)
		}

		// Step and check the second yield
		tasks, err = vm.Step(task)
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 1 {
			t.Fatal("expected task to yield again")
		}
		vals = tasks[0].Yielded
		if len(vals) != 1 || vals[0].String() != "second" {
			t.Fatalf("unexpected yield values: %v", vals)
		}

		// Final step should complete the coroutine
		tasks, err = vm.Step(tasks[0])
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 0 {
			t.Fatal("expected coroutine to complete")
		}
	})
}

func TestCoroutineVM_ParallelTasks(t *testing.T) {
	logger := zap.NewNop()

	t.Run("multiple parallel coroutines", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.StartString(context.Background(), `
			function task1()
				coroutine.yield("task1_start")
				coroutine.yield("task1_middle")
				return "task1_done"
			end

			function task2()
				coroutine.yield("task2_start")
				return "task2_done"
			end

			coroutine.spawn(task1)
			coroutine.spawn(task2)
		`, "parallel_test")

		if err != nil {
			t.Fatal(err)
		}

		// GetField initial yielded tasks
		tasks, err := vm.Step()
		if err != nil {
			t.Fatal(err)
		}

		if len(tasks) != 2 {
			t.Fatalf("expected 2 yielded tasks, got %d", len(tasks))
		}

		// Verify first yields
		var task1, task2 *Task
		for _, task := range tasks {
			vals := task.Yielded
			if len(vals) != 1 {
				t.Fatalf("expected 1 yielded value, got %d", len(vals))
			}

			switch vals[0].String() {
			case "task1_start":
				task1 = task
			case "task2_start":
				task2 = task
			default:
				t.Fatalf("unexpected yield value: %s", vals[0].String())
			}
		}

		if task1 == nil || task2 == nil {
			t.Fatal("failed to identify both tasks")
		}

		// Step task1 to middle
		tasks, err = vm.Step(task1)
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 1 {
			t.Fatal("expected task1 to yield again")
		}
		if tasks[0].Yielded[0].String() != "task1_middle" {
			t.Fatal("unexpected yield value from task1")
		}

		// Complete task2
		tasks, err = vm.Step(task2)
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 0 {
			t.Fatal("expected task2 to complete")
		}

		// Complete task1
		tasks, err = vm.Step(task1)
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 0 {
			t.Fatal("expected task1 to complete")
		}
	})
}

func TestCoroutineVM_ErrorHandling(t *testing.T) {
	logger := zap.NewNop()

	t.Run("error in coroutine", func(t *testing.T) {
		vm, err := NewCVM(logger)

		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.StartString(context.Background(), `
			function error_task()
				coroutine.yield("start")
				error("intentional error")
			end

			coroutine.spawn(error_task)
		`, "error_test")

		if err != nil {
			t.Fatal(err)
		}

		tasks, _ := vm.Step()
		if len(tasks) != 1 {
			t.Fatalf("expected 1 yielded task, got %d", len(tasks))
		}

		task := tasks[0]
		vals := task.Yielded
		if vals[0].String() != "start" {
			t.Fatalf("unexpected yield value: %s", vals[0].String())
		}

		// Step should output in error
		_, err = vm.Step(task)
		if err == nil {
			t.Fatal("expected error from coroutine")
		}
		if !strings.Contains(err.Error(), "intentional error") {
			t.Fatalf("unexpected error message: %v", err)
		}
	})

	t.Run("cancel coroutine", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.StartString(context.Background(), `
			function long_task()
				coroutine.yield("running")
				-- Simulate long operation
				local x = 0
				for i = 1, 1000000 do
					x = x + i
				end
				return x
			end

			coroutine.spawn(long_task)
		`, "cancel_test")

		if err != nil {
			t.Fatal(err)
		}

		tasks, _ := vm.Step()
		if len(tasks) != 1 {
			t.Fatalf("expected 1 yielded task, got %d", len(tasks))
		}

		task := tasks[0]
		if err := vm.removeTask(task); err != nil {
			t.Fatal("failed to remove task")
		}

		remainingTasks := vm.GetTasks()
		if len(remainingTasks) != 0 {
			t.Fatal("expected no remaining tasks after removal")
		}
	})
}

func TestCoroutineVM_ContextPropagation(t *testing.T) {
	logger := zap.NewNop()

	t.Run("context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		vm, err := NewCVM(logger)

		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.StartString(ctx, `
			function check_ctx()
				while true do
					if coroutine.yield("check") == "stop" then
						return "stopped"
					end
				end
			end

			coroutine.spawn(check_ctx)
		`, "context_test")

		if err != nil {
			t.Fatal(err)
		}

		tasks, _ := vm.Step()
		if len(tasks) != 1 {
			t.Fatalf("expected 1 yielded task, got %d", len(tasks))
		}

		// Cancel context
		cancel()

		// Attempting to step should fail due to canceled context
		_, err = vm.Step(tasks[0])
		if err == nil {
			t.Fatal("expected error due to canceled context")
		}
		if !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("unexpected error message: %v", err)
		}
	})
}

func TestCoroutineVM_NativeCoroutines(t *testing.T) {
	logger := zap.NewNop()

	t.Run("native coroutine inside task", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.StartString(context.Background(), `
			function task_func()
				-- Spawn a native coroutine
				local co = coroutine.create(function()
					coroutine.yield("native_first")
					return "native_done"
				end)
				
				-- First resume of native coroutine
				local status, val = coroutine.resume(co)
				coroutine.yield(val) -- yields "native_first" to VM
				
				-- Second resume of native coroutine
				status, val = coroutine.resume(co)
				coroutine.yield(val) -- yields "native_done" to VM
			end

			coroutine.spawn(task_func)
		`, "test")

		if err != nil {
			t.Fatal(err)
		}

		tasks, _ := vm.Step()
		if len(tasks) != 1 {
			t.Fatalf("expected 1 yielded task, got %d", len(tasks))
		}

		task := tasks[0]
		vals := task.Yielded
		if len(vals) != 1 || vals[0].String() != "native_first" {
			t.Fatalf("unexpected first yield value: %v", vals)
		}

		tasks, err = vm.Step(task)
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 1 {
			t.Fatal("expected second yield")
		}
		vals = tasks[0].Yielded
		if len(vals) != 1 || vals[0].String() != "native_done" {
			t.Fatalf("unexpected second yield value: %v", vals)
		}
	})

	t.Run("shared coroutine between tasks", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.StartString(context.Background(), `
			-- Spawn a shared native coroutine
			shared_co = coroutine.create(function(task_name)
				for i = 1, 2 do
					local val = task_name .. "_" .. i
					coroutine.yield(val)
				end
				local final = task_name .. "_done"
				return final
			end)

			function task1()
				local status, val = coroutine.resume(shared_co, "task1")
				if not status then 
					error(val) 
				end
				coroutine.yield(val)
			end

			function task2()
				local status, val = coroutine.resume(shared_co, "task2")
				if not status then 
					error(val) 
				end
				coroutine.yield(val)
			end

			coroutine.spawn(task1)
			coroutine.spawn(task2)
		`, "test")

		if err != nil {
			t.Fatal(err)
		}

		tasks, _ := vm.Step()
		if len(tasks) != 2 {
			t.Fatalf("expected 2 yielded tasks, got %d", len(tasks))
		}

		// First task should get first value
		vals := tasks[0].Yielded
		if len(vals) != 1 {
			t.Fatalf("unexpected number of values for task1: %v", vals)
		}

		// Second task gets "task2_1" since it starts its own coroutine sequence
		vals = tasks[1].Yielded
		if len(vals) != 1 {
			t.Fatalf("unexpected number of values for task2: %v", vals)
		}
	})

	t.Run("prevent task self-resumption", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.StartString(context.Background(), `
			error_caught = false
			
			function task_func()
				local co = coroutine.create(function()
					local task = coroutine.running()
					local ok, err = pcall(function()
						coroutine.resume(task)
					end)
					error_caught = not ok
					return "after_error"
				end)
				
				local ok, val = coroutine.resume(co)
				coroutine.yield(val)
			end

			coroutine.spawn(task_func)
		`, "test")

		if err != nil {
			t.Fatal(err)
		}

		tasks, _ := vm.Step()
		task := tasks[0]
		vals := task.Yielded
		if len(vals) != 1 || vals[0].String() != "after_error" {
			t.Fatalf("unexpected yield value: %v", vals)
		}

		err = vm.StartString(context.Background(), `assert(error_caught == true)`, "verify")
		if err != nil {
			t.Fatalf("assertion failed: %v", err)
		}
	})
}

func TestCoroutineVM_ArgumentValidation(t *testing.T) {
	logger := zap.NewNop()

	t.Run("spawn with nil argument", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.StartString(context.Background(), `
			coroutine.spawn(nil)
		`, "nil_test")

		if err != nil {
			t.Fatal(err)
		}

		_, err = vm.Step()
		if err == nil || !strings.Contains(err.Error(), "requires a function argument") {
			t.Fatal("expected error when spawning with nil argument")
		}
	})

	t.Run("spawn with non-function argument", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		testCases := []struct {
			name  string
			value string
		}{
			{"string", `"not a function"`},
			{"number", "42"},
			{"table", "{}"},
			{"boolean", "true"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				script := fmt.Sprintf(`
					coroutine.spawn(%s)
				`, tc.value)

				err := vm.StartString(context.Background(), script, tc.name)
				if err != nil {
					t.Fatal(err)
				}

				_, err = vm.Step()
				if err == nil || !strings.Contains(err.Error(), "requires a function argument") {
					t.Fatal("expected error when spawning with non-function argument")
				}
			})
		}
	})

	t.Run("spawn with missing argument", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.StartString(context.Background(), `
			coroutine.spawn()
		`, "missing_arg_test")

		if err != nil {
			t.Fatal(err)
		}

		_, err = vm.Step()
		if err == nil || !strings.Contains(err.Error(), "requires a function argument") {
			t.Fatal("expected error when spawning with no arguments")
		}
	})

	t.Run("spawn with extra arguments", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.StartString(context.Background(), `
			local function fn()
				coroutine.yield("ok")
			end
			
			-- Extra arguments should be ignored
			coroutine.spawn(fn, "extra1", "extra2")
		`, "extra_args_test")

		if err != nil {
			t.Fatal(err)
		}

		tasks, err := vm.Step()
		if err != nil {
			t.Fatal(err)
		}

		if len(tasks) != 1 {
			t.Fatalf("expected 1 yielded task, got %d", len(tasks))
		}

		vals := tasks[0].Yielded
		if len(vals) != 1 || vals[0].String() != "ok" {
			t.Fatalf("unexpected yield values: %v", vals)
		}
	})

	t.Run("spawn upvalue function", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.StartString(context.Background(), `
			local x = "captured"
			local function fn()
				coroutine.yield(x)
			end
			coroutine.spawn(fn)
		`, "upvalue_test")

		if err != nil {
			t.Fatal(err)
		}

		tasks, err := vm.Step()
		if err != nil {
			t.Fatal(err)
		}

		if len(tasks) != 1 {
			t.Fatalf("expected 1 yielded task, got %d", len(tasks))
		}

		vals := tasks[0].Yielded
		if len(vals) != 1 || vals[0].String() != "captured" {
			t.Fatalf("unexpected yield values: %v", vals)
		}
	})
}

func TestCoroutineVM_AdditionalCoverage(t *testing.T) {
	logger := zap.NewNop()

	t.Run("resume value", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.StartString(context.Background(), `
			function test_resume_value()
				local val = coroutine.yield("first")
				coroutine.yield("got " .. tostring(val))
			end
			coroutine.spawn(test_resume_value)
		`, "test")

		if err != nil {
			t.Fatal(err)
		}

		tasks, _ := vm.Step()
		if len(tasks) != 1 {
			t.Fatalf("expected 1 yielded task, got %d", len(tasks))
		}

		task := tasks[0]
		if task.State != lua.ResumeYield {
			t.Fatal("expected task to be yielded")
		}

		// Test SetResumeValues
		task.Resumed = []lua.LValue{lua.LString("test_value")}
		tasks, err = vm.Step(task)
		if err != nil {
			t.Fatal(err)
		}

		if len(tasks) != 1 {
			t.Fatal("expected task to yield again")
		}
		vals := tasks[0].Yielded
		if len(vals) != 1 || vals[0].String() != "got test_value" {
			t.Fatalf("unexpected yield values: %v", vals)
		}
	})

	t.Run("step non-yielded task", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.StartString(context.Background(), `
			function completed_task()
				return "done"
			end
			coroutine.spawn(completed_task)
		`, "test")

		if err != nil {
			t.Fatal(err)
		}

		tasks, _ := vm.Step()
		if len(tasks) != 0 {
			t.Fatal("expected no yielded tasks")
		}

		// Try to step a completed task
		newTasks, err := vm.Step(tasks...)
		if err != nil {
			t.Fatal(err)
		}
		if len(newTasks) != 0 {
			t.Fatal("expected no tasks after stepping completed task")
		}
	})

	t.Run("resume error", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.StartString(context.Background(), `
			function error_on_resume()
				local val = coroutine.yield("first")
				error("resume error")
			end
			coroutine.spawn(error_on_resume)
		`, "test")

		if err != nil {
			t.Fatal(err)
		}

		tasks, _ := vm.Step()
		if len(tasks) != 1 {
			t.Fatalf("expected 1 yielded task, got %d", len(tasks))
		}

		task := tasks[0]
		_, err = vm.Step(task)
		if err == nil {
			t.Fatal("expected error when resuming task")
		}
		if !strings.Contains(err.Error(), "resume error") {
			t.Fatalf("unexpected error message: %v", err)
		}
	})

	t.Run("remove non-existent task", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		// Spawn a dummy task that's not in the VM
		dummyTask := &Task{}
		err = vm.removeTask(dummyTask)
		if err == nil {
			t.Fatal("expected error when removing non-existent task")
		}
		if !strings.Contains(err.Error(), "task not found") {
			t.Fatalf("unexpected error message: %v", err)
		}
	})

	t.Run("create coroutine error", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.StartString(context.Background(), `
			-- Spawn a function that will error when called
			local badFunc = function()
				error("coroutine creation error")
			end
			coroutine.spawn(badFunc)
		`, "test")
		require.NoError(t, err)

		_, err = vm.Step()
		if err == nil {
			t.Fatal("expected error when creating coroutine")
		}

		if !strings.Contains(err.Error(), "coroutine creation error") {
			t.Fatalf("unexpected error message: %v", err)
		}
	})
}

func TestCoroutineVM_StatusAndWrap(t *testing.T) {
	logger := zap.NewNop()

	t.Run("status with spawned coroutines", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.StartString(context.Background(), `
			-- Spawn a task that will check statuses
			local status_results = {}
			
			function check_status()
				local inner = coroutine.create(function()
					coroutine.yield("inner")
				end)
				
				-- Check initial status
				status_results[1] = coroutine.status(inner)
				
				-- Resume and check again
				coroutine.resume(inner)
				status_results[2] = coroutine.status(inner)
				
				-- Complete and check final status
				coroutine.resume(inner)
				status_results[3] = coroutine.status(inner)
				
				coroutine.yield(status_results)
			end

			coroutine.spawn(check_status)
		`, "status_test")

		if err != nil {
			t.Fatal(err)
		}

		tasks, _ := vm.Step()
		if len(tasks) != 1 {
			t.Fatalf("expected 1 yielded task, got %d", len(tasks))
		}

		vals := tasks[0].Yielded
		if len(vals) != 1 {
			t.Fatal("expected yielded status results")
		}

		results := vals[0].(*lua.LTable)
		if results.RawGetInt(1).String() != "suspended" ||
			results.RawGetInt(2).String() != "suspended" ||
			results.RawGetInt(3).String() != "dead" {
			t.Fatal("unexpected status progression")
		}
	})

	t.Run("wrap interaction with spawn", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.StartString(context.Background(), `
			-- Spawn a vm coroutine
			local vm = coroutine.wrap(function()
				coroutine.yield("from_wrap")
				return "wrap_done"
			end)

			-- Try to spawn the vm function
			coroutine.spawn(vm)
		`, "wrap_test")

		if err != nil {
			t.Fatal(err)
		}

		// The error should occur during Step() when we try to actually spawn
		_, err = vm.Step()
		if err == nil {
			t.Fatal("expected error when spawning vm coroutine")
		}

		if !strings.Contains(err.Error(), "cannot spawn vm coroutines") {
			t.Fatalf("unexpected error message: %v", err)
		}
	})
}

func TestCoroutineVM_SharedBuffer(t *testing.T) {
	logger := zap.NewNop()

	t.Run("writer and flusher using shared buffer", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.StartString(context.Background(), `	
			-- Shared State
			shared_buffer = {
				data = {},
				size = 0
			}

			-- Writer coroutine that accepts input values
			function writer()
				local values_written = 0
				
				while values_written < 5 do
					-- wait for input value
					local value = coroutine.yield("ready_for_input")
					
					-- Add to shared buffer
					table.insert(shared_buffer.data, value)
					shared_buffer.size = #shared_buffer.data
					values_written = values_written + 1
					
					-- Report progress
					coroutine.yield("wrote:" .. value .. ", size:" .. shared_buffer.size)
				end
				
				return "writer_done"
			end

			-- Flusher that reads accumulated values
			function flusher()
				while true do
					-- wait until signaled to wait	
					local cmd = coroutine.yield("waiting")
					
					if cmd == "wait" then
						-- Read all data
						local output = table.concat(shared_buffer.data, ", ")
						local count = #shared_buffer.data
						
						-- Clear buffer
						shared_buffer.data = {}
						shared_buffer.size = 0
						
						coroutine.yield("flushed:" .. output)
						return "flusher_done"
					end
				end
			end

			coroutine.spawn(writer)
			coroutine.spawn(flusher)
		`, "buffer_test")

		if err != nil {
			t.Fatal(err)
		}

		// GetField initial tasks
		tasks, _ := vm.Step()
		if len(tasks) != 2 {
			t.Fatalf("expected 2 yielded tasks, got %d", len(tasks))
		}

		// Identify writer and flusher tasks
		var writerTask, flusherTask *Task
		for _, task := range tasks {
			vals := task.Yielded
			if len(vals) != 1 {
				t.Fatalf("expected 1 yielded value, got %d", len(vals))
			}

			val := vals[0].String()
			switch val {
			case "ready_for_input":
				writerTask = task
			case "waiting":
				flusherTask = task
			default:
				t.Fatalf("unexpected yield value: %s", val)
			}
		}

		if writerTask == nil || flusherTask == nil {
			t.Fatal("failed to identify both tasks")
		}

		// Write 5 values
		testValues := []string{"val1", "val2", "val3", "val4", "val5"}

		for _, val := range testValues {
			// send value to writer
			writerTask.Resumed = []lua.LValue{lua.LString(val)}
			tasks, err = vm.Step(writerTask)
			if err != nil {
				t.Fatal(err)
			}
			if len(tasks) != 1 {
				t.Fatal("expected writer to yield after write")
			}

			// Verify write confirmation
			writeResult := tasks[0].Yielded[0].String()
			if !strings.Contains(writeResult, "wrote:"+val) {
				t.Fatalf("unexpected write output: %v", writeResult)
			}

			// GetField layerNotify for next value
			tasks, err = vm.Step(tasks[0])
			if err != nil {
				t.Fatal(err)
			}
			if len(tasks) == 1 {
				writerTask = tasks[0]
				writeStatus := writerTask.Yielded[0].String()
				if writeStatus != "ready_for_input" {
					t.Fatalf("expected writer layerNotify for input, got: %v", writeStatus)
				}
			}
		}

		// Trigger wait
		flusherTask.Resumed = []lua.LValue{lua.LString("wait")}
		tasks, err = vm.Step(flusherTask)
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 1 {
			t.Fatal("expected flusher to yield output")
		}

		// Verify flushed data
		flushResult := tasks[0].Yielded[0].String()
		expectedResult := "flushed:val1, val2, val3, val4, val5"
		if flushResult != expectedResult {
			t.Fatalf("unexpected wait output: got %q, want %q", flushResult, expectedResult)
		}

		// Complete flusher
		flusherTask = tasks[0]
		tasks, err = vm.Step(flusherTask)
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 0 {
			t.Fatal("expected flusher to complete")
		}
	})
}

func TestCoroutineVM_NestedSpawn(t *testing.T) {
	logger := zap.NewNop()

	t.Run("spawn from within coroutine", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.StartString(context.Background(), `
			function child()
				coroutine.yield("child_running")
				return "child_done"
			end

			function parent()
				coroutine.yield("parent_start")
				coroutine.spawn(child)
				coroutine.yield("parent_spawned")
				return "parent_done"
			end

			coroutine.spawn(parent)
		`, "nested_spawn_test")

		if err != nil {
			t.Fatal(err)
		}

		tasks, _ := vm.Step()
		if len(tasks) != 1 {
			t.Fatal("expected 1 initial task")
		}

		parentTask := tasks[0]
		if parentTask.Yielded[0].String() != "parent_start" {
			t.Fatal("unexpected parent initial yield")
		}

		// Step parent to spawn child
		tasks, err = vm.Step(parentTask)
		if err != nil {
			t.Fatal(err)
		}

		if len(tasks) != 2 {
			t.Fatal("unexpected yield after spawn")
		}

		if tasks[0].Yielded[0].String() != "parent_spawned" {
			t.Fatal("unexpected parent yield after spawn")
		}

		if tasks[1].Yielded[0].String() != "child_running" {
			t.Fatal("unexpected child initial yield")
		}

		// Check all yielded tasks - should include child
		allTasks := vm.GetTasks()
		if len(allTasks) != 2 {
			t.Fatal("expected both parent and child tasks")
		}

		// Verify child task
		var childTask *Task
		for _, task := range allTasks {
			vals := task.Yielded
			if vals[0].String() == "child_running" {
				childTask = task
				break
			}
		}
		if childTask == nil {
			t.Fatal("child task not found")
		}

		// Complete child task
		tasks, err = vm.Step(childTask)
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 0 {
			t.Fatal("expected child to complete")
		}

		// Complete parent task
		tasks, err = vm.Step(parentTask)
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 0 {
			t.Fatal("expected parent to complete")
		}
	})

	t.Run("multiple nested spawns", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.StartString(context.Background(), `
			function leaf()
				coroutine.yield("leaf")
				return "leaf_done"
			end

			function middle()
				coroutine.yield("middle_start")
				coroutine.spawn(leaf)
				coroutine.spawn(leaf)
				coroutine.yield("middle_spawned")
				return "middle_done"
			end

			function root()
				coroutine.yield("root_start")
				coroutine.spawn(middle)
				coroutine.spawn(middle)
				coroutine.yield("root_spawned")
				return "root_done"
			end

			coroutine.spawn(root)
		`, "multi_nested_test")

		if err != nil {
			t.Fatal(err)
		}

		// Step through and verify task hierarchy
		tasks, _ := vm.Step()
		if len(tasks) != 1 {
			t.Fatal("expected 1 root task")
		}

		rootTask := tasks[0]
		if rootTask.Yielded[0].String() != "root_start" {
			t.Fatal("unexpected root initial yield")
		}

		// Step root to spawn middle tasks
		tasks, err = vm.Step(rootTask)
		if err != nil {
			t.Fatal(err)
		}

		if len(tasks) != 3 {
			t.Fatal("missing tasks")
		}

		if tasks[0].Yielded[0].String() != "root_spawned" {
			t.Fatal("unexpected root yield after spawn")
		}

		if tasks[1].Yielded[0].String() != "middle_start" {
			t.Fatal("unexpected middle initial yield")
		}

		if tasks[2].Yielded[0].String() != "middle_start" {
			t.Fatal("unexpected middle initial yield")
		}

		// Should now have root + 2 middle tasks
		allTasks := vm.GetTasks()
		if len(allTasks) != 3 {
			t.Fatal("expected root + 2 middle tasks")
		}

		// Step middle tasks to spawn leaves
		var middleTasks []*Task
		for _, task := range allTasks {
			vals := task.Yielded
			if vals[0].String() == "middle_start" {
				middleTasks = append(middleTasks, task)
			}
		}

		if len(middleTasks) != 2 {
			t.Fatal("expected 2 middle tasks")
		}

		// Step both middle tasks
		tasks, err = vm.Step(middleTasks...)
		if err != nil {
			t.Fatal(err)
		}

		// Verify leaf tasks:2 middle + 4 leaf
		leafCount := 0
		for _, task := range tasks {
			if task.Yielded[0].String() == "leaf" {
				leafCount++
			}
		}
		if leafCount != 4 {
			t.Fatal("expected 4 leaf tasks")
		}
	})
}

func TestCoroutineVM_MonitorStatus(t *testing.T) {
	logger := zap.NewNop()

	t.Run("monitor another coroutine's status", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.StartString(context.Background(), `
			status_log = {}
			target_thread = nil
			yield_values = {}
			
			function target_task()
				local val1 = coroutine.yield("target_running")
				table.insert(yield_values, val1 or "nil")
				
				local val2 = coroutine.yield("target_still_running")
				table.insert(yield_values, val2 or "nil")
				
				return "target_complete"
			end
			
			function monitor_task()
				if target_thread == nil then
					error("target thread not available")
				end
				
				-- Initial status check (after first yield)
				table.insert(status_log, {phase = "initial", status = coroutine.status(target_thread)})
				coroutine.yield("monitor_check1")
				
				-- Status check while target is still at first yield
				table.insert(status_log, {phase = "second", status = coroutine.status(target_thread)})
				coroutine.yield("monitor_check2")
				
				-- Status check while target is at second yield
				table.insert(status_log, {phase = "pre_complete", status = coroutine.status(target_thread)})
				coroutine.yield("monitor_check3")
				
				-- Final check after target completes
				table.insert(status_log, {phase = "post_complete", status = coroutine.status(target_thread)})
				return "monitor_done"
			end
			
			target_thread = coroutine.spawn(target_task)
			coroutine.spawn(monitor_task)
		`, "monitor_test")

		if err != nil {
			t.Fatal(err)
		}

		tasks, _ := vm.Step()
		if len(tasks) != 2 {
			t.Fatal("expected 2 initial tasks")
		}

		var targetTask, monitorTask *Task
		for _, task := range tasks {
			vals := task.Yielded
			switch vals[0].String() {
			case "target_running":
				targetTask = task
			case "monitor_check1":
				monitorTask = task
			}
		}

		if targetTask == nil || monitorTask == nil {
			t.Fatal("failed to identify both tasks")
		}

		// First monitor observation (while target is at first yield)
		tasks, err = vm.Step(monitorTask)
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 1 {
			t.Fatal("expected monitor to yield")
		}
		monitorTask = tasks[0]

		// Move target to second yield
		targetTask.Resumed = []lua.LValue{lua.LString("resume1")}
		tasks, err = vm.Step(targetTask)
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 1 {
			t.Fatal("expected target to yield")
		}
		targetTask = tasks[0]

		// Second monitor observation
		tasks, err = vm.Step(monitorTask)
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 1 {
			t.Fatal("expected monitor to yield")
		}
		monitorTask = tasks[0]

		// Complete target
		targetTask.Resumed = []lua.LValue{lua.LString("resume2")}
		tasks, err = vm.Step(targetTask)
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 0 {
			t.Fatal("expected target to complete")
		}

		// Final monitor observations
		tasks, err = vm.Step(monitorTask)
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) == 1 {
			monitorTask = tasks[0]
			_, err = vm.Step(monitorTask)
			if err != nil {
				t.Fatal(err)
			}
		}

		// Verify status transitions and resume values
		err = vm.StartString(context.Background(), `
			assert(#status_log == 4, string.format("expected 4 status entries, got %d", #status_log))
			
			assert(status_log[1].phase == "initial" and status_log[1].status == "suspended", 
				"initial status should be suspended")
			
			assert(status_log[2].phase == "second" and status_log[2].status == "suspended", 
				"second status should be suspended")
			
			assert(status_log[3].phase == "pre_complete" and status_log[3].status == "suspended", 
				"pre-completion status should be suspended")
			
			assert(status_log[4].phase == "post_complete" and status_log[4].status == "dead", 
				"post-completion status should be dead")

			assert(#yield_values == 2, string.format("expected 2 resume values, got %d", #yield_values))
			assert(yield_values[1] == "resume1", "unexpected first resume value")
			assert(yield_values[2] == "resume2", "unexpected second resume value")
		`, "verify")
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("monitor invalid thread", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.StartString(context.Background(), `
			local ok, err = pcall(function()
				coroutine.status(nil)
			end)
			assert(not ok, "expected error when checking status of nil")
			
			ok, err = pcall(function()
				coroutine.status(42)
			end)
			assert(not ok, "expected error when checking status of number")
			
			ok, err = pcall(function()
				coroutine.status(function() end)
			end)
			assert(not ok, "expected error when checking status of function")
		`, "invalid_thread_test")

		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestCoroutineVM_ClosedCoroutines(t *testing.T) {
	logger := zap.NewNop()

	t.Run("coroutines removed after immediate completion", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.StartString(context.Background(), `
			completed_count = 0
			
			function test_cleanup()
				completed_count = completed_count + 1
				return "done"
			end

			-- Spawn multiple coroutines
			for i = 1, 3 do
				coroutine.spawn(test_cleanup)
			end
		`, "cleanup_test")

		if err != nil {
			t.Fatal(err)
		}

		_, err = vm.Step() // initial tick
		if err != nil {
			t.Fatal(err)
		}

		// Check the initial task count
		if len(vm.tasks) != 0 {
			t.Fatalf("expected all tasks immediately done")
		}
	})

	t.Run("coroutines removed after error", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.StartString(context.Background(), `
			function error_test()
				error("test error")
			end
	
			coroutine.spawn(error_test)
		`, "error_test")

		if err != nil {
			t.Fatal(err)
		}

		if len(vm.tasks) != 1 {
			t.Fatal("expected 1 initial task")
		}

		// Step should output in error but cleanup task
		_, err = vm.Step(vm.tasks...)
		if err == nil {
			t.Fatal("expected error from coroutine")
		}

		// Verify task was removed
		if len(vm.tasks) != 0 {
			t.Fatal("expected task to be removed after error")
		}

		if len(vm.GetTasks()) != 0 {
			t.Fatal("GetTasks should return empty after error")
		}
	})
}

type customValue struct {
	value string
}

func (cv *customValue) String() string {
	return cv.value
}

func (cv *customValue) Type() lua.LValueType {
	return lua.LTUserData
}

func TestCoroutineVM_CustomValue(t *testing.T) {
	logger := zap.NewNop()

	t.Run("yield custom value type directly", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		// Register a function that creates our custom value
		vm.vm.state.SetGlobal("createCustomValue", vm.vm.state.NewFunction(func(l *lua.LState) int {
			val := &customValue{value: "custom_data"}
			l.Push(val)
			return 1
		}))

		err = vm.StartString(context.Background(), `
			function custom_task()
				local custom = createCustomValue()
				coroutine.yield(custom)
				return "done"
			end

			coroutine.spawn(custom_task)
		`, "custom_value_test")

		if err != nil {
			t.Fatal(err)
		}

		// Initial step to start coroutine
		tasks, err := vm.Step()
		if err != nil {
			t.Fatal(err)
		}

		if len(tasks) != 1 {
			t.Fatalf("expected 1 yielded task, got %d", len(tasks))
		}

		// Verify yielded value
		task := tasks[0]
		if len(task.Yielded) != 1 {
			t.Fatalf("expected 1 yielded value, got %d", len(task.Yielded))
		}

		// Check if the yielded value is our custom type
		customVal, ok := task.Yielded[0].(*customValue)
		if !ok {
			t.Fatalf("expected customValue, got %T", task.Yielded[0])
		}

		if customVal.value != "custom_data" {
			t.Fatalf("expected custom_data, got %s", customVal.value)
		}

		// Complete the coroutine
		tasks, err = vm.Step(task)
		if err != nil {
			t.Fatal(err)
		}

		if len(tasks) != 0 {
			t.Fatal("expected coroutine to complete")
		}
	})

	t.Run("yield custom value from function", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		// Register a function that creates our custom value
		vm.vm.state.SetGlobal("createCustomValue", vm.vm.state.NewFunction(func(l *lua.LState) int {
			l.Push(&customValue{value: "custom_data"})
			return -1
		}))

		err = vm.StartString(context.Background(), `
			function custom_task()
				createCustomValue()
				return "done"
			end

			coroutine.spawn(custom_task)
		`, "custom_value_test")

		if err != nil {
			t.Fatal(err)
		}

		// Initial step to start coroutine
		tasks, err := vm.Step()
		if err != nil {
			t.Fatal(err)
		}

		if len(tasks) != 1 {
			t.Fatalf("expected 1 yielded task, got %d", len(tasks))
		}

		// Verify yielded value
		task := tasks[0]
		if len(task.Yielded) != 1 {
			t.Fatalf("expected 1 yielded value, got %d", len(task.Yielded))
		}

		// Check if the yielded value is our custom type
		customVal, ok := task.Yielded[0].(*customValue)
		if !ok {
			t.Fatalf("expected customValue, got %T", task.Yielded[0])
		}

		if customVal.value != "custom_data" {
			t.Fatalf("expected custom_data, got %s", customVal.value)
		}

		// Complete the coroutine
		tasks, err = vm.Step(task)
		if err != nil {
			t.Fatal(err)
		}

		if len(tasks) != 0 {
			t.Fatal("expected coroutine to complete")
		}
	})

	t.Run("yield custom value in root coroutine", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		// Register a function that creates our custom value
		vm.vm.state.SetGlobal("createCustomValue", vm.vm.state.NewFunction(func(l *lua.LState) int {
			l.Push(&customValue{value: "custom_data"})
			return -1
		}))

		err = vm.StartString(context.Background(), `
			createCustomValue()
		`, "custom_value_test")

		if err != nil {
			t.Fatal(err)
		}

		// Initial step to start coroutine
		tasks, err := vm.Step()
		if err != nil {
			t.Fatal(err)
		}

		if len(tasks) != 1 {
			t.Fatalf("expected 1 yielded task, got %d", len(tasks))
		}

		// Verify yielded value
		task := tasks[0]
		if len(task.Yielded) != 1 {
			t.Fatalf("expected 1 yielded value, got %d", len(task.Yielded))
		}

		// Check if the yielded value is our custom type
		customVal, ok := task.Yielded[0].(*customValue)
		if !ok {
			t.Fatalf("expected customValue, got %T", task.Yielded[0])
		}

		if customVal.value != "custom_data" {
			t.Fatalf("expected custom_data, got %s", customVal.value)
		}

		// Complete the coroutine
		tasks, err = vm.Step(task)
		if err != nil {
			t.Fatal(err)
		}

		if len(tasks) != 0 {
			t.Fatal("expected coroutine to complete")
		}
	})

	t.Run("yield custom value in root coroutine with args", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		// Register a function that creates our custom value
		vm.vm.state.SetGlobal("createCustomValue", vm.vm.state.NewFunction(func(l *lua.LState) int {
			l.Push(&customValue{value: "custom_data"})
			return -1
		}))

		err = vm.StartString(context.Background(), `
			createCustomValue("arg1")
		`, "custom_value_test")

		if err != nil {
			t.Fatal(err)
		}

		// Initial step to start coroutine
		tasks, err := vm.Step()
		if err != nil {
			t.Fatal(err)
		}

		if len(tasks) != 1 {
			t.Fatalf("expected 1 yielded task, got %d", len(tasks))
		}

		// Verify yielded value
		task := tasks[0]
		if len(task.Yielded) != 2 {
			t.Fatalf("expected 2 yielded value, got %d", len(task.Yielded))
		}

		// Check if the yielded value is our custom type
		arg1, ok := task.Yielded[0].(lua.LString)
		if !ok {
			t.Fatalf("expected customValue, got %T", task.Yielded[0])
		}

		if arg1.String() != "arg1" {
			t.Fatalf("expected arg1, got %v", arg1)
		}

		// Check if the yielded value is our custom type
		customVal, ok := task.Yielded[1].(*customValue)
		if !ok {
			t.Fatalf("expected customValue, got %T", task.Yielded[0])
		}

		if customVal.value != "custom_data" {
			t.Fatalf("expected custom_data, got %s", customVal.value)
		}

		// Complete the coroutine
		tasks, err = vm.Step(task)
		if err != nil {
			t.Fatal(err)
		}

		if len(tasks) != 0 {
			t.Fatal("expected coroutine to complete")
		}
	})
}

func BenchmarkCoroutineVM(b *testing.B) {
	logger := zap.NewNop()

	b.Run("send_message", func(b *testing.B) {
		ctx := context.Background()
		vm, err := NewCVM(logger)
		require.NoError(b, err)

		defer vm.Close()

		script := `
			function echo()
				while true do
					local msg = coroutine.yield("layerNotify")
				end
			end

			coroutine.spawn(echo)
		`

		err = vm.StartString(ctx, script, "bench")
		if err != nil {
			b.Fatal(err)
		}
		_, err = vm.Step()
		if err != nil {
			b.Fatal(err)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			tasks := vm.GetTasks()
			if len(tasks) != 1 {
				b.Fatal("expected 1 yielded task")
			}

			task := tasks[0]
			if len(task.Yielded) > 0 && task.Yielded[0].String() == "layerNotify" {
				// send a test message
				task.Resumed = []lua.LValue{lua.LString("ping")}
				_, err = vm.Step(task)
				if err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}

func TestCoroutineVM_Mount(t *testing.T) {
	logger := zap.NewNop()

	t.Run("mount and execute coroutine function", func(t *testing.T) {
		// Spawn first VM and compile function
		vm1, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm1.Close()

		script := `
            function test()
                coroutine.yield("hello")
                local val = coroutine.yield("world")
                return "done: " .. val
            end
        `

		// Parse and compile the script
		chunk, err := parse.Parse(strings.NewReader(script), "test")
		if err != nil {
			t.Fatal(err)
		}

		proto, err := lua.Compile(chunk, "test")
		if err != nil {
			t.Fatal(err)
		}

		// Mount the function in first VM
		if err := vm1.Mount(proto, "test"); err != nil {
			t.Fatal(err)
		}

		// Spawn second VM and mount same function
		vm2, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm2.Close()

		// Mount same function prototype in VM2
		if err := vm2.Mount(proto, "test"); err != nil {
			t.Fatal(err)
		}

		// Start coroutines in both VMs
		ch1, err := vm1.Start(context.Background(), "test")
		if err != nil {
			t.Fatal(err)
		}

		ch2, err := vm2.Start(context.Background(), "test")
		if err != nil {
			t.Fatal(err)
		}

		// Process VM1
		tasks, err := vm1.Step()
		if err != nil {
			t.Fatal(err)
		}

		for len(tasks) > 0 {
			var nextTasks []*Task
			for _, task := range tasks {
				switch task.Yielded[0].String() {
				case "hello":
					task.Resumed = []lua.LValue{}
				case "world":
					task.Resumed = []lua.LValue{lua.LString("test1")}
				default:
					t.Fatalf("unexpected yield value in VM1: %v", task.Yielded[0])
				}

				newTasks, err := vm1.Step(task)
				if err != nil {
					t.Fatal(err)
				}
				nextTasks = append(nextTasks, newTasks...)
			}
			tasks = nextTasks
		}

		// Process VM2
		tasks, err = vm2.Step()
		if err != nil {
			t.Fatal(err)
		}

		for len(tasks) > 0 {
			var nextTasks []*Task
			for _, task := range tasks {
				switch task.Yielded[0].String() {
				case "hello":
					task.Resumed = []lua.LValue{}
				case "world":
					task.Resumed = []lua.LValue{lua.LString("test2")}
				default:
					t.Fatalf("unexpected yield value in VM2: %v", task.Yielded[0])
				}

				newTasks, err := vm2.Step(task)
				if err != nil {
					t.Fatal(err)
				}
				nextTasks = append(nextTasks, newTasks...)
			}
			tasks = nextTasks
		}

		// Verify results from both VMs
		result1 := <-ch1
		if result1.Error != nil {
			t.Fatal(result1.Error)
		}
		if result1.Result[0].String() != "done: test1" {
			t.Fatalf("unexpected result from VM1: %v", result1.Result)
		}

		result2 := <-ch2
		if result2.Error != nil {
			t.Fatal(result2.Error)
		}
		if result2.Result[0].String() != "done: test2" {
			t.Fatalf("unexpected result from VM2: %v", result2.Result)
		}
	})

	t.Run("mount invalid function prototype", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		// Try to mount nil prototype
		err = vm.Mount(nil, "test")
		if err == nil {
			t.Error("expected error when mounting nil prototype")
		}

		// Try to mount with no function names
		chunk, err := parse.Parse(strings.NewReader("function test() end"), "test")
		if err != nil {
			t.Fatal(err)
		}
		proto, err := lua.Compile(chunk, "test")
		if err != nil {
			t.Fatal(err)
		}

		err = vm.Mount(proto)
		if err == nil {
			t.Error("expected error when mounting without function names")
		}
	})

	t.Run("mount and execute multiple functions", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
			function func1(arg1)
				coroutine.yield("from_func1")
				return "func1_done" .. arg1
			end
	
			function func2(arg1, arg2)
				coroutine.yield("from_func2")
				return "func2_done" .. arg2 .. arg1
			end
		`

		chunk, err := parse.Parse(strings.NewReader(script), "test")
		if err != nil {
			t.Fatal(err)
		}

		proto, err := lua.Compile(chunk, "test")
		if err != nil {
			t.Fatal(err)
		}

		// Mount both functions
		if err := vm.Mount(proto, "func1", "func2"); err != nil {
			t.Fatal(err)
		}

		// Start both coroutines
		ch1, err := vm.Start(context.Background(), "func1", lua.LString("arg1"))
		if err != nil {
			t.Fatal(err)
		}

		ch2, err := vm.Start(context.Background(), "func2", lua.LString("arg1"), lua.LString("arg2"))
		if err != nil {
			t.Fatal(err)
		}

		// Process all tasks until completion
		var nextTasks []*Task
		for {
			tasks, err := vm.Step(nextTasks...)
			if err != nil {
				t.Fatal(err)
			}
			if len(tasks) == 0 {
				break
			}

			// Verify yields from both functions
			for _, task := range tasks {
				yieldVal := task.Yielded[0].String()
				if yieldVal != "from_func1" && yieldVal != "from_func2" {
					t.Fatalf("unexpected yield value: %v", yieldVal)
				}
				task.Resumed = []lua.LValue{} // Empty resume is fine for this test
			}
			nextTasks = tasks
		}

		// Verify results
		result1 := <-ch1
		if result1.Error != nil {
			t.Fatal(result1.Error)
		}
		if result1.Result[0].String() != "func1_donearg1" {
			t.Fatalf("unexpected result from func: %v", result1.Result)
		}

		result2 := <-ch2
		if result2.Error != nil {
			t.Fatal(result2.Error)
		}
		if result2.Result[0].String() != "func2_donearg2arg1" {
			t.Fatalf("unexpected result from func2: %v", result2.Result)
		}
	})
}

func TestCoroutineVM_StartWithArguments(t *testing.T) {
	logger := zap.NewNop()

	t.Run("start with different argument types", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
			function test_args(str, num, bool, tbl)
				-- First yield returns the types
				coroutine.yield(
					type(str) .. "," ..
					type(num) .. "," ..
					type(bool) .. "," ..
					type(tbl)
				)
				
				-- Second yield returns the values as strings
				coroutine.yield(
					tostring(str) .. "," ..
					tostring(num) .. "," ..
					tostring(bool) .. "," ..
					tostring(#tbl)
				)
				
				return "args_received"
			end
		`

		// Parse and compile the script
		chunk, err := parse.Parse(strings.NewReader(script), "test")
		if err != nil {
			t.Fatal(err)
		}

		proto, err := lua.Compile(chunk, "test")
		if err != nil {
			t.Fatal(err)
		}

		// Mount the function
		if err := vm.Mount(proto, "test_args"); err != nil {
			t.Fatal(err)
		}

		// Spawn test arguments of different types
		testTable := &lua.LTable{}
		testTable.RawSetInt(1, lua.LString("item1"))
		testTable.RawSetInt(2, lua.LString("item2"))

		args := []lua.LValue{
			lua.LString("test_string"),
			lua.LNumber(42),
			lua.LBool(true),
			testTable,
		}

		// Start the function with arguments
		ch, err := vm.Start(context.Background(), "test_args", args...)
		if err != nil {
			t.Fatal(err)
		}

		// Process the coroutine
		tasks, err := vm.Step()
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 1 {
			t.Fatal("expected 1 yielded task")
		}

		// Check first yield - type checking
		typeCheck := tasks[0].Yielded[0].String()
		expectedTypes := "string,number,boolean,table"
		if typeCheck != expectedTypes {
			t.Fatalf("wrong argument types, got %q, want %q", typeCheck, expectedTypes)
		}

		// Step again to get values
		tasks, err = vm.Step(tasks[0])
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 1 {
			t.Fatal("expected 1 yielded task")
		}

		// Check second yield - value checking
		valueCheck := tasks[0].Yielded[0].String()
		expectedValues := "test_string,42,true,2"
		if valueCheck != expectedValues {
			t.Fatalf("wrong argument values, got %q, want %q", valueCheck, expectedValues)
		}

		// Complete the coroutine
		tasks, err = vm.Step(tasks[0])
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 0 {
			t.Fatal("expected coroutine to complete")
		}

		// Verify final result from channel
		result := <-ch
		if result.Error != nil {
			t.Fatal(result.Error)
		}
		if result.Result[0].String() != "args_received" {
			t.Fatalf("unexpected result: %v", result.Result)
		}
	})

	t.Run("start with no arguments", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
			function no_args()
				coroutine.yield("no_args_called")
				return "no_args_ok"
			end
		`

		// Parse and compile the script
		chunk, err := parse.Parse(strings.NewReader(script), "test")
		if err != nil {
			t.Fatal(err)
		}

		proto, err := lua.Compile(chunk, "test")
		if err != nil {
			t.Fatal(err)
		}

		// Mount the function
		if err := vm.Mount(proto, "no_args"); err != nil {
			t.Fatal(err)
		}

		ch, err := vm.Start(context.Background(), "no_args")
		if err != nil {
			t.Fatal(err)
		}

		tasks, err := vm.Step()
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 1 {
			t.Fatal("expected 1 yielded task")
		}

		// Verify function was called with no arguments
		if tasks[0].Yielded[0].String() != "no_args_called" {
			t.Fatal("function not called correctly")
		}

		// Complete the coroutine
		_, err = vm.Step(tasks[0])
		if err != nil {
			t.Fatal(err)
		}

		result := <-ch
		if result.Error != nil {
			t.Fatal(result.Error)
		}
		if result.Result[0].String() != "no_args_ok" {
			t.Fatalf("unexpected result: %v", result.Result)
		}
	})

	t.Run("start with nil arguments", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
			function nil_args(a, b)
				coroutine.yield(
					(a == nil and "nil" or "not_nil") .. "," ..
					(b == nil and "nil" or "not_nil")
				)
				return "nil_args_ok"
			end
		`

		// Parse and compile the script
		chunk, err := parse.Parse(strings.NewReader(script), "test")
		if err != nil {
			t.Fatal(err)
		}

		proto, err := lua.Compile(chunk, "test")
		if err != nil {
			t.Fatal(err)
		}

		// Mount the function
		if err := vm.Mount(proto, "nil_args"); err != nil {
			t.Fatal(err)
		}

		ch, err := vm.Start(context.Background(), "nil_args", lua.LNil, lua.LNil)
		if err != nil {
			t.Fatal(err)
		}

		tasks, err := vm.Step()
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 1 {
			t.Fatal("expected 1 yielded task")
		}

		// Verify nil arguments were handled correctly
		nilCheck := tasks[0].Yielded[0].String()
		if nilCheck != "nil,nil" {
			t.Fatalf("expected nil arguments, got %s", nilCheck)
		}

		// Complete the coroutine
		_, err = vm.Step(tasks[0])
		if err != nil {
			t.Fatal(err)
		}

		result := <-ch
		if result.Error != nil {
			t.Fatal(result.Error)
		}
		if result.Result[0].String() != "nil_args_ok" {
			t.Fatalf("unexpected result: %v", result.Result)
		}
	})
}

func TestCoroutineVM_ImmediateErrors(t *testing.T) {
	logger := zap.NewNop()

	t.Run("immediate error before yield", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.StartString(context.Background(), `
            function immediate_error()
                error("immediate error")
                -- No yield, error happens immediately
            end

            coroutine.spawn(immediate_error)
        `, "immediate_error_test")

		if err != nil {
			t.Fatal(err)
		}

		// Step should result in error
		tasks, err := vm.Step()
		if err == nil {
			t.Fatal("expected immediate error")
		}
		if !strings.Contains(err.Error(), "immediate error") {
			t.Fatalf("unexpected error message: %v", err)
		}
		if len(tasks) != 0 {
			t.Fatal("expected no tasks after error")
		}
	})

	t.Run("compile and start immediate error", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		// Parse and compile the function
		script := `
            function test_error()
                error("start error")
            end
        `

		chunk, err := parse.Parse(strings.NewReader(script), "error_test")
		if err != nil {
			t.Fatal(err)
		}

		proto, err := lua.Compile(chunk, "error_test")
		if err != nil {
			t.Fatal(err)
		}

		// Mount the function
		if err := vm.Mount(proto, "test_error"); err != nil {
			t.Fatal(err)
		}

		// Start the function and get the channel
		ch, err := vm.Start(context.Background(), "test_error")
		if err != nil {
			t.Fatal(err)
		}

		// Step should trigger the error
		_, stepErr := vm.Step()
		if stepErr == nil {
			t.Fatal("expected error from Step")
		}

		// Verify error is sent through channel
		result := <-ch
		if result.Error == nil {
			t.Fatal("expected error in task result")
		}
		if !strings.Contains(result.Error.Error(), "start error") {
			t.Fatalf("unexpected error message: %v", result.Error)
		}
	})

	t.Run("error in nested immediate function", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.StartString(context.Background(), `
            function nested_func()
                local function inner()
                    error("nested error")
                end
                inner() -- Call immediately
            end

            coroutine.spawn(nested_func)
        `, "nested_error_test")

		if err != nil {
			t.Fatal(err)
		}

		// Step should result in error
		tasks, err := vm.Step()
		if err == nil {
			t.Fatal("expected error from nested function")
		}
		if !strings.Contains(err.Error(), "nested error") {
			t.Fatalf("unexpected error message: %v", err)
		}
		if len(tasks) != 0 {
			t.Fatal("expected no tasks after error")
		}
	})
}

func TestCoroutineVM_Import(t *testing.T) {
	logger := zap.NewNop()

	t.Run("import and execute simple function", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
			function test()
				coroutine.yield("first")
				return "done"
			end
		`

		err = vm.Import(script, "test", "test")
		if err != nil {
			t.Fatal(err)
		}

		// Start the function and get output channel
		ch, err := vm.Start(context.Background(), "test")
		if err != nil {
			t.Fatal(err)
		}

		// Process task
		tasks, err := vm.Step()
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 1 {
			t.Fatal("expected 1 yielded task")
		}

		if tasks[0].Yielded[0].String() != "first" {
			t.Fatalf("unexpected yield value: %v", tasks[0].Yielded[0])
		}

		// Complete task
		_, err = vm.Step(tasks[0])
		if err != nil {
			t.Fatal(err)
		}

		result := <-ch
		if result.Error != nil {
			t.Fatal(result.Error)
		}
		if result.Result[0].String() != "done" {
			t.Fatalf("unexpected result: %v", result.Result[0])
		}
	})

	t.Run("import multiple functions", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
			function func1()
				coroutine.yield("from_func1")
				return "func1_done"
			end

			function func2()
				coroutine.yield("from_func2")
				return "func2_done"
			end
		`

		err = vm.Import(script, "test", "func1", "func2")
		if err != nil {
			t.Fatal(err)
		}

		// Test func1
		ch1, err := vm.Start(context.Background(), "func1")
		if err != nil {
			t.Fatal(err)
		}

		tasks, err := vm.Step()
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 1 || tasks[0].Yielded[0].String() != "from_func1" {
			t.Fatal("unexpected func1 yield")
		}

		_, err = vm.Step(tasks[0])
		if err != nil {
			t.Fatal(err)
		}

		result1 := <-ch1
		if result1.Error != nil || result1.Result[0].String() != "func1_done" {
			t.Fatal("unexpected func1 result")
		}

		// Test func2
		ch2, err := vm.Start(context.Background(), "func2")
		if err != nil {
			t.Fatal(err)
		}

		tasks, err = vm.Step()
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 1 || tasks[0].Yielded[0].String() != "from_func2" {
			t.Fatal("unexpected func2 yield")
		}

		_, err = vm.Step(tasks[0])
		if err != nil {
			t.Fatal(err)
		}

		result2 := <-ch2
		if result2.Error != nil || result2.Result[0].String() != "func2_done" {
			t.Fatal("unexpected func2 result")
		}
	})

	t.Run("import with no function names", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
			function test()
				return "test"
			end
		`

		err = vm.Import(script, "test")
		if err == nil {
			t.Error("expected error when importing with no function names")
		}
		if !strings.Contains(err.Error(), "no function names provided") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("import non-existent function", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
			function test()
				return "test"
			end
		`

		err = vm.Import(script, "test", "nonexistent")
		if err == nil {
			t.Error("expected error when importing non-existent function")
		}
	})

	t.Run("import with syntax error", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
			function test()
				return "missing end
		`

		err = vm.Import(script, "test", "test")
		if err == nil {
			t.Error("expected error with invalid syntax")
		}
		if !strings.Contains(err.Error(), "parse error") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("import function with upvalues", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
			local x = "captured"
			function test()
				coroutine.yield(x)
				return "done"
			end
		`

		err = vm.Import(script, "test", "test")
		if err != nil {
			t.Fatal(err)
		}

		ch, err := vm.Start(context.Background(), "test")
		if err != nil {
			t.Fatal(err)
		}

		tasks, err := vm.Step()
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 1 || tasks[0].Yielded[0].String() != "captured" {
			t.Fatal("unexpected yield value")
		}

		_, err = vm.Step(tasks[0])
		if err != nil {
			t.Fatal(err)
		}

		result := <-ch
		if result.Error != nil || result.Result[0].String() != "done" {
			t.Fatal("unexpected result")
		}
	})

	t.Run("import with nested functions", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
			function outer()
				local function inner()
					coroutine.yield("inner")
					return "inner_done"
				end
				coroutine.yield("outer")
				return inner()
			end
		`

		err = vm.Import(script, "test", "outer")
		if err != nil {
			t.Fatal(err)
		}

		ch, err := vm.Start(context.Background(), "outer")
		if err != nil {
			t.Fatal(err)
		}

		tasks, err := vm.Step()
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 1 || tasks[0].Yielded[0].String() != "outer" {
			t.Fatal("unexpected outer yield")
		}

		tasks, err = vm.Step(tasks[0])
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 1 || tasks[0].Yielded[0].String() != "inner" {
			t.Fatal("unexpected inner yield")
		}

		_, err = vm.Step(tasks[0])
		if err != nil {
			t.Fatal(err)
		}

		result := <-ch
		if result.Error != nil || result.Result[0].String() != "inner_done" {
			t.Fatal("unexpected result")
		}
	})

	t.Run("import module style function", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
			local M = {}
			function M.test()
				coroutine.yield("module")
				return "module_done"
			end
			return M
		`

		err = vm.Import(script, "test", "test")
		if err != nil {
			t.Fatal(err)
		}

		ch, err := vm.Start(context.Background(), "test")
		if err != nil {
			t.Fatal(err)
		}

		tasks, err := vm.Step()
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 1 || tasks[0].Yielded[0].String() != "module" {
			t.Fatal("unexpected yield")
		}

		_, err = vm.Step(tasks[0])
		if err != nil {
			t.Fatal(err)
		}

		result := <-ch
		if result.Error != nil || result.Result[0].String() != "module_done" {
			t.Fatal("unexpected result")
		}
	})

	t.Run("import with shared state between functions", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		script := `
			local state = 0
			
			function increment()
				state = state + 1
				coroutine.yield(state)
				return state
			end

			function get_state()
				coroutine.yield(state)
				return state
			end
		`

		err = vm.Import(script, "test", "increment", "get_state")
		if err != nil {
			t.Fatal(err)
		}

		// Run increment
		ch1, err := vm.Start(context.Background(), "increment")
		if err != nil {
			t.Fatal(err)
		}

		tasks, err := vm.Step()
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 1 || tasks[0].Yielded[0].(lua.LNumber) != 1 {
			t.Fatal("unexpected increment yield")
		}

		_, err = vm.Step(tasks[0])
		if err != nil {
			t.Fatal(err)
		}

		result1 := <-ch1
		if result1.Error != nil || result1.Result[0].(lua.LNumber) != 1 {
			t.Fatal("unexpected increment result")
		}

		// Check state
		ch2, err := vm.Start(context.Background(), "get_state")
		if err != nil {
			t.Fatal(err)
		}

		tasks, err = vm.Step()
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 1 || tasks[0].Yielded[0].(lua.LNumber) != 1 {
			t.Fatal("unexpected get_state yield")
		}

		_, err = vm.Step(tasks[0])
		if err != nil {
			t.Fatal(err)
		}

		result2 := <-ch2
		if result2.Error != nil || result2.Result[0].(lua.LNumber) != 1 {
			t.Fatal("unexpected get_state result")
		}
	})
}

func TestCoroutineVM_GoErrorPropagation(t *testing.T) {
	logger := zap.NewNop()

	t.Run("wrapped go error propagation through resume", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		// Register a function that creates a wrapped Go error
		vm.vm.state.SetGlobal("go_produce_error", vm.vm.state.NewFunction(func(l *lua.LState) int {
			// Spawn a Go error
			goErr := fmt.Errorf("test go error")

			// Wrap it to capture Go stack trace
			wrappedErr := errors.WrapError(l, goErr, "error context")

			// Spawn userdata with error metatable
			ud := l.NewUserData()
			ud.Value = wrappedErr
			l.SetMetatable(ud, l.GetTypeMetatable("error"))

			l.Push(ud)
			return 1
		}))

		// Start a coroutine that will receive and try to use an error
		err = vm.StartString(context.Background(), `
			-- First coroutine that creates and yields a Go error
			function error_producer()
				local err = go_produce_error()  -- Call to Go function that produces error
				coroutine.yield(err)
				return "producer_done"
			end

			-- Second coroutine that receives and uses the error
			function error_consumer()
				local err = coroutine.yield("ready")  -- First yield to get error
				error(err)  -- Try to use the error, should fail
			end

			-- Spawn both coroutines
			coroutine.spawn(error_producer)
			coroutine.spawn(error_consumer)
		`, "error_test")

		if err != nil {
			t.Fatal(err)
		}

		// First Step - both coroutines should yield
		tasks, err := vm.Step()
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 2 {
			t.Fatalf("expected 2 yielded tasks, got %d", len(tasks))
		}

		// Find producer and consumer tasks
		var producerTask, consumerTask *Task
		for _, task := range tasks {
			if len(task.Yielded) == 1 {
				if ud, ok := task.Yielded[0].(*lua.LUserData); ok {
					if _, ok := ud.Value.(*errors.WrappedError); ok {
						producerTask = task
					}
				} else if task.Yielded[0].String() == "ready" {
					consumerTask = task
				}
			}
		}

		if producerTask == nil || consumerTask == nil {
			t.Fatal("failed to identify both tasks")
		}

		// Send error from producer to consumer
		consumerTask.Resumed = producerTask.Yielded

		// Step consumer - should fail with wrapped error
		tasks, err = vm.Step(consumerTask)
		if err == nil {
			t.Fatal("expected error when resuming with Go error")
		}

		// Verify error is a wrapped error
		wrappedErr := errors.GetWrappedError(err)
		if wrappedErr == nil {
			t.Fatal("expected wrapped error")
		}

		// Verify error contains both Go and Lua frames
		stack := wrappedErr.Stack()
		if !strings.Contains(stack, "error_test:4") { // Lua frame
			t.Fatal("stack trace missing Lua frames")
		}
		if !strings.Contains(stack, "go_produce_error") { // Go frame
			t.Fatal("stack trace missing Go frames")
		}
	})
}

func TestCoroutineVM_PcallErrorHandling(t *testing.T) {
	logger := zap.NewNop()

	t.Run("coroutine pcall error wrapping", func(t *testing.T) {
		vm, err := NewCVM(logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		errors.RegisterErrorsModule(vm.vm.state)

		// Register a function that creates a wrapped Go error
		vm.vm.state.SetGlobal("go_produce_error", vm.vm.state.NewFunction(func(l *lua.LState) int {
			// Spawn a Go error
			goErr := fmt.Errorf("test go error")

			// Wrap it to capture Go stack trace
			wrappedErr := errors.WrapError(l, goErr, "error context")

			// Spawn userdata with error metatable
			ud := l.NewUserData()
			ud.Value = wrappedErr
			l.SetMetatable(ud, l.GetTypeMetatable("error"))

			l.Push(ud)
			return 1
		}))

		// Start coroutines - one produces error, other handles with pcall
		err = vm.StartString(context.Background(), `
			-- First coroutine that creates and yields a Go error
			function error_producer()
				local err = go_produce_error()  -- Will be injected by CVM
				coroutine.yield(err)
				return "producer_done"
			end

			-- Second coroutine that receives error and handles with pcall
			function error_consumer()
				local err = coroutine.yield("ready")  -- First yield to get error
				
				-- Use pcall to handle the error and add our own context
				local ok, result = pcall(function()
					error(err)  -- This will fail with the original error
				end)
				
				if not ok then
					-- Add our own context by wrapping the error
					local wrapped = errors.wrap(result, "consumer wrapper")
					error(wrapped)  -- Re-raise with our additional context
				end
				
				return "consumer_done"  -- Should never reach here
			end

			-- Spawn both coroutines
			coroutine.spawn(error_producer)
			coroutine.spawn(error_consumer)
		`, "error_test")

		if err != nil {
			t.Fatal(err)
		}

		// First Step - both coroutines should yield
		tasks, err := vm.Step()
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 2 {
			t.Fatalf("expected 2 yielded tasks, got %d", len(tasks))
		}

		// Find producer and consumer tasks
		var producerTask, consumerTask *Task
		for _, task := range tasks {
			if len(task.Yielded) == 1 {
				if ud, ok := task.Yielded[0].(*lua.LUserData); ok {
					if _, ok := ud.Value.(*errors.WrappedError); ok {
						producerTask = task
					}
				} else if task.Yielded[0].String() == "ready" {
					consumerTask = task
				}
			}
		}

		if producerTask == nil || consumerTask == nil {
			t.Fatal("failed to identify both tasks")
		}

		// Send error from producer to consumer
		consumerTask.Resumed = producerTask.Yielded

		// Step consumer - should fail with wrapped error that includes both contexts
		tasks, err = vm.Step(consumerTask)
		if err == nil {
			t.Fatal("expected error when resuming with Go error")
		}
		// Verify error is a wrapped error
		wrappedErr := errors.GetWrappedError(err)
		if wrappedErr == nil {
			t.Fatal("expected wrapped error")
		}

		// Verify error message contains both contexts
		errMsg := wrappedErr.Error()
		if !strings.Contains(errMsg, "consumer wrapper") {
			t.Error("error missing consumer context")
		}
		if !strings.Contains(errMsg, "error context") {
			t.Error("error missing original context")
		}

		// Verify error stack contains all relevant frames
		stack := wrappedErr.Stack()

		requiredFrames := []string{
			"consumer wrapper", // Consumer wrapper
			"go_produce_error", // Original Go function
			"error_test:20",    // error() call
			"error_test:4",     // coroutine.yield
		}

		for _, frame := range requiredFrames {
			if !strings.Contains(stack, frame) {
				t.Errorf("stack missing required frame: %s", frame)
			}
		}

		// Verify error chain preserves both wrapper contexts
		var contexts []string
		current := wrappedErr
		for current != nil {
			if current.Context != "" {
				contexts = append(contexts, current.Context)
			}
			if next := errors.GetWrappedError(current.Unwrap()); next != nil {
				current = next
			} else {
				break
			}
		}

		expectedContexts := []string{
			"consumer wrapper",
			"error context",
		}

		if !reflect.DeepEqual(contexts, expectedContexts) {
			t.Errorf("wrong context order:\nwant: %v\ngot: %v", expectedContexts, contexts)
		}
	})
}
