package engine

import (
	"context"
	"fmt"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"strings"
	"testing"
)

func TestCoroutineVM_Basic(t *testing.T) {
	logger := zap.NewNop()

	t.Run("spawn and step simple coroutine", func(t *testing.T) {
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		// Load and run script that spawns a coroutine
		err = vm.PushScript(`
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

		// Get yielded tasks
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

		// Step and check second yield
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
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.PushScript(`
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

		// Get initial yielded tasks
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
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.PushScript(`
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

		// Step should result in error
		_, err = vm.Step(task)
		if err == nil {
			t.Fatal("expected error from coroutine")
		}
		if !strings.Contains(err.Error(), "intentional error") {
			t.Fatalf("unexpected error message: %v", err)
		}
	})

	t.Run("cancel coroutine", func(t *testing.T) {
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.PushScript(`
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

		remainingTasks := vm.GetYieldedTasks()
		if len(remainingTasks) != 0 {
			t.Fatal("expected no remaining tasks after removal")
		}
	})
}

func TestCoroutineVM_ContextPropagation(t *testing.T) {
	logger := zap.NewNop()

	t.Run("context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		vm, err := NewCoroutineVM(ctx, logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.PushScript(`
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

		// Attempting to step should fail due to cancelled context
		_, err = vm.Step(tasks[0])
		if err == nil {
			t.Fatal("expected error due to cancelled context")
		}
		if !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("unexpected error message: %v", err)
		}
	})
}

func TestCoroutineVM_NativeCoroutines(t *testing.T) {
	logger := zap.NewNop()

	t.Run("native coroutine inside task", func(t *testing.T) {
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.PushScript(`
			function task_func()
				-- Create a native coroutine
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
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.PushScript(`
			-- Create a shared native coroutine
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
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.PushScript(`
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

		err = vm.PushScript(`assert(error_caught == true)`, "verify")
		if err != nil {
			t.Fatal(fmt.Sprintf("assertion failed: %v", err))
		}
	})
}

func TestCoroutineVM_ArgumentValidation(t *testing.T) {
	logger := zap.NewNop()

	t.Run("spawn with nil argument", func(t *testing.T) {
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.PushScript(`
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
		vm, err := NewCoroutineVM(context.Background(), logger)
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

				err := vm.PushScript(script, tc.name)
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
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.PushScript(`
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
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.PushScript(`
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
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.PushScript(`
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

	t.Run("nil context", func(t *testing.T) {
		_, err := NewCoroutineVM(nil, logger)
		if err == nil {
			t.Fatal("expected error when creating VM with nil context")
		}
		if !strings.Contains(err.Error(), "context is required for async VMs") {
			t.Fatalf("unexpected error message: %v", err)
		}
	})

	t.Run("resume value", func(t *testing.T) {
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.PushScript(`
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
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.PushScript(`
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
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.PushScript(`
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
		tasks, err = vm.Step(task)
		if err == nil {
			t.Fatal("expected error when resuming task")
		}
		if !strings.Contains(err.Error(), "resume error") {
			t.Fatalf("unexpected error message: %v", err)
		}
	})

	t.Run("remove non-existent task", func(t *testing.T) {
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		// Create a dummy task that's not in the VM
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
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		vm.PushScript(`
			-- Create a function that will error when called
			local badFunc = function()
				error("coroutine creation error")
			end
			coroutine.spawn(badFunc)
		`, "test")

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
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.PushScript(`
			-- Create a task that will check statuses
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
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.PushScript(`
			-- Create a wrapped coroutine
			local wrapped = coroutine.wrap(function()
				coroutine.yield("from_wrap")
				return "wrap_done"
			end)

			-- Try to spawn the wrapped function
			coroutine.spawn(wrapped)
		`, "wrap_test")

		if err != nil {
			t.Fatal(err)
		}

		// The error should occur during Step() when we try to actually spawn
		_, err = vm.Step()
		if err == nil {
			t.Fatal("expected error when spawning wrapped coroutine")
		}

		if !strings.Contains(err.Error(), "cannot spawn wrapped coroutines") {
			t.Fatalf("unexpected error message: %v", err)
		}
	})
}

func TestCoroutineVM_SharedBuffer(t *testing.T) {
	logger := zap.NewNop()

	t.Run("writer and flusher using shared buffer", func(t *testing.T) {
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.PushScript(`	
			-- Shared State
			shared_buffer = {
				data = {},
				size = 0
			}

			-- Writer coroutine that accepts input values
			function writer()
				local values_written = 0
				
				while values_written < 5 do
					-- Wait for input value
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
					-- Wait until signaled to flush	
					local cmd = coroutine.yield("waiting")
					
					if cmd == "flush" then
						-- Read all data
						local result = table.concat(shared_buffer.data, ", ")
						local count = #shared_buffer.data
						
						-- Clear buffer
						shared_buffer.data = {}
						shared_buffer.size = 0
						
						coroutine.yield("flushed:" .. result)
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

		// Get initial tasks
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
			if val == "ready_for_input" {
				writerTask = task
			} else if val == "waiting" {
				flusherTask = task
			} else {
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
				t.Fatalf("unexpected write result: %v", writeResult)
			}

			// Get ready for next value
			tasks, err = vm.Step(tasks[0])
			if err != nil {
				t.Fatal(err)
			}
			if len(tasks) == 1 {
				writerTask = tasks[0]
				writeStatus := writerTask.Yielded[0].String()
				if writeStatus != "ready_for_input" {
					t.Fatalf("expected writer ready for input, got: %v", writeStatus)
				}
			}
		}

		// Trigger flush
		flusherTask.Resumed = []lua.LValue{lua.LString("flush")}
		tasks, err = vm.Step(flusherTask)
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 1 {
			t.Fatal("expected flusher to yield result")
		}

		// Verify flushed data
		flushResult := tasks[0].Yielded[0].String()
		expectedResult := "flushed:val1, val2, val3, val4, val5"
		if flushResult != expectedResult {
			t.Fatalf("unexpected flush result: got %q, want %q", flushResult, expectedResult)
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
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.PushScript(`
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
		allTasks := vm.GetYieldedTasks()
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
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.PushScript(`
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
		allTasks := vm.GetYieldedTasks()
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
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.PushScript(`
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
			tasks, err = vm.Step(monitorTask)
			if err != nil {
				t.Fatal(err)
			}
		}

		// Verify status transitions and resume values
		err = vm.PushScript(`
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
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.PushScript(`
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

	t.Run("coroutines removed after completion", func(t *testing.T) {
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.PushScript(`
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

		// Check initial task count
		if len(vm.tasks) != 4 {
			t.Fatalf("expected 4 initial tasks, got %d", len(vm.tasks))
		}

		// Step to complete all tasks
		_, err = vm.Step(vm.tasks...)
		if err != nil {
			t.Fatal(err)
		}

		// Verify tasks were removed
		if len(vm.tasks) != 0 {
			t.Fatalf("expected tasks to be removed after completion, got %d tasks", len(vm.tasks))
		}

		// Verify completion counter
		err = vm.PushScript(`assert(completed_count == 3, "not all coroutines completed")`, "verify")
		if err != nil {
			t.Fatal(err)
		}

		// Verify GetYieldedTasks also returns empty
		if len(vm.GetYieldedTasks()) != 0 {
			t.Fatal("GetYieldedTasks should return empty after completion")
		}
	})

	t.Run("coroutines removed after error", func(t *testing.T) {
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.PushScript(`
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

		// Step should result in error but cleanup task
		_, err = vm.Step(vm.tasks...)
		if err == nil {
			t.Fatal("expected error from coroutine")
		}

		// Verify task was removed
		if len(vm.tasks) != 0 {
			t.Fatal("expected task to be removed after error")
		}

		if len(vm.GetYieldedTasks()) != 0 {
			t.Fatal("GetYieldedTasks should return empty after error")
		}
	})
}

func BenchmarkCoroutineVM(b *testing.B) {
	logger := zap.NewNop()

	b.Run("send_message", func(b *testing.B) {
		ctx := context.Background()
		vm, err := NewCoroutineVM(ctx, logger)
		if err != nil {
			b.Fatal(err)
		}
		defer vm.Close()

		script := `
			function echo()
				while true do
					local msg = coroutine.yield("ready")
				end
			end

			coroutine.spawn(echo)
		`

		err = vm.PushScript(script, "bench")
		if err != nil {
			b.Fatal(err)
		}
		_, err = vm.Step()
		if err != nil {
			b.Fatal(err)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			tasks := vm.GetYieldedTasks()
			if len(tasks) != 1 {
				b.Fatal("expected 1 yielded task")
			}

			task := tasks[0]
			if len(task.Yielded) > 0 && task.Yielded[0].String() == "ready" {
				// send test message
				task.Resumed = []lua.LValue{lua.LString("ping")}
				tasks, err = vm.Step(task)
				if err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}
