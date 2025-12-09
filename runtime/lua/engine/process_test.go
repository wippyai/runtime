package engine

import (
	"context"
	"fmt"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/system/clock"
	funcpool "github.com/wippyai/runtime/system/scheduler/pool"
	lua "github.com/yuin/gopher-lua"
)

func TestProcessBasicExecution(t *testing.T) {
	script := `
		local result = 1 + 2
		return result
	`

	proc := NewProcess(WithScript(script, "test.lua"))

	// Create frame context
	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer proc.Close()

	var output process.StepOutput
	err := proc.Step(nil, &output)
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	if output.Status() != process.StepDone {
		t.Errorf("Expected StepDone, got %v", output.Status())
	}
}

func TestProcessMultipleCoroutines(t *testing.T) {
	script := `
		local result = 0

		local co1 = coroutine.spawn(function()
			result = result + 1
		end)

		local co2 = coroutine.spawn(function()
			result = result + 2
		end)

		return result
	`

	proc := NewProcess(WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer proc.Close()

	// Spawns create yield points, so we need to step until done
	var output process.StepOutput
	for i := 0; i < 10; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step %d failed: %v", i, err)
		}
		if output.Status() == process.StepDone {
			return
		}
	}
	t.Error("Did not complete in expected steps")
}

func TestResourceStoreInContext(t *testing.T) {
	// Create frame context
	ctx, fc := ctxapi.AcquireFrameContext(context.Background())

	// No store yet
	store := resource.GetStore(ctx)
	if store != nil {
		t.Error("Expected nil store before process start")
	}

	script := `return 1`
	proc := NewProcess(WithScript(script, "test.lua"))

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer proc.Close()

	// Now store should exist
	store = resource.GetStore(ctx)
	if store == nil {
		t.Fatal("Expected store after process start")
	}

	// Test cleanup registration
	cleanupCalled := false
	store.AddCleanup(func() error {
		cleanupCalled = true
		return nil
	})

	// Release frame context - this triggers cleanup of all Closer values
	ctxapi.ReleaseFrameContext(fc)
	if !cleanupCalled {
		t.Error("Cleanup function was not called")
	}
}

func TestErrorPropagationFromRaiseError(t *testing.T) {
	// Test that errors properly propagate through the scheduler
	// and result in a lua.Error with stack trace information
	script := `
		function trigger_error()
			error("test error from lua")
		end
		trigger_error()
	`

	proc := NewProcess(WithScript(script, "test_error.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	defer proc.Close()

	// Step should return an error
	var output process.StepOutput
	err := proc.Step(nil, &output)

	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	// Verify error contains expected message
	errStr := err.Error()
	if errStr == "" {
		t.Error("Error string is empty")
	}

	// Check if we can extract lua.Error
	we := lua.GetError(err)
	if we == nil {
		t.Logf("Error type: %T", err)
		t.Logf("Error: %v", err)
		t.Fatal("Failed to extract lua.Error from error")
	}

	// Verify Lua stack is captured (may be empty for simple errors)
	if we.LuaStack != nil && len(we.LuaStack.Frames) > 0 {
		hasSource := false
		for _, frame := range we.LuaStack.Frames {
			if frame.Source != "" {
				hasSource = true
				break
			}
		}
		if !hasSource {
			t.Log("No source file info in Lua stack frames (may be expected)")
		}
	}
}

func TestErrorPropagationWithPcall(t *testing.T) {
	// Test that pcall can catch errors
	script := `
		local function will_fail()
			error("inner error")
		end

		local ok, err = pcall(will_fail)

		-- ok should be false since error was raised
		assert(not ok, "expected pcall to return false")

		-- err should contain the error message
		assert(err ~= nil, "expected error to be non-nil")

		-- tostring should work
		local msg = tostring(err)
		assert(#msg > 0, "error message is empty")

		return "success"
	`

	proc := NewProcess(WithScript(script, "test_pcall.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	defer proc.Close()

	// Run to completion
	var output process.StepOutput
	for i := 0; i < 10; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step failed: %v", err)
		}
		if output.Status() == process.StepDone {
			return
		}
	}
	t.Error("Did not complete in expected steps")
}

func TestLuaErrorWithStack(t *testing.T) {
	// Test that a regular Lua error() also produces proper stack trace
	script := `
		function deep()
			error("deep error")
		end

		function middle()
			deep()
		end

		function top()
			middle()
		end

		top()
	`

	proc := NewProcess(WithScript(script, "stack_test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	defer proc.Close()

	var output process.StepOutput
	err := proc.Step(nil, &output)

	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	// Verify error message contains the error text
	errStr := err.Error()
	if !containsString(errStr, "deep error") {
		t.Errorf("Error message doesn't contain 'deep error': %s", errStr)
	}

	// Check that error is wrapped properly
	we := lua.GetError(err)
	if we == nil {
		t.Logf("Error type: %T", err)
		t.Logf("Error: %v", err)
		t.Fatal("Failed to extract lua.Error")
	}

	// Verify the wrapped error contains the original message
	if !containsString(we.Error(), "deep error") {
		t.Errorf("lua.Error doesn't contain 'deep error': %s", we.Error())
	}
}

func containsString(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 &&
		(s == substr || len(s) > len(substr) &&
			(s[:len(substr)] == substr ||
				s[len(s)-len(substr):] == substr ||
				findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestProcessReturnsResult(t *testing.T) {
	// Test that Step() captures the return value in StepOutput.Result
	script := `return {ok = true, value = 42}`

	proc := NewProcess(WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	if output.Status() != process.StepDone {
		t.Fatalf("Expected StepDone, got %v", output.Status())
	}

	// The result should contain the return value
	if output.Result() == nil {
		t.Fatal("Expected output.Result to be non-nil, got nil")
	}
}

func TestProcessReturnsSimpleValue(t *testing.T) {
	// Test returning a simple number
	script := `return 123`

	proc := NewProcess(WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	if output.Status() != process.StepDone {
		t.Fatalf("Expected StepDone, got %v", output.Status())
	}

	if output.Result() == nil {
		t.Fatal("Expected output.Result to be non-nil")
	}
}

func TestProcessReturnsMethodResult(t *testing.T) {
	// Test that method return values are captured
	script := `
		return {
			main = function()
				return {status = "success", code = 200}
			end
		}
	`

	proc := NewProcess(WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "main", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	if output.Status() != process.StepDone {
		t.Fatalf("Expected StepDone, got %v", output.Status())
	}

	if output.Result() == nil {
		t.Fatal("Expected output.Result to be non-nil")
	}
}

func TestProcessReturnsStringError(t *testing.T) {
	// Test that returning (value, "error string") is treated as an error
	script := `
		return {
			main = function()
				return nil, "something went wrong"
			end
		}
	`

	proc := NewProcess(WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "main", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	var output process.StepOutput
	err := proc.Step(nil, &output)
	if output.Status() != process.StepDone {
		t.Fatalf("Expected StepDone, got %v", output.Status())
	}

	if err == nil {
		t.Fatal("Expected error from second return value, got nil")
	}

	if err.Error() != "something went wrong" {
		t.Errorf("Expected error 'something went wrong', got '%s'", err.Error())
	}
}

func TestProcessReturnsLuaError(t *testing.T) {
	// Test that returning (value, errors.new({...})) is treated as an error
	script := `
		return {
			main = function()
				local err = errors.new({
					message = "validation failed",
					kind = errors.INVALID,
					retryable = false
				})
				return nil, err
			end
		}
	`

	// Create factory with errors module
	factory := NewFactory(FactoryConfig{
		Script:        script,
		ScriptName:    "test.lua",
		ModuleBinders: []ModuleBinder{func(l *lua.LState) { lua.OpenErrors(l) }},
	})

	p, err := factory()
	if err != nil {
		t.Fatalf("Factory failed: %v", err)
	}
	proc := p.(*Process)
	defer proc.Close()

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "main", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	var output process.StepOutput
	stepErr := proc.Step(nil, &output)
	if output.Status() != process.StepDone {
		t.Fatalf("Expected StepDone, got %v", output.Status())
	}

	if stepErr == nil {
		t.Fatal("Expected error from second return value, got nil")
	}

	// Check that it's a lua.Error
	luaErr := lua.GetError(stepErr)
	if luaErr == nil {
		t.Fatalf("Expected lua.Error, got %T", stepErr)
	}

	if luaErr.Message != "validation failed" {
		t.Errorf("Expected message 'validation failed', got '%s'", luaErr.Message)
	}

	if luaErr.Kind() != lua.KindInvalid {
		t.Errorf("Expected kind Invalid, got %s", luaErr.Kind())
	}
}

func TestProcessReturnsValueNoError(t *testing.T) {
	// Test that returning (value, nil) is NOT treated as an error
	script := `
		return {
			main = function()
				return {success = true}, nil
			end
		}
	`

	proc := NewProcess(WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "main", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	var output process.StepOutput
	err := proc.Step(nil, &output)
	if output.Status() != process.StepDone {
		t.Fatalf("Expected StepDone, got %v", output.Status())
	}

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if output.Result() == nil {
		t.Fatal("Expected output.Result to be non-nil")
	}
}

func TestProcessReturnsValueWithFalseSecond(t *testing.T) {
	// Test that returning (value, false) is NOT treated as an error
	script := `
		return {
			main = function()
				return 42, false
			end
		}
	`

	proc := NewProcess(WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "main", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	var output process.StepOutput
	err := proc.Step(nil, &output)
	if output.Status() != process.StepDone {
		t.Fatalf("Expected StepDone, got %v", output.Status())
	}

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
}

// Task queue tests (from task_test.go)

func TestTaskPool(t *testing.T) {
	state := lua.NewState()
	defer state.Close()

	fn := state.NewFunction(func(_ *lua.LState) int { return 0 })
	thread, _ := state.NewThread()

	task := NewTask(thread, fn)
	if task == nil {
		t.Fatal("NewTask returned nil")
	}

	if task.Thread() != thread {
		t.Error("Thread() returned wrong value")
	}

	if task.Function() != fn {
		t.Error("Function() returned wrong value")
	}

	if task.State != lua.ResumeYield {
		t.Errorf("State = %v, want ResumeYield", task.State)
	}

	if task.Type() != lua.LTThread {
		t.Errorf("Type() = %v, want LTThread", task.Type())
	}

	str := task.String()
	if str == "" {
		t.Error("String() returned empty")
	}

	task.Close()
}

func TestTaskResumeWith(t *testing.T) {
	state := lua.NewState()
	defer state.Close()

	fn := state.NewFunction(func(_ *lua.LState) int { return 0 })
	thread, _ := state.NewThread()

	task := NewTask(thread, fn)
	defer task.Close()

	task.ResumeWith(lua.LString("a"), lua.LNumber(42))

	if len(task.Resumed) != 2 {
		t.Errorf("Resumed length = %d, want 2", len(task.Resumed))
	}

	if task.Resumed[0] != lua.LString("a") {
		t.Error("Resumed[0] wrong value")
	}

	if task.Resumed[1] != lua.LNumber(42) {
		t.Error("Resumed[1] wrong value")
	}
}

func TestTaskQueueBasic(t *testing.T) {
	q := NewTaskQueue()

	if !q.IsEmpty() {
		t.Error("new queue should be empty")
	}

	if q.Len() != 0 {
		t.Errorf("Len() = %d, want 0", q.Len())
	}

	state := lua.NewState()
	defer state.Close()

	fn := state.NewFunction(func(_ *lua.LState) int { return 0 })

	t1, _ := state.NewThread()
	t2, _ := state.NewThread()
	t3, _ := state.NewThread()
	task1 := NewTask(t1, fn)
	task2 := NewTask(t2, fn)
	task3 := NewTask(t3, fn)

	q.Push(task1)
	q.Push(task2)
	q.Push(task3)

	if q.IsEmpty() {
		t.Error("queue should not be empty after Push")
	}

	if q.Len() != 3 {
		t.Errorf("Len() = %d, want 3", q.Len())
	}

	popped := q.Pop()
	if popped != task1 {
		t.Error("Pop() returned wrong task (FIFO order)")
	}

	if q.Len() != 2 {
		t.Errorf("Len() = %d after Pop, want 2", q.Len())
	}

	popped = q.Pop()
	if popped != task2 {
		t.Error("second Pop() returned wrong task")
	}

	popped = q.Pop()
	if popped != task3 {
		t.Error("third Pop() returned wrong task")
	}

	popped = q.Pop()
	if popped != nil {
		t.Error("Pop() from empty queue should return nil")
	}

	task1.Close()
	task2.Close()
	task3.Close()
}

func TestTaskQueueDrain(t *testing.T) {
	q := NewTaskQueue()

	state := lua.NewState()
	defer state.Close()

	fn := state.NewFunction(func(_ *lua.LState) int { return 0 })

	tasks := make([]*Task, 5)
	for i := range tasks {
		thread, _ := state.NewThread()
		tasks[i] = NewTask(thread, fn)
		q.Push(tasks[i])
	}

	drained := q.Drain()
	if len(drained) != 5 {
		t.Errorf("Drain() returned %d tasks, want 5", len(drained))
	}

	for i, task := range drained {
		if task != tasks[i] {
			t.Errorf("Drain()[%d] wrong task", i)
		}
	}

	if !q.IsEmpty() {
		t.Error("queue should be empty after Drain")
	}

	drained = q.Drain()
	if drained != nil {
		t.Error("Drain() on empty queue should return nil")
	}

	for _, task := range tasks {
		task.Close()
	}
}

func TestTaskQueueGrow(t *testing.T) {
	q := NewTaskQueue()

	state := lua.NewState()
	defer state.Close()

	fn := state.NewFunction(func(_ *lua.LState) int { return 0 })

	tasks := make([]*Task, 20)
	for i := range tasks {
		thread, _ := state.NewThread()
		tasks[i] = NewTask(thread, fn)
		q.Push(tasks[i])
	}

	if q.Len() != 20 {
		t.Errorf("Len() = %d, want 20", q.Len())
	}

	for i := 0; i < 20; i++ {
		popped := q.Pop()
		if popped != tasks[i] {
			t.Errorf("Pop() at %d returned wrong task", i)
		}
	}

	for _, task := range tasks {
		task.Close()
	}
}

func TestTaskQueueSequential(t *testing.T) {
	q := NewTaskQueue()

	state := lua.NewState()
	defer state.Close()

	fn := state.NewFunction(func(_ *lua.LState) int { return 0 })

	pushCount := 100
	for i := 0; i < pushCount; i++ {
		thread, _ := state.NewThread()
		task := NewTask(thread, fn)
		q.Push(task)
	}

	if q.Len() != pushCount {
		t.Errorf("Len() = %d, want %d", q.Len(), pushCount)
	}

	popped := 0
	for i := 0; i < pushCount; i++ {
		if task := q.Pop(); task != nil {
			task.Close()
			popped++
		}
	}

	if popped != pushCount {
		t.Errorf("popped = %d, want %d", popped, pushCount)
	}

	if !q.IsEmpty() {
		t.Error("queue should be empty after all pops")
	}
}

func TestTaskQueueWrapAround(t *testing.T) {
	q := NewTaskQueue()

	state := lua.NewState()
	defer state.Close()

	fn := state.NewFunction(func(_ *lua.LState) int { return 0 })

	for i := 0; i < 5; i++ {
		thread, _ := state.NewThread()
		task := NewTask(thread, fn)
		q.Push(task)
		q.Pop().Close()
	}

	tasks := make([]*Task, 6)
	for i := range tasks {
		thread, _ := state.NewThread()
		tasks[i] = NewTask(thread, fn)
		q.Push(tasks[i])
	}

	for i := 0; i < 6; i++ {
		popped := q.Pop()
		if popped != tasks[i] {
			t.Errorf("Pop() at %d returned wrong task after wrap-around", i)
		}
		popped.Close()
	}
}

// Pool tests (from pool_test.go)

type poolTestDispatcher struct {
	handlers map[dispatcher.CommandID]dispatcher.Handler
	clock    *clock.Dispatcher
}

func newPoolTestDispatcher() *poolTestDispatcher {
	d := &poolTestDispatcher{handlers: make(map[dispatcher.CommandID]dispatcher.Handler)}
	d.clock = clock.NewDispatcher()
	d.clock.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		d.handlers[id] = h
	})
	return d
}

func (d *poolTestDispatcher) Dispatch(cmd dispatcher.Command) dispatcher.Handler {
	return d.handlers[cmd.CmdID()]
}

func (d *poolTestDispatcher) Stop() {
	if d.clock != nil {
		_ = d.clock.Stop(context.Background())
	}
}

func newLuaFactory(script string) process.FactoryFunc {
	return func() (process.Process, error) {
		proto, err := lua.CompileString(script, "test.lua")
		if err != nil {
			return nil, err
		}

		proc := NewProcess(
			WithProto(proto),
		)

		return proc, nil
	}
}

func TestPoolBasicCall(t *testing.T) {
	factory := newLuaFactory(`return 1 + 2`)
	disp := newPoolTestDispatcher()
	defer disp.Stop()

	ps, err := funcpool.NewStatic(factory, disp, funcpool.Config{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}

	ps.Start()
	defer ps.Stop()

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	result, err := ps.Call(ctx, "", nil)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	if result == nil {
		t.Fatal("nil result")
	}
	t.Log("Basic pool call passed")
}

func TestPoolStateReuse(t *testing.T) {
	factory := newLuaFactory(`
		counter = (counter or 0) + 1
		return counter
	`)
	disp := newPoolTestDispatcher()
	defer disp.Stop()

	ps, err := funcpool.NewStatic(factory, disp, funcpool.Config{Workers: 1})
	if err != nil {
		t.Fatal(err)
	}

	ps.Start()
	defer ps.Stop()

	for i := 1; i <= 3; i++ {
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		result, err := ps.Call(ctx, "", nil)
		if err != nil {
			t.Fatalf("Call %d failed: %v", i, err)
		}
		if result.Error != nil {
			t.Fatalf("Call %d result error: %v", i, result.Error)
		}
		t.Logf("Call %d result: %v", i, result.Value)
	}
}

func Benchmark8x8NoYield(b *testing.B) {
	factory := newLuaFactory(`return 1 + 2`)
	disp := newPoolTestDispatcher()
	defer disp.Stop()

	ps, err := funcpool.NewStatic(factory, disp, funcpool.Config{Workers: 8, QueueSize: 256})
	if err != nil {
		b.Fatal(err)
	}

	ps.Start()
	defer ps.Stop()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ctx, fc := ctxapi.AcquireFrameContext(context.Background())
			_, _ = ps.Call(ctx, "", nil)
			ctxapi.ReleaseFrameContext(fc)
		}
	})
}

func BenchmarkSingleWorker(b *testing.B) {
	factory := newLuaFactory(`return 1`)
	disp := newPoolTestDispatcher()
	defer disp.Stop()

	ps, err := funcpool.NewStatic(factory, disp, funcpool.Config{
		Workers:   1,
		QueueSize: 16,
	})
	if err != nil {
		b.Fatal(err)
	}

	ps.Start()
	defer ps.Stop()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx, fc := ctxapi.AcquireFrameContext(context.Background())
		_, _ = ps.Call(ctx, "", nil)
		ctxapi.ReleaseFrameContext(fc)
	}
}

func BenchmarkWorkerScalingLua(b *testing.B) {
	for _, workers := range []int{1, 2, 4, 8, 16} {
		b.Run(fmt.Sprintf("W%d", workers), func(b *testing.B) {
			factory := newLuaFactory(`return 1`)
			disp := newPoolTestDispatcher()
			defer disp.Stop()

			ps, err := funcpool.NewStatic(factory, disp, funcpool.Config{
				Workers:   workers,
				QueueSize: 16,
			})
			if err != nil {
				b.Fatal(err)
			}

			ps.Start()
			defer ps.Stop()

			b.ResetTimer()
			b.ReportAllocs()

			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					ctx, fc := ctxapi.AcquireFrameContext(context.Background())
					_, _ = ps.Call(ctx, "", nil)
					ctxapi.ReleaseFrameContext(fc)
				}
			})
		})
	}
}
