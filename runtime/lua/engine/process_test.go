package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/system/clock"
	"github.com/wippyai/runtime/system/scheduler/pool/static"
)

// testYieldCmdID is a test command ID for simulating external yields
const testYieldCmdID dispatcher.CommandID = 999

// testYieldCmd is the dispatcher command for test yield
type testYieldCmd struct {
	Duration time.Duration
}

func (testYieldCmd) CmdID() dispatcher.CommandID { return testYieldCmdID }

// testYield simulates an external yield like time.sleep or SQL query
// Implements HandledYield to test the yield result handling path
type testYield struct {
	duration time.Duration
	released bool
}

func (y *testYield) Release()                      { y.released = true }
func (y *testYield) String() string                { return "<test_yield>" }
func (y *testYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *testYield) CmdID() dispatcher.CommandID   { return testYieldCmdID }
func (y *testYield) ToCommand() dispatcher.Command { return testYieldCmd{Duration: y.duration} }

// HandleResult implements luaapi.HandledYield
func (y *testYield) HandleResult(_ *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}
	if data == nil {
		return []lua.LValue{lua.LTrue}
	}
	// Pass through lua values
	if vals, ok := data.([]lua.LValue); ok {
		return vals
	}
	return []lua.LValue{lua.LString(fmt.Sprintf("%v", data))}
}

// luaTestYield is a Lua function that yields testYield
func luaTestYield(l *lua.LState) int {
	ms := l.CheckNumber(1)
	duration := time.Duration(ms) * time.Millisecond
	yield := &testYield{duration: duration}
	l.Push(yield)
	return -1
}

// bindTestYield binds test_yield function to Lua state
func bindTestYield(l *lua.LState) error {
	l.SetGlobal("test_yield", l.NewFunction(luaTestYield))
	return nil
}

// testPID creates a test PID for unit tests
func testPID() pid.PID {
	return pid.PID{Host: "test", UniqID: "source"}
}

func TestProcessBasicExecution(t *testing.T) {
	script := `
		local result = 1 + 2
		return result
	`

	proc := mustNewProcess(t, WithScript(script, "test.lua"))

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

	proc := mustNewProcess(t, WithScript(script, "test.lua"))

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
	ctx, fc := ctxapi.OpenFrameContext(context.Background())

	// No store yet
	store := resource.GetStore(ctx)
	if store != nil {
		t.Error("Expected nil store before process start")
	}

	script := `return 1`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

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

	proc := mustNewProcess(t, WithScript(script, "test_error.lua"))

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

	// Step() crosses runtime boundary and should return apierror.Error.
	var apiErr apierror.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("Expected apierror.Error, got %T", err)
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

	proc := mustNewProcess(t, WithScript(script, "test_pcall.lua"))

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

	proc := mustNewProcess(t, WithScript(script, "stack_test.lua"))

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

	// Step() crosses runtime boundary and should return apierror.Error.
	var apiErr apierror.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("Expected apierror.Error, got %T", err)
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

	proc := mustNewProcess(t, WithScript(script, "test.lua"))

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

	proc := mustNewProcess(t, WithScript(script, "test.lua"))

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

	proc := mustNewProcess(t, WithScript(script, "test.lua"))

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

	proc := mustNewProcess(t, WithScript(script, "test.lua"))

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
		ModuleBinders: []ModuleBinder{wrapBinder(func(l *lua.LState) { lua.OpenErrors(l) })},
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

	// Step() crosses runtime boundary and should return apierror.Error.
	var apiErr apierror.Error
	if !errors.As(stepErr, &apiErr) {
		t.Fatalf("Expected apierror.Error, got %T", stepErr)
	}

	if apiErr.Error() != "validation failed" {
		t.Errorf("Expected message 'validation failed', got '%s'", apiErr.Error())
	}

	if apiErr.Kind() != apierror.Invalid {
		t.Errorf("Expected kind Invalid, got %s", apiErr.Kind())
	}

	if apiErr.Retryable() != apierror.False {
		t.Errorf("Expected retryable False, got %s", apiErr.Retryable())
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

	proc := mustNewProcess(t, WithScript(script, "test.lua"))

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

	proc := mustNewProcess(t, WithScript(script, "test.lua"))

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
	// Add handler for test yields (mock time.sleep)
	d.handlers[testYieldCmdID] = dispatcher.HandlerFunc(func(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
		c := cmd.(testYieldCmd)
		// Use short delay or immediate completion for testing
		if c.Duration > 0 {
			time.AfterFunc(time.Millisecond, func() {
				receiver.CompleteYield(tag, nil, nil)
			})
		} else {
			receiver.CompleteYield(tag, nil, nil)
		}
		return nil
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

		proc, err := NewProcess(WithProto(proto))
		if err != nil {
			return nil, err
		}

		return proc, nil
	}
}

func TestPoolBasicCall(t *testing.T) {
	factory := newLuaFactory(`return 1 + 2`)
	disp := newPoolTestDispatcher()
	defer disp.Stop()

	ps, err := static.New(factory, disp, static.Config{Workers: 2})
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

	ps, err := static.New(factory, disp, static.Config{Workers: 1})
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

// bindMockTimeModule binds a mock time module with test_yield for sleep
func bindMockTimeModule(l *lua.LState) error {
	timeMod := l.NewTable()
	timeMod.RawSetString("MILLISECOND", lua.LNumber(1000000)) // nanoseconds
	timeMod.RawSetString("sleep", l.NewFunction(luaTestYield))
	l.SetGlobal("time", timeMod)
	return nil
}

// newLuaFactoryWithChannelsAndTime creates a factory that includes channel and mock time modules
func newLuaFactoryWithChannelsAndTime(script string) process.FactoryFunc {
	return func() (process.Process, error) {
		proto, err := lua.CompileString(script, "test.lua")
		if err != nil {
			return nil, err
		}

		proc, err := NewProcess(
			WithProto(proto),
			WithModuleBinder(wrapBinder(func(l *lua.LState) { LoadModuleDef(l, ChannelModule) })),
			WithModuleBinder(bindMockTimeModule),
		)
		if err != nil {
			return nil, err
		}

		return proc, nil
	}
}

// TestPoolDistributedWorkWithSleep tests the distributed_work.lua pattern
// with actual scheduler/pool integration including time.sleep yields.
func TestPoolDistributedWorkWithSleep(t *testing.T) {
	script := `
		local time = require("time")

		local work_queue = channel.new(10)
		local results = channel.new(10)
		local worker_count = 3
		local job_count = 6

		-- Spawn workers that simulate processing time
		for w = 1, worker_count do
			coroutine.spawn(function()
				while true do
					local job, ok = work_queue:receive()
					if not ok then break end
					time.sleep(10 * time.MILLISECOND)
					results:send({worker = w, job = job, result = job * 2})
				end
			end)
		end

		-- Producer sends jobs
		for i = 1, job_count do
			work_queue:send(i)
		end
		work_queue:close()

		-- Collect results
		local total = 0
		for i = 1, job_count do
			local r = results:receive()
			total = total + r.result
		end

		return total
	`

	factory := newLuaFactoryWithChannelsAndTime(script)
	disp := newPoolTestDispatcher()
	defer disp.Stop()

	ps, err := static.New(factory, disp, static.Config{Workers: 1})
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
	if result.Error != nil {
		t.Fatalf("Process error: %v", result.Error)
	}

	t.Logf("Result: %v", result.Value)
}

func Benchmark8x8NoYield(b *testing.B) {
	factory := newLuaFactory(`return 1 + 2`)
	disp := newPoolTestDispatcher()
	defer disp.Stop()

	ps, err := static.New(factory, disp, static.Config{Workers: 8, QueueSize: 256})
	if err != nil {
		b.Fatal(err)
	}

	ps.Start()
	defer ps.Stop()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ctx, fc := ctxapi.OpenFrameContext(context.Background())
			_, _ = ps.Call(ctx, "", nil)
			ctxapi.ReleaseFrameContext(fc)
		}
	})
}

func BenchmarkSingleWorker(b *testing.B) {
	factory := newLuaFactory(`return 1`)
	disp := newPoolTestDispatcher()
	defer disp.Stop()

	ps, err := static.New(factory, disp, static.Config{
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
		ctx, fc := ctxapi.OpenFrameContext(context.Background())
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

			ps, err := static.New(factory, disp, static.Config{
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
					ctx, fc := ctxapi.OpenFrameContext(context.Background())
					_, _ = ps.Call(ctx, "", nil)
					ctxapi.ReleaseFrameContext(fc)
				}
			})
		})
	}
}

// TestProcessExternalYieldWithChannelSelect tests that channel select works correctly
// after an external yield completes. This is a unit test that calls Process.Step() directly.
// Pattern: main sends work to worker, waits for result, signals stop, waits for worker exit.
func TestProcessExternalYieldWithChannelSelect(t *testing.T) {
	script := `
		local ops_channel = channel.new(256)
		local stop_signal = channel.new(0)
		local bus_done = channel.new(0)
		local result_channel = channel.new(1)

		coroutine.spawn(function()
			while true do
				local result = channel.select({
					stop_signal:case_receive(),
					ops_channel:case_receive()
				})

				if result.channel == stop_signal then
					bus_done:send(true)
					return
				end

				if result.channel == ops_channel then
					result_channel:send({success = true, data = result.value})
				end
			end
		end)

		-- External yield (simulates time.sleep or SQL query)
		test_yield(10)

		-- Send work to worker
		ops_channel:send({type = "test_op"})

		-- Wait for result
		local res = result_channel:receive()
		if not res.success then
			return "failed"
		end

		-- Signal worker to stop
		stop_signal:send(true)

		-- Wait for worker to confirm exit
		bus_done:receive()

		return "success"
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(wrapBinder(func(l *lua.LState) { LoadModuleDef(l, ChannelModule) })),
		WithModuleBinder(bindTestYield),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput

	// Step 1: should yield test_yield (external yield)
	if err := proc.Step(nil, &output); err != nil {
		t.Fatalf("Step 1 failed: %v", err)
	}

	if output.Count() != 1 {
		t.Fatalf("Step 1: expected 1 yield, got %d", output.Count())
	}

	yields := output.Yields()
	if yields[0].Cmd.CmdID() != testYieldCmdID {
		t.Fatalf("Step 1: expected test_yield (cmd %d), got %d", testYieldCmdID, yields[0].Cmd.CmdID())
	}
	tag := yields[0].Tag

	// Step 2: complete the external yield, process should continue and complete
	events := []process.Event{
		{Type: process.EventYieldComplete, Tag: tag, Data: nil, Error: nil},
	}
	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatalf("Step 2 failed: %v", err)
	}

	if output.Status() != process.StepDone {
		t.Fatalf("Expected StepDone, got status %d (yields=%d)", output.Status(), output.Count())
	}
}

// TestProcessExternalYieldBasic tests simple external yield and completion
func TestProcessExternalYieldBasic(t *testing.T) {
	script := `
		test_yield(10)
		return "done"
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(bindTestYield),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput

	if err := proc.Step(nil, &output); err != nil {
		t.Fatalf("Step 1 failed: %v", err)
	}

	if output.Count() != 1 {
		t.Fatalf("expected 1 yield, got %d", output.Count())
	}

	if output.Status() != process.StepYield {
		t.Fatalf("expected StepYield, got %d", output.Status())
	}

	tag := output.Yields()[0].Tag
	events := []process.Event{
		{Type: process.EventYieldComplete, Tag: tag},
	}
	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatalf("Step 2 failed: %v", err)
	}

	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}
}

// TestProcessMultipleExternalYields tests multiple sequential external yields
func TestProcessMultipleExternalYields(t *testing.T) {
	script := `
		test_yield(10)
		test_yield(20)
		test_yield(30)
		return "done"
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(bindTestYield),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	var events []process.Event

	for i := 1; i <= 3; i++ {
		output.Reset()
		if err := proc.Step(events, &output); err != nil {
			t.Fatalf("Step %d failed: %v", i, err)
		}

		if output.Count() != 1 {
			t.Fatalf("step %d: expected 1 yield, got %d", i, output.Count())
		}

		tag := output.Yields()[0].Tag
		events = []process.Event{
			{Type: process.EventYieldComplete, Tag: tag},
		}
	}

	// Final step with last completion event
	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatalf("Final step failed: %v", err)
	}

	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}
}

// TestProcessYieldWithError tests external yield completing with error
func TestProcessYieldWithError(t *testing.T) {
	script := `
		local ok, err = pcall(function()
			test_yield(10)
		end)
		if err then
			return "error: " .. tostring(err)
		end
		return "success"
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(bindTestYield),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatalf("Step 1 failed: %v", err)
	}

	tag := output.Yields()[0].Tag
	events := []process.Event{
		{Type: process.EventYieldComplete, Tag: tag, Error: fmt.Errorf("simulated error")},
	}
	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatalf("Step 2 failed: %v", err)
	}

	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}
}

// TestProcessYieldInCoroutine tests external yield inside spawned coroutine
func TestProcessYieldInCoroutine(t *testing.T) {
	script := `
		local result = nil
		coroutine.spawn(function()
			test_yield(10)
			result = "from_coroutine"
		end)

		-- yield to let coroutine start
		coroutine.yield()

		return result or "pending"
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(bindTestYield),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput

	// First step - coroutine yields externally
	if err := proc.Step(nil, &output); err != nil {
		t.Fatalf("Step 1 failed: %v", err)
	}

	if output.Count() != 1 {
		t.Fatalf("expected 1 yield, got %d", output.Count())
	}

	tag := output.Yields()[0].Tag
	events := []process.Event{
		{Type: process.EventYieldComplete, Tag: tag},
	}
	output.Reset()

	// Complete the yield - should finish
	if err := proc.Step(events, &output); err != nil {
		t.Fatalf("Step 2 failed: %v", err)
	}

	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}
}

// TestProcessConcurrentYieldsFromCoroutines tests multiple coroutines yielding externally
func TestProcessConcurrentYieldsFromCoroutines(t *testing.T) {
	script := `
		local results = {}

		coroutine.spawn(function()
			test_yield(10)
			results[1] = "a"
		end)

		coroutine.spawn(function()
			test_yield(20)
			results[2] = "b"
		end)

		-- main also yields
		test_yield(30)
		results[3] = "c"

		return #results
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(bindTestYield),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	maxSteps := 20
	steps := 0

	for steps < maxSteps {
		steps++
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step %d failed: %v", steps, err)
		}

		if output.Status() == process.StepDone {
			break
		}

		// Complete all pending yields
		if output.Count() > 0 {
			var events []process.Event
			for _, y := range output.Yields() {
				events = append(events, process.Event{
					Type: process.EventYieldComplete,
					Tag:  y.Tag,
				})
			}
			output.Reset()
			if err := proc.Step(events, &output); err != nil {
				t.Fatalf("Step complete failed: %v", err)
			}
			if output.Status() == process.StepDone {
				break
			}
		}
	}

	if steps >= maxSteps {
		t.Fatal("did not complete in expected steps")
	}
}

// TestProcessYieldTagCorrelation tests that yield tags correctly correlate responses
func TestProcessYieldTagCorrelation(t *testing.T) {
	script := `
		local a, b = nil, nil

		coroutine.spawn(function()
			test_yield(1)
			a = "first"
		end)

		coroutine.spawn(function()
			test_yield(2)
			b = "second"
		end)

		-- wait for both
		while a == nil or b == nil do
			coroutine.yield()
		end

		return a .. "_" .. b
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(bindTestYield),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatalf("Step 1 failed: %v", err)
	}

	// Should have 2 yields from 2 coroutines
	if output.Count() != 2 {
		t.Fatalf("expected 2 yields, got %d", output.Count())
	}

	// Complete in reverse order to test tag correlation
	yields := output.Yields()
	events := []process.Event{
		{Type: process.EventYieldComplete, Tag: yields[1].Tag},
		{Type: process.EventYieldComplete, Tag: yields[0].Tag},
	}

	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatalf("Step 2 failed: %v", err)
	}

	// May need more steps to complete
	for i := 0; i < 10 && output.Status() != process.StepDone; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step %d failed: %v", i+3, err)
		}
	}

	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}
}

// TestProcessStepStatusTransitions tests all step status transitions
func TestProcessStepStatusTransitions(t *testing.T) {
	tests := []struct {
		name           string
		script         string
		expectedStatus process.StepStatus
	}{
		{
			name:           "immediate_done",
			script:         `return 1`,
			expectedStatus: process.StepDone,
		},
		{
			name:           "yield_wait",
			script:         `test_yield(10); return 1`,
			expectedStatus: process.StepYield,
		},
		{
			name:           "continue_with_coroutines",
			script:         `coroutine.spawn(function() end); coroutine.yield(); return 1`,
			expectedStatus: process.StepDone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proc := mustNewProcess(t,
				WithScript(tt.script, "test.lua"),
				WithModuleBinder(bindTestYield),
			)

			ctx, _ := ctxapi.OpenFrameContext(context.Background())
			if err := proc.Init(ctx, "", nil); err != nil {
				t.Fatal(err)
			}
			defer proc.Close()

			var output process.StepOutput
			for i := 0; i < 10; i++ {
				output.Reset()
				if err := proc.Step(nil, &output); err != nil {
					t.Fatalf("Step failed: %v", err)
				}

				if output.Status() == tt.expectedStatus {
					return
				}

				// If waiting for yields, complete them
				if output.Count() > 0 {
					var events []process.Event
					for _, y := range output.Yields() {
						events = append(events, process.Event{
							Type: process.EventYieldComplete,
							Tag:  y.Tag,
						})
					}
					output.Reset()
					_ = proc.Step(events, &output)
				}

				if output.Status() == process.StepDone {
					break
				}
			}

			if output.Status() != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, output.Status())
			}
		})
	}
}

// TestProcessPendingYieldsCleanup tests that pendingYields are properly cleaned up
func TestProcessPendingYieldsCleanup(t *testing.T) {
	script := `
		test_yield(10)
		test_yield(20)
		return "done"
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(bindTestYield),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput

	// First yield
	if err := proc.Step(nil, &output); err != nil {
		t.Fatal(err)
	}
	if len(proc.pendingYields) != 1 {
		t.Fatalf("expected 1 pending yield, got %d", len(proc.pendingYields))
	}

	tag1 := output.Yields()[0].Tag
	events := []process.Event{{Type: process.EventYieldComplete, Tag: tag1}}
	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatal(err)
	}

	// Second yield - step continues to next yield point
	if output.Count() != 1 {
		t.Fatalf("expected 1 yield, got %d", output.Count())
	}
	// pendingYields should contain the new yield (old one was cleaned up internally)
	if len(proc.pendingYields) != 1 {
		t.Fatalf("expected 1 pending yield, got %d", len(proc.pendingYields))
	}

	tag2 := output.Yields()[0].Tag
	events = []process.Event{{Type: process.EventYieldComplete, Tag: tag2}}
	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatal(err)
	}

	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}
	if len(proc.pendingYields) != 0 {
		t.Fatalf("expected 0 pending yields at end, got %d", len(proc.pendingYields))
	}
}

// TestProcessYieldWithData tests external yield completing with data
func TestProcessYieldWithData(t *testing.T) {
	script := `
		local result = test_yield(10)
		return result or "no_data"
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(bindTestYield),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatal(err)
	}

	tag := output.Yields()[0].Tag
	events := []process.Event{
		{Type: process.EventYieldComplete, Tag: tag, Data: "test_data"},
	}
	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatal(err)
	}

	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}
}

// TestProcessInitNoScript tests Init with no script or proto
func TestProcessInitNoScript(t *testing.T) {
	proc := mustNewProcess(t)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	err := proc.Init(ctx, "", nil)

	if err == nil {
		t.Fatal("expected error for no script")
	}
}

// TestProcessInitInvalidMethod tests Init with non-existent method
func TestProcessInitInvalidMethod(t *testing.T) {
	script := `return {}`

	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	err := proc.Init(ctx, "nonexistent", nil)

	if err == nil {
		t.Fatal("expected error for missing method")
	}
	proc.Close()
}

// TestProcessInitSyntaxError tests Init with malformed Lua
func TestProcessInitSyntaxError(t *testing.T) {
	script := `this is not valid lua {{{{`

	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	err := proc.Init(ctx, "", nil)

	if err == nil {
		t.Fatal("expected error for syntax error")
	}
}

// TestProcessInitWithInputPayloads tests Init with input arguments
func TestProcessInitWithInputPayloads(t *testing.T) {
	script := `
		return {
			main = function(a, b)
				return a + b
			end
		}
	`

	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	p1 := &testPayload{val: lua.LNumber(10)}
	p2 := &testPayload{val: lua.LNumber(20)}

	if err := proc.Init(ctx, "main", payload.Payloads{p1, p2}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}
}

// testPayload implements payload.Payload interface for testing
type testPayload struct {
	val lua.LValue
}

func (p *testPayload) Format() payload.Format { return payload.Lua }
func (p *testPayload) Data() any              { return p.val }

// TestProcessStepWithEmptyEvents tests Step with empty events array
func TestProcessStepWithEmptyEvents(t *testing.T) {
	script := `
		test_yield(10)
		return "done"
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(bindTestYield),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatal(err)
	}

	// Now step with empty events while yield is pending
	output.Reset()
	if err := proc.Step([]process.Event{}, &output); err != nil {
		t.Fatal(err)
	}

	// Should still be waiting
	if output.Count() != 0 && len(proc.pendingYields) != 1 {
		t.Fatalf("expected pending yield to remain")
	}
}

// TestProcessStepEventWithInvalidTag tests yield complete with wrong tag
func TestProcessStepEventWithInvalidTag(t *testing.T) {
	script := `
		test_yield(10)
		return "done"
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(bindTestYield),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatal(err)
	}

	// Send completion with wrong tag
	events := []process.Event{
		{Type: process.EventYieldComplete, Tag: 99999},
	}
	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatal(err)
	}

	// Original yield should still be pending
	if len(proc.pendingYields) != 1 {
		t.Fatalf("expected 1 pending yield, got %d", len(proc.pendingYields))
	}
}

// TestProcessStepEventWithZeroTag tests yield complete with tag 0
func TestProcessStepEventWithZeroTag(t *testing.T) {
	script := `
		test_yield(10)
		return "done"
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(bindTestYield),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatal(err)
	}

	events := []process.Event{
		{Type: process.EventYieldComplete, Tag: 0},
	}
	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatal(err)
	}

	if len(proc.pendingYields) != 1 {
		t.Fatalf("expected 1 pending yield, got %d", len(proc.pendingYields))
	}
}

// TestProcessChannelWithoutChannelModule tests process without channel operations
func TestProcessChannelWithoutChannelModule(t *testing.T) {
	script := `
		local x = 1 + 2
		return x
	`

	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatal(err)
	}

	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}
}

// TestProcessDeadlockDetection tests deadlock when all coroutines blocked
// Note: main returning kills all threads (like Go), so we test deadlock via main blocking
func TestProcessDeadlockDetection(t *testing.T) {
	script := `
		local ch = channel.new(0)

		coroutine.spawn(function()
			ch:receive()
		end)

		-- main blocks too, creating true deadlock
		ch:receive()
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(wrapBinder(func(l *lua.LState) { LoadModuleDef(l, ChannelModule) })),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	for i := 0; i < 20; i++ {
		output.Reset()
		err := proc.Step(nil, &output)
		if err != nil {
			// Deadlock error expected
			return
		}
		if output.Status() == process.StepDone {
			t.Fatal("should not complete - deadlock expected")
		}
		if output.Status() == process.StepIdle {
			// Idle with channels means blocked - this is the deadlock state
			return
		}
	}
}

// TestProcessChannelClosedReceive tests receiving from closed channel
func TestProcessChannelClosedReceive(t *testing.T) {
	script := `
		local ch = channel.new(0)
		ch:close()

		local val, ok = ch:receive()
		return ok
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(wrapBinder(func(l *lua.LState) { LoadModuleDef(l, ChannelModule) })),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

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
	t.Fatal("did not complete")
}

// TestProcessChannelBufferedFull tests sending to full buffered channel
func TestProcessChannelBufferedFull(t *testing.T) {
	script := `
		local ch = channel.new(1)
		ch:send("first")

		local sent = false
		coroutine.spawn(function()
			ch:send("second")
			sent = true
		end)

		coroutine.yield()
		local val = ch:receive()
		coroutine.yield()

		return sent
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(wrapBinder(func(l *lua.LState) { LoadModuleDef(l, ChannelModule) })),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	for i := 0; i < 20; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step failed: %v", err)
		}
		if output.Status() == process.StepDone {
			return
		}
	}
	t.Fatal("did not complete")
}

// TestProcessSelectWithDefault tests select with default case
func TestProcessSelectWithDefault(t *testing.T) {
	script := `
		local ch = channel.new(0)

		local result = channel.select({
			ch:case_receive(),
			default = true
		})

		return result.default
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(wrapBinder(func(l *lua.LState) { LoadModuleDef(l, ChannelModule) })),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

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
	t.Fatal("did not complete")
}

// TestProcessSelectMultipleChannels tests select with multiple ready channels
func TestProcessSelectMultipleChannels(t *testing.T) {
	script := `
		local ch1 = channel.new(1)
		local ch2 = channel.new(1)

		ch1:send("from_ch1")
		ch2:send("from_ch2")

		local result = channel.select({
			ch1:case_receive(),
			ch2:case_receive()
		})

		return result.ok
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(wrapBinder(func(l *lua.LState) { LoadModuleDef(l, ChannelModule) })),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

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
	t.Fatal("did not complete")
}

// TestProcessCoroutineError tests error in spawned coroutine
func TestProcessCoroutineError(t *testing.T) {
	script := `
		local err_val = nil

		coroutine.spawn(function()
			error("coroutine error")
		end)

		coroutine.yield()
		return "main completed"
	`

	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	var lastErr error
	for i := 0; i < 10; i++ {
		output.Reset()
		lastErr = proc.Step(nil, &output)
		if lastErr != nil {
			// Error from coroutine expected
			return
		}
		if output.Status() == process.StepDone {
			return
		}
	}
}

// TestProcessNestedCoroutines tests coroutines spawning coroutines
func TestProcessNestedCoroutines(t *testing.T) {
	script := `
		local result = 0

		coroutine.spawn(function()
			coroutine.spawn(function()
				coroutine.spawn(function()
					result = result + 1
				end)
				result = result + 10
			end)
			result = result + 100
		end)

		for i = 1, 10 do
			coroutine.yield()
		end

		return result
	`

	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	for i := 0; i < 20; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step failed: %v", err)
		}
		if output.Status() == process.StepDone {
			return
		}
	}
	t.Fatal("did not complete")
}

// TestProcessYieldResumeOrder tests that yields resume in correct order
func TestProcessYieldResumeOrder(t *testing.T) {
	script := `
		local order = {}

		coroutine.spawn(function()
			test_yield(1)
			table.insert(order, "a")
		end)

		coroutine.spawn(function()
			test_yield(2)
			table.insert(order, "b")
		end)

		coroutine.spawn(function()
			test_yield(3)
			table.insert(order, "c")
		end)

		while #order < 3 do
			coroutine.yield()
		end

		return table.concat(order, ",")
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(bindTestYield),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	maxSteps := 30

	for step := 0; step < maxSteps; step++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step failed: %v", err)
		}

		if output.Status() == process.StepDone {
			return
		}

		// Complete any pending yields
		if output.Count() > 0 {
			var events []process.Event
			for _, y := range output.Yields() {
				events = append(events, process.Event{
					Type: process.EventYieldComplete,
					Tag:  y.Tag,
				})
			}
			output.Reset()
			if err := proc.Step(events, &output); err != nil {
				t.Fatalf("Step with events failed: %v", err)
			}
			if output.Status() == process.StepDone {
				return
			}
		}
	}
	t.Fatal("did not complete")
}

// TestProcessMixedYieldsAndChannels tests external yields mixed with channel ops
func TestProcessMixedYieldsAndChannels(t *testing.T) {
	script := `
		local ch = channel.new(1)
		local result = nil

		coroutine.spawn(function()
			test_yield(10)
			ch:send("from_yield")
		end)

		coroutine.spawn(function()
			result = ch:receive()
		end)

		while result == nil do
			coroutine.yield()
		end

		return result
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(wrapBinder(func(l *lua.LState) { LoadModuleDef(l, ChannelModule) })),
		WithModuleBinder(bindTestYield),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	maxSteps := 30

	for step := 0; step < maxSteps; step++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step failed: %v", err)
		}

		if output.Status() == process.StepDone {
			return
		}

		if output.Count() > 0 {
			var events []process.Event
			for _, y := range output.Yields() {
				events = append(events, process.Event{
					Type: process.EventYieldComplete,
					Tag:  y.Tag,
				})
			}
			output.Reset()
			if err := proc.Step(events, &output); err != nil {
				t.Fatalf("Step with events failed: %v", err)
			}
			if output.Status() == process.StepDone {
				return
			}
		}
	}
	t.Fatal("did not complete")
}

// TestProcessTaskQueueDrainOrder tests FIFO order of task queue
func TestProcessTaskQueueDrainOrder(t *testing.T) {
	script := `
		local order = {}

		coroutine.spawn(function()
			table.insert(order, 1)
		end)

		coroutine.spawn(function()
			table.insert(order, 2)
		end)

		coroutine.spawn(function()
			table.insert(order, 3)
		end)

		coroutine.yield()
		coroutine.yield()
		coroutine.yield()

		return order[1] == 1 and order[2] == 2 and order[3] == 3
	`

	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	for i := 0; i < 20; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step failed: %v", err)
		}
		if output.Status() == process.StepDone {
			return
		}
	}
	t.Fatal("did not complete")
}

// TestProcessGetTask tests GetTask method
func TestProcessGetTask(t *testing.T) {
	script := `
		coroutine.spawn(function()
			coroutine.yield()
		end)
		return 1
	`

	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// Before step, main task exists
	if proc.mainTask == nil {
		t.Fatal("expected mainTask to exist")
	}

	thread := proc.mainTask.Thread()
	task, err := proc.GetTask(thread)
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}
	if task != proc.mainTask {
		t.Fatal("GetTask should return mainTask for main thread")
	}

	// Non-existent thread
	fakeThread, _ := proc.state.NewThread()
	_, err = proc.GetTask(fakeThread)
	if err == nil {
		t.Fatal("GetTask should return error for unknown thread")
	}
}

// TestProcessMultipleReturnValues tests handling of multiple return values
func TestProcessMultipleReturnValues(t *testing.T) {
	script := `
		return 1, 2, 3
	`

	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatal(err)
	}

	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}

	if output.Result() == nil {
		t.Fatal("expected result")
	}
}

// TestProcessReturnNil tests explicit nil return
func TestProcessReturnNil(t *testing.T) {
	script := `return nil`

	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatal(err)
	}

	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}
}

// TestProcessNoReturn tests script with no return statement
func TestProcessNoReturn(t *testing.T) {
	script := `local x = 1`

	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatal(err)
	}

	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}
}

// TestProcessSubscribe tests Subscribe method
func TestProcessSubscribe(t *testing.T) {
	script := `return 1`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// Subscribe to a topic
	ch, err := proc.Subscribe("test-topic", 10)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}

	// Subscribe again to same topic returns same channel
	ch2, err := proc.Subscribe("test-topic", 10)
	if err != nil {
		t.Fatalf("second Subscribe failed: %v", err)
	}
	if ch2 != ch {
		t.Fatal("expected same channel for same topic")
	}

	// HasSubscriptions should return true
	if !proc.HasSubscriptions() {
		t.Fatal("expected HasSubscriptions to return true")
	}
}

// TestProcessSubscribeWithoutInit tests Subscribe before Init
func TestProcessSubscribeWithoutInit(t *testing.T) {
	script := `return 1`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	// Subscribe without Init should fail
	ch, err := proc.Subscribe("test-topic", 10)
	if err == nil {
		t.Fatal("expected error when subscribing before Init")
	}
	if ch != nil {
		t.Fatal("expected nil channel")
	}
}

// TestProcessSubscribeExisting tests SubscribeExisting method
func TestProcessSubscribeExisting(t *testing.T) {
	script := `return 1`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// Create external channel
	externalCh := NewChannel(5)

	// Subscribe with existing channel
	err := proc.SubscribeExisting("external-topic", externalCh)
	if err != nil {
		t.Fatalf("SubscribeExisting failed: %v", err)
	}

	// Subscribe again with same channel is ok
	err = proc.SubscribeExisting("external-topic", externalCh)
	if err != nil {
		t.Fatalf("second SubscribeExisting failed: %v", err)
	}

	// Subscribe with different channel to same topic should fail
	differentCh := NewChannel(5)
	err = proc.SubscribeExisting("external-topic", differentCh)
	if err == nil {
		t.Fatal("expected error when subscribing different channel to same topic")
	}
}

// TestProcessSubscribeExistingWithoutInit tests SubscribeExisting before Init
func TestProcessSubscribeExistingWithoutInit(t *testing.T) {
	script := `return 1`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ch := NewChannel(5)
	err := proc.SubscribeExisting("test-topic", ch)
	if err == nil {
		t.Fatal("expected error when subscribing before Init")
	}
}

// TestProcessTopicHandler tests SetTopicHandler/GetTopicHandler/RemoveTopicHandler
func TestProcessTopicHandler(t *testing.T) {
	script := `return 1`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// No handler initially
	_, ok := proc.GetTopicHandler("test-topic")
	if ok {
		t.Fatal("expected no handler initially")
	}

	// Set handler
	proc.SetTopicHandler("test-topic", func(_ context.Context, _ *lua.LState, _ pid.PID, _ string, _ []payload.Payload) lua.LValue {
		return lua.LTrue
	})

	// Get handler
	h, ok := proc.GetTopicHandler("test-topic")
	if !ok {
		t.Fatal("expected handler to exist")
	}
	if h == nil {
		t.Fatal("expected non-nil handler")
	}

	// Remove handler
	proc.RemoveTopicHandler("test-topic")
	_, ok = proc.GetTopicHandler("test-topic")
	if ok {
		t.Fatal("expected no handler after removal")
	}
}

// TestProcessChannelQueue tests ChannelQueue method
func TestProcessChannelQueue(t *testing.T) {
	script := `return 1`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// Get channel queue (creates if needed)
	q := proc.ChannelQueue()
	if q == nil {
		t.Fatal("expected non-nil queue")
	}

	// Getting again returns same queue
	q2 := proc.ChannelQueue()
	if q2 != q {
		t.Fatal("expected same queue instance")
	}
}

// TestProcessHasSubscriptionsEmpty tests HasSubscriptions with no subscriptions
func TestProcessHasSubscriptionsEmpty(t *testing.T) {
	script := `return 1`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	if proc.HasSubscriptions() {
		t.Fatal("expected no subscriptions")
	}
}

// TestProcessHasSubscriptionsNilSubs tests HasSubscriptions before Init
func TestProcessHasSubscriptionsNilSubs(t *testing.T) {
	script := `return 1`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	// Before Init, subs is nil
	if proc.HasSubscriptions() {
		t.Fatal("expected false when subs is nil")
	}
}

// TestProcessGetProcess tests GetProcess helper
func TestProcessGetProcess(t *testing.T) {
	script := `return 1`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// GetProcess from state should return the process
	retrieved := GetProcess(proc.state)
	if retrieved != proc {
		t.Fatal("expected GetProcess to return the process")
	}

	// GetProcess from state with nil G.Owner should return nil
	emptyState := lua.NewState()
	defer emptyState.Close()
	retrieved = GetProcess(emptyState)
	if retrieved != nil {
		t.Fatal("expected nil for state without Owner")
	}
}

// TestProcessSubscribeYieldsBasic tests processSubscribeYields with subscribe request
func TestProcessSubscribeYieldsBasic(t *testing.T) {
	script := `
		local topic = process.subscribe("test-topic", 10)
		return topic ~= nil
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(wrapBinder(func(l *lua.LState) { LoadModuleDef(l, ChannelModule) })),
		WithModuleBinder(bindProcessModule),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

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
	t.Fatal("did not complete")
}

// TestProcessSubscribeYieldsUnsubscribe tests processSubscribeYields with unsubscribe
func TestProcessSubscribeYieldsUnsubscribe(t *testing.T) {
	script := `
		local ch = process.subscribe("test-topic", 10)
		local ok = process.unsubscribe(ch)
		return ok
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(wrapBinder(func(l *lua.LState) { LoadModuleDef(l, ChannelModule) })),
		WithModuleBinder(bindProcessModule),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

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
	t.Fatal("did not complete")
}

// bindProcessModule binds a minimal process module for subscribe testing
func bindProcessModule(l *lua.LState) error {
	mod := l.NewTable()

	// subscribe(topic, bufsize) -> channel
	mod.RawSetString("subscribe", l.NewFunction(func(l *lua.LState) int {
		topic := l.CheckString(1)
		bufSize := l.OptInt(2, 0)

		req := &SubscribeRequest{
			Topic:   topic,
			BufSize: bufSize,
		}
		l.Push(req)
		return -1 // yield
	}))

	// unsubscribe(channel) -> bool
	mod.RawSetString("unsubscribe", l.NewFunction(func(l *lua.LState) int {
		ud := l.CheckUserData(1)
		ch, ok := ud.Value.(*Channel)
		if !ok {
			l.Push(lua.LFalse)
			l.Push(lua.LString("not a channel"))
			return 2
		}

		req := &UnsubscribeRequest{Channel: ch}
		l.Push(req)
		return -1 // yield
	}))

	l.SetGlobal("process", mod)
	return nil
}

// TestProcessDeliverMessageBasic tests deliverMessage
func TestProcessDeliverMessageBasic(t *testing.T) {
	script := `
		local ch = process.subscribe("test-topic", 10)
		-- Wait for message
		local msg = ch:receive()
		return msg
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(wrapBinder(func(l *lua.LState) { LoadModuleDef(l, ChannelModule) })),
		WithModuleBinder(bindProcessModule),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput

	// Run until subscribed and waiting for message
	for i := 0; i < 5; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step %d failed: %v", i, err)
		}
		if output.Status() == process.StepDone {
			t.Fatal("completed too early")
		}
		if output.Status() == process.StepIdle {
			break
		}
	}

	// Queue a message to the topic
	testPayload := payload.New("hello")
	proc.messageQueue = append(proc.messageQueue, queuedMessage{
		Topic:    "test-topic",
		Source:   testPID(),
		Payloads: []payload.Payload{testPayload},
	})

	// Step should deliver the message
	for i := 0; i < 10; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step %d failed: %v", i, err)
		}
		if output.Status() == process.StepDone {
			return
		}
	}
	t.Fatal("did not complete")
}

// TestProcessDeliverMessageWithHandler tests deliverMessage with topic handler
func TestProcessDeliverMessageWithHandler(t *testing.T) {
	script := `return 1`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// Subscribe to topic
	ch, err := proc.Subscribe("test-topic", 10)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Set topic handler that transforms the message
	proc.SetTopicHandler("test-topic", func(_ context.Context, _ *lua.LState, _ pid.PID, _ string, _ []payload.Payload) lua.LValue {
		return lua.LString("handled")
	})

	// Deliver message
	testPayload := payload.New("original")
	proc.messageQueue = append(proc.messageQueue, queuedMessage{
		Topic:    "test-topic",
		Source:   testPID(),
		Payloads: []payload.Payload{testPayload},
	})

	// Flush messages
	proc.flushMessageQueue(proc.subs)

	// Check channel received transformed message
	result := ch.Receive(nil, nil)
	if result == nil {
		t.Fatal("expected receive result")
	}
	updates := result.GetUpdates()
	if len(updates) != 1 {
		t.Fatal("expected one update")
	}
	if updates[0].GetResult()[0].String() != "handled" {
		t.Fatalf("expected 'handled', got %v", updates[0].GetResult()[0])
	}
	ReleaseResult(result)
}

// TestProcessDeliverMessageToInboxFallback tests deliverMessage inbox fallback
func TestProcessDeliverMessageToInboxFallback(t *testing.T) {
	script := `return 1`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// Subscribe to inbox topic
	ch, err := proc.Subscribe("@inbox", 10)
	if err != nil {
		t.Fatalf("Subscribe to inbox failed: %v", err)
	}

	// Queue message to unknown topic (should fallback to inbox)
	testPayload := payload.New("fallback-message")
	proc.messageQueue = append(proc.messageQueue, queuedMessage{
		Topic:    "unknown-topic",
		Source:   testPID(),
		Payloads: []payload.Payload{testPayload},
	})

	// Flush messages
	proc.flushMessageQueue(proc.subs)

	// Check inbox received the message
	result := ch.Receive(nil, nil)
	if result == nil {
		t.Fatal("expected inbox to receive message")
	}
	ReleaseResult(result)
}

// TestProcessDeliverMessageNoSubscription tests deliverMessage with no subscription
func TestProcessDeliverMessageNoSubscription(t *testing.T) {
	script := `return 1`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// Queue message to non-existent topic
	testPayload := payload.New("lost")
	proc.messageQueue = append(proc.messageQueue, queuedMessage{
		Topic:    "nonexistent",
		Source:   testPID(),
		Payloads: []payload.Payload{testPayload},
	})

	// Flush - message should stay in queue
	proc.flushMessageQueue(proc.subs)

	if len(proc.messageQueue) != 1 {
		t.Fatalf("expected message to remain in queue, got %d", len(proc.messageQueue))
	}
}

// TestProcessDeliverMessageTerminal tests deliverMessage with terminal payload
func TestProcessDeliverMessageTerminal(t *testing.T) {
	script := `return 1`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// Subscribe to topic
	ch, err := proc.Subscribe("test-topic", 10)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Queue terminal payload
	terminalPayload := payload.NewPayload(nil, payload.Terminal)
	proc.messageQueue = append(proc.messageQueue, queuedMessage{
		Topic:    "test-topic",
		Source:   testPID(),
		Payloads: []payload.Payload{terminalPayload},
	})

	// Flush - terminal should close channel
	proc.flushMessageQueue(proc.subs)

	if len(proc.messageQueue) != 0 {
		t.Fatalf("expected empty queue after terminal, got %d", len(proc.messageQueue))
	}

	// Channel should be closed
	if !ch.IsClosed() {
		t.Fatal("channel should be closed after terminal")
	}
}

// TestProcessDeliverMessageWithTerminalAtEnd tests multi-payload message with terminal at end
func TestProcessDeliverMessageWithTerminalAtEnd(t *testing.T) {
	script := `return 1`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// Subscribe to topic
	ch, err := proc.Subscribe("test-topic", 10)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Queue message with result + terminal pattern
	dataPayload := payload.New("result-data")
	terminalPayload := payload.NewPayload(nil, payload.Terminal)
	proc.messageQueue = append(proc.messageQueue, queuedMessage{
		Topic:    "test-topic",
		Source:   testPID(),
		Payloads: []payload.Payload{dataPayload, terminalPayload},
	})

	// Flush - should deliver data then close channel
	proc.flushMessageQueue(proc.subs)

	if len(proc.messageQueue) != 0 {
		t.Fatalf("expected empty queue, got %d", len(proc.messageQueue))
	}

	// Channel should be closed
	if !ch.IsClosed() {
		t.Fatal("channel should be closed after terminal")
	}
}

// TestProcessDeliverMessageHandlerReturnsNil tests handler that returns nil
func TestProcessDeliverMessageHandlerReturnsNil(t *testing.T) {
	script := `return 1`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// Subscribe to topic
	_, err := proc.Subscribe("test-topic", 10)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Set handler that returns nil (doesn't want to send to channel)
	proc.SetTopicHandler("test-topic", func(_ context.Context, _ *lua.LState, _ pid.PID, _ string, _ []payload.Payload) lua.LValue {
		return nil
	})

	// Queue message
	dataPayload := payload.New("data")
	proc.messageQueue = append(proc.messageQueue, queuedMessage{
		Topic:    "test-topic",
		Source:   testPID(),
		Payloads: []payload.Payload{dataPayload},
	})

	// Flush - handler returns nil, message handled but nothing sent to channel
	proc.flushMessageQueue(proc.subs)

	if len(proc.messageQueue) != 0 {
		t.Fatalf("expected empty queue, got %d", len(proc.messageQueue))
	}
}

// TestProcessDeliverMessageHandlerWithTerminal tests handler returning nil with terminal
func TestProcessDeliverMessageHandlerWithTerminal(t *testing.T) {
	script := `return 1`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// Subscribe to topic
	ch, err := proc.Subscribe("test-topic", 10)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Set handler that returns nil
	proc.SetTopicHandler("test-topic", func(_ context.Context, _ *lua.LState, _ pid.PID, _ string, _ []payload.Payload) lua.LValue {
		return nil
	})

	// Queue message with data + terminal
	dataPayload := payload.New("data")
	terminalPayload := payload.NewPayload(nil, payload.Terminal)
	proc.messageQueue = append(proc.messageQueue, queuedMessage{
		Topic:    "test-topic",
		Source:   testPID(),
		Payloads: []payload.Payload{dataPayload, terminalPayload},
	})

	// Flush - handler returns nil + terminal should close channel
	proc.flushMessageQueue(proc.subs)

	if !ch.IsClosed() {
		t.Fatal("channel should be closed after handler with terminal")
	}
}

// TestProcessDeliverMessageAtTopic tests message to @ prefixed topic without inbox fallback
func TestProcessDeliverMessageAtTopic(t *testing.T) {
	script := `return 1`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// Subscribe to inbox
	_, err := proc.Subscribe("@inbox", 10)
	if err != nil {
		t.Fatal(err)
	}

	// Queue message to @ prefixed topic that doesn't exist
	dataPayload := payload.New("data")
	proc.messageQueue = append(proc.messageQueue, queuedMessage{
		Topic:    "@nonexistent",
		Source:   testPID(),
		Payloads: []payload.Payload{dataPayload},
	})

	// Flush - @ topics don't fallback to inbox, so message stays in queue
	proc.flushMessageQueue(proc.subs)

	if len(proc.messageQueue) != 1 {
		t.Fatalf("expected 1 message in queue (no fallback for @ topics), got %d", len(proc.messageQueue))
	}
}

// TestProcessResumeTaskWithResultHandledYield tests resumeTaskWithResult with HandledYield
func TestProcessResumeTaskWithResultHandledYield(t *testing.T) {
	script := `
		local result = test_yield(10)
		return result
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(bindTestYield),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatal(err)
	}

	// Should yield external
	if output.Count() != 1 {
		t.Fatalf("expected 1 yield, got %d", output.Count())
	}

	// Resume with result
	events := []process.Event{{
		Type: process.EventYieldComplete,
		Tag:  output.Yields()[0].Tag,
		Data: []lua.LValue{lua.LString("test-result")},
	}}

	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatal(err)
	}

	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}
}

// TestProcessResumeTaskWithResultLuaValues tests resumeTaskWithResult with raw LValues
func TestProcessResumeTaskWithResultLuaValues(t *testing.T) {
	script := `
		local result = test_yield(10)
		return result
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(bindTestYield),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatal(err)
	}

	if output.Count() != 1 {
		t.Fatalf("expected 1 yield, got %d", output.Count())
	}

	// Resume with raw lua values
	events := []process.Event{{
		Type: process.EventYieldComplete,
		Tag:  output.Yields()[0].Tag,
		Data: []lua.LValue{lua.LNumber(42)},
	}}

	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatal(err)
	}

	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}
}

// TestProcessState tests State method
func TestProcessState(t *testing.T) {
	script := `return 1`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	state := proc.State()
	if state == nil {
		t.Fatal("expected non-nil state")
	}
	if state != proc.state {
		t.Fatal("State() should return internal state")
	}
}

// TestProcessGetTasks tests GetTasks method
// Note: main returning kills all threads (like Go), so main must block to keep workers alive
func TestProcessGetTasks(t *testing.T) {
	script := `
		local ch = channel.new(0)
		coroutine.spawn(function() ch:receive() end)
		coroutine.spawn(function() ch:receive() end)
		ch:receive() -- main blocks to keep workers alive
	`
	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(wrapBinder(func(l *lua.LState) { LoadModuleDef(l, ChannelModule) })),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// Step to create spawned threads
	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatal(err)
	}

	tasks := proc.GetTasks()
	// Should have at least the spawned tasks plus main
	if len(tasks) < 3 {
		t.Fatalf("expected at least 3 tasks, got %d", len(tasks))
	}
}

// TestProcessQueue tests Queue method
func TestProcessQueue(t *testing.T) {
	script := `return 1`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	queue := proc.Queue()
	if queue == nil {
		t.Fatal("expected non-nil queue")
	}
	if queue != proc.queue {
		t.Fatal("Queue() should return internal queue")
	}
}

// TestProcessWithProto tests WithProto option
func TestProcessWithProto(t *testing.T) {
	// First compile script to proto
	script := `return 42`
	state := lua.NewState()
	defer state.Close()

	fn, err := state.Load(strings.NewReader(script), "test.lua")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	proto := fn.Proto

	// Create process with proto
	proc := mustNewProcess(t, WithProto(proto))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatal(err)
	}

	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}
}

// TestProcessWithStateOptions tests WithStateOptions
func TestProcessWithStateOptions(t *testing.T) {
	script := `return 1`
	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithStateOptions(lua.Options{SkipOpenLibs: false}),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatal(err)
	}

	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}
}

// TestProcessTranscodeToLua tests transcodeToLua paths
func TestProcessTranscodeToLua(t *testing.T) {
	script := `return 1`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// Test nil payload
	result := transcodeToLua(ctx, nil)
	if result != lua.LNil {
		t.Fatal("expected LNil for nil payload")
	}

	// Test Lua format payload
	luaPayload := payload.NewPayload(lua.LNumber(123), payload.Lua)
	result = transcodeToLua(ctx, luaPayload)
	if result != lua.LNumber(123) {
		t.Fatal("expected lua value to pass through")
	}

	// Test non-Lua payload without transcoder returns nil
	stringPayload := payload.New("test-string")
	result = transcodeToLua(ctx, stringPayload)
	if result != lua.LNil {
		t.Fatal("expected LNil for non-Lua payload without transcoder")
	}
}

// TestProcessPayloadsToLua tests PayloadsToLua function
func TestProcessPayloadsToLua(t *testing.T) {
	script := `return 1`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// Empty payloads
	result := PayloadsToLua(ctx, proc.state, nil)
	if result != lua.LNil {
		t.Fatal("expected LNil for empty payloads")
	}

	// Single payload
	p1 := payload.NewPayload(lua.LString("single"), payload.Lua)
	result = PayloadsToLua(ctx, proc.state, []payload.Payload{p1})
	if result.String() != "single" {
		t.Fatalf("expected 'single', got %v", result)
	}

	// Multiple payloads
	p2 := payload.NewPayload(lua.LString("second"), payload.Lua)
	result = PayloadsToLua(ctx, proc.state, []payload.Payload{p1, p2})
	tbl, ok := result.(*lua.LTable)
	if !ok {
		t.Fatal("expected table for multiple payloads")
	}
	if tbl.Len() != 2 {
		t.Fatalf("expected 2 elements, got %d", tbl.Len())
	}
}

// TestProcessSyncExecute tests SyncExecute method
func TestProcessSyncExecute(t *testing.T) {
	// Compile script to proto first
	script := `return 42`
	state := lua.NewState()
	fn, err := state.Load(strings.NewReader(script), "test.lua")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	proto := fn.Proto
	state.Close()

	proc := mustNewProcess(t, WithProto(proto))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	// SyncExecute doesn't need Init - it runs directly
	result, err := proc.SyncExecute(ctx)
	if err != nil {
		t.Fatalf("SyncExecute failed: %v", err)
	}
	// Result can be LNumber or LInteger depending on Lua VM
	if result.String() != "42" {
		t.Fatalf("expected 42, got %v", result)
	}
}

// TestProcessWithModuleBinderFirst tests WithModuleBinder as first option (factory init)
func TestProcessWithModuleBinderFirst(t *testing.T) {
	binderCalled := false
	binder := func(L *lua.LState) error {
		binderCalled = true
		L.SetGlobal("test_var", lua.LNumber(123))
		return nil
	}

	// WithModuleBinder as first option - exercises factory nil check
	proc := mustNewProcess(t,
		WithModuleBinder(binder),
		WithScript(`return test_var`, "test.lua"),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatal(err)
	}

	if !binderCalled {
		t.Fatal("binder was not called")
	}
	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}
}

// TestProcessWithStateOptionsFirst tests WithStateOptions as first option (factory init)
func TestProcessWithStateOptionsFirst(t *testing.T) {
	// WithStateOptions as first option - exercises factory nil check
	proc := mustNewProcess(t,
		WithStateOptions(lua.Options{SkipOpenLibs: false}),
		WithScript(`return 1`, "test.lua"),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatal(err)
	}

	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}
}

// mockTranscoder implements payload.Transcoder for testing
type mockTranscoder struct{}

func (m *mockTranscoder) Transcode(p payload.Payload, f payload.Format) (payload.Payload, error) {
	// Convert to Lua format by wrapping data as LString
	if f == payload.Lua {
		return payload.NewPayload(lua.LString(fmt.Sprintf("%v", p.Data())), payload.Lua), nil
	}
	return payload.NewPayload(p.Data(), f), nil
}

func (m *mockTranscoder) Unmarshal(_ payload.Payload, _ any) error {
	return nil
}

// TestProcessTranscodeToLuaWithTranscoder tests transcodeToLua with context transcoder
func TestProcessTranscodeToLuaWithTranscoder(t *testing.T) {
	script := `return 1`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	// Create context with AppContext and attach transcoder
	ctx := context.Background()
	ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())
	ctx = payload.WithTranscoder(ctx, &mockTranscoder{})

	// Wrap with frame context for process
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// Test transcoding with transcoder in context
	jsonPayload := payload.NewPayload("test-data", payload.JSON)
	result := transcodeToLua(ctx, jsonPayload)

	// Should use transcoder to convert to Lua
	if result == lua.LNil {
		t.Fatal("expected non-nil result from transcoder")
	}
	if result.String() != "test-data" {
		t.Fatalf("expected 'test-data', got %v", result)
	}
}

// TestProcessWrapErrorNil tests wrapError with nil error
func TestProcessWrapErrorNil(t *testing.T) {
	script := `return 1`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// wrapError with nil should return nil
	result := proc.wrapError(proc.state, nil)
	if result != nil {
		t.Fatal("expected nil for nil error")
	}
}

// TestProcessWrapErrorAlreadyWrapped tests wrapError with already-wrapped lua.Error
func TestProcessWrapErrorAlreadyWrapped(t *testing.T) {
	script := `return 1`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// Create a lua.Error directly
	luaErr := lua.NewLuaError(proc.state, "already wrapped error")

	// wrapError should return the same error
	result := proc.wrapError(proc.state, luaErr)
	if !errors.Is(result, luaErr) {
		t.Fatal("expected same lua.Error to be returned")
	}
}

// TestProcessWrapErrorNilThread tests wrapError with nil thread (fallback to p.state)
func TestProcessWrapErrorNilThread(t *testing.T) {
	script := `return 1`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// wrapError with nil thread should use p.state
	plainErr := fmt.Errorf("plain error")
	result := proc.wrapError(nil, plainErr)

	if result == nil {
		t.Fatal("expected wrapped error")
	}

	// Should be wrapped as lua.Error
	luaErr := lua.GetError(result)
	if luaErr == nil {
		t.Fatal("expected lua.Error wrapper")
	}
}

// TestProcessWrapErrorPlainError tests wrapError with plain error
func TestProcessWrapErrorPlainError(t *testing.T) {
	script := `return 1`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// wrapError with plain error should wrap it
	plainErr := fmt.Errorf("plain error message")
	result := proc.wrapError(proc.state, plainErr)

	if result == nil {
		t.Fatal("expected wrapped error")
	}

	// Should contain original message
	if !strings.Contains(result.Error(), "plain error message") {
		t.Fatalf("expected error to contain message, got: %v", result)
	}
}

// TestProcessSubscribeError tests Subscribe returning error path
func TestProcessSubscribeError(t *testing.T) {
	script := `return 1`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	// Don't init - subs will be nil
	_, err := proc.Subscribe("topic", 10)
	if err == nil {
		t.Fatal("expected error when subscribing without init")
	}
}

// TestProcessMultipleModuleBinders tests multiple WithModuleBinder calls
func TestProcessMultipleModuleBinders(t *testing.T) {
	binder1Called := false
	binder2Called := false

	proc := mustNewProcess(t,
		WithModuleBinder(wrapBinder(func(L *lua.LState) {
			binder1Called = true
			L.SetGlobal("var1", lua.LNumber(1))
		})),
		WithModuleBinder(wrapBinder(func(L *lua.LState) {
			binder2Called = true
			L.SetGlobal("var2", lua.LNumber(2))
		})),
		WithScript(`return var1 + var2`, "test.lua"),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatal(err)
	}

	if !binder1Called || !binder2Called {
		t.Fatal("both binders should be called")
	}
}

// TestProcessSyncExecuteNotInitialized tests SyncExecute with nil state
func TestProcessSyncExecuteNotInitialized(t *testing.T) {
	proc := mustNewProcess(t, WithScript(`return 1`, "test.lua"))
	// Don't initialize state
	_, err := proc.SyncExecute(context.Background())
	if err == nil {
		t.Fatal("expected error for uninitialized state")
	}
}

// TestProcessSyncExecuteError tests SyncExecute with script error
func TestProcessSyncExecuteError(t *testing.T) {
	script := `error("test error")`
	state := lua.NewState()
	fn, err := state.Load(strings.NewReader(script), "test.lua")
	if err != nil {
		t.Fatal(err)
	}
	proto := fn.Proto
	state.Close()

	proc := mustNewProcess(t, WithProto(proto))
	// Create state manually
	proc.state = lua.NewState()
	defer proc.Close()

	_, err = proc.SyncExecute(context.Background())
	if err == nil {
		t.Fatal("expected error from script")
	}
}

// TestProcessDistributeEventZeroTag tests distributeEvent with tag=0
func TestProcessDistributeEventZeroTag(t *testing.T) {
	proc := mustNewProcess(t, WithScript(`return 1`, "test.lua"))
	proc.state = lua.NewState()
	defer proc.Close()

	proc.pendingYields = make(map[uint64]*Task)
	task := &Task{State: lua.ResumeYield}
	proc.pendingYields[1] = task

	// Call distributeEvent directly with tag=0 - should return early
	proc.distributeEvent(process.Event{Type: process.EventYieldComplete, Tag: 0})

	// Task should still be pending (not processed)
	if _, exists := proc.pendingYields[1]; !exists {
		t.Fatal("task should still be pending when tag=0")
	}
}

// TestProcessDistributeEventNoPendingYields tests distributeEvent with empty pendingYields
func TestProcessDistributeEventNoPendingYields(t *testing.T) {
	proc := mustNewProcess(t, WithScript(`return 1`, "test.lua"))
	proc.state = lua.NewState()
	defer proc.Close()

	// Don't set pendingYields
	proc.distributeEvent(process.Event{Type: process.EventYieldComplete, Tag: 1})
	// Should return early without panic
}

// TestProcessDistributeEventNonExistentTag tests distributeEvent with non-existent tag
func TestProcessDistributeEventNonExistentTag(t *testing.T) {
	proc := mustNewProcess(t, WithScript(`return 1`, "test.lua"))
	proc.state = lua.NewState()
	defer proc.Close()

	proc.pendingYields = make(map[uint64]*Task)
	proc.pendingYields[1] = &Task{State: lua.ResumeYield}

	// Call with non-existent tag
	proc.distributeEvent(process.Event{Type: process.EventYieldComplete, Tag: 999})

	// Original task should still be pending
	if _, exists := proc.pendingYields[1]; !exists {
		t.Fatal("task should still be pending")
	}
}

// TestProcessExtractMethodWithScript tests extractMethod using script string
func TestProcessExtractMethodWithScript(t *testing.T) {
	script := `return { handle = function() return "ok" end }`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "handle", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// Verify method was extracted
	if proc.exported == nil || proc.exported["handle"] == nil {
		t.Fatal("handle method should be extracted")
	}
}

// TestProcessExtractMethodNotFound tests extractMethod with missing method
func TestProcessExtractMethodNotFound(t *testing.T) {
	script := `return { other = function() return "ok" end }`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	err := proc.Init(ctx, "nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for missing method")
	}
}

// TestProcessExtractMethodScriptReturnsFunction tests extractMethod with direct function return
func TestProcessExtractMethodScriptReturnsFunction(t *testing.T) {
	script := `return function() return "direct" end`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "handle", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// Should work - direct function return gets stored
	if proc.exported == nil || proc.exported["handle"] == nil {
		t.Fatal("direct function should be extracted")
	}
}

// TestProcessExtractMethodScriptError tests extractMethod with script execution error
func TestProcessExtractMethodScriptError(t *testing.T) {
	script := `error("init error")`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	err := proc.Init(ctx, "handle", nil)
	if err == nil {
		t.Fatal("expected error from script execution")
	}
}

// TestProcessExtractMethodLoadError tests extractMethod with invalid script
func TestProcessExtractMethodLoadError(t *testing.T) {
	script := `this is not valid lua`
	proc := mustNewProcess(t, WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	err := proc.Init(ctx, "handle", nil)
	if err == nil {
		t.Fatal("expected load error")
	}
}

// TestProcessYieldToCommandEmpty tests yieldToCommand with empty yields
func TestProcessYieldToCommandEmpty(t *testing.T) {
	proc := mustNewProcess(t, WithScript(`return 1`, "test.lua"))
	task := &Task{Yielded: nil}
	cmd := proc.yieldToCommand(task)
	if cmd != nil {
		t.Fatal("expected nil command for empty yields")
	}
}

// TestProcessResumeTaskNonHandledYield tests resumeTaskWithResult with plain LValues
func TestProcessResumeTaskNonHandledYield(t *testing.T) {
	proc := mustNewProcess(t, WithScript(`return 1`, "test.lua"))
	proc.state = lua.NewState()
	defer proc.Close()

	proc.queue = NewTaskQueue()

	// Task with non-HandledYield value
	task := &Task{
		State:   lua.ResumeYield,
		Yielded: []lua.LValue{lua.LString("not a handled yield")},
	}

	// Pass lua values directly
	proc.resumeTaskWithResult(task, []lua.LValue{lua.LNumber(42)}, nil)

	// Task should be queued
	if proc.queue.IsEmpty() {
		t.Fatal("task should be queued")
	}
}

// TestProcessResumeTaskEmptyYieldsWithLValues tests resumeTaskWithResult with empty yields but LValues data
func TestProcessResumeTaskEmptyYieldsWithLValues(t *testing.T) {
	proc := mustNewProcess(t, WithScript(`return 1`, "test.lua"))
	proc.state = lua.NewState()
	defer proc.Close()

	proc.queue = NewTaskQueue()

	task := &Task{
		State:   lua.ResumeYield,
		Yielded: nil, // empty
	}

	proc.resumeTaskWithResult(task, []lua.LValue{lua.LNumber(99)}, nil)

	if proc.queue.IsEmpty() {
		t.Fatal("task should be queued")
	}
	if len(task.Resumed) != 1 {
		t.Fatalf("expected 1 resumed value, got %d", len(task.Resumed))
	}
}

// TestProcessResumeTaskNotYieldState tests resumeTaskWithResult when task not in yield state
func TestProcessResumeTaskNotYieldState(t *testing.T) {
	proc := mustNewProcess(t, WithScript(`return 1`, "test.lua"))
	proc.state = lua.NewState()
	defer proc.Close()

	proc.queue = NewTaskQueue()

	task := &Task{
		State:   lua.ResumeOK, // not yielded
		Yielded: nil,
	}

	proc.resumeTaskWithResult(task, []lua.LValue{lua.LNumber(1)}, nil)

	// Task should NOT be queued since state is not ResumeYield
	if !proc.queue.IsEmpty() {
		t.Fatal("task should not be queued when not in yield state")
	}
}

// TestDistributedWorkWithExternalYields tests multiple coroutines yielding external operations
// while main coroutine is blocked on channel receive.
// This simulates: 3 workers each process 2 jobs with time.sleep, main waits for results.
// Matches distributed_work.lua Test 1 pattern.
func TestDistributedWorkWithExternalYields(t *testing.T) {
	script := `
		local work_queue = channel.new(10)
		local results = channel.new(10)
		local worker_count = 3
		local job_count = 6

		-- Spawn workers that simulate processing time (like distributed_work.lua)
		for w = 1, worker_count do
			coroutine.spawn(function()
				while true do
					local job, ok = work_queue:receive()
					if not ok then break end
					test_yield(10)  -- simulate time.sleep
					results:send({worker = w, job = job, result = job * 2})
				end
			end)
		end

		-- Producer sends jobs
		for i = 1, job_count do
			work_queue:send(i)
		end
		work_queue:close()

		-- Collect results
		local total = 0
		for i = 1, job_count do
			local r = results:receive()
			total = total + r.result
		end

		return total
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(wrapBinder(func(l *lua.LState) { LoadModuleDef(l, ChannelModule) })),
		WithModuleBinder(bindTestYield),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	var pendingEvents []process.Event

	// Run steps until done, completing yields as they come
	maxSteps := 100
	for step := 0; step < maxSteps; step++ {
		output.Reset()
		if err := proc.Step(pendingEvents, &output); err != nil {
			t.Fatalf("Step %d failed: %v", step, err)
		}
		pendingEvents = nil

		if output.Status() == process.StepDone {
			break
		}

		// Collect any new yields to complete on next step
		if output.Count() > 0 {
			yields := output.Yields()
			for _, y := range yields {
				pendingEvents = append(pendingEvents, process.Event{
					Type: process.EventYieldComplete,
					Tag:  y.Tag,
				})
			}
		}
	}

	if output.Status() != process.StepDone {
		t.Fatalf("expected StepDone, got %d", output.Status())
	}

	// Result should be sum of (1+2+3+4+5+6)*2 = 42
	result := output.Result()
	if result == nil {
		t.Fatal("expected result")
	}
}

// TestExternalYieldLostOnSubscribeLoop reproduces the bug where external yields
// are lost when the Step loop continues due to subscription handling.
// Bug: processChannelYields clears p.externalTasks at the start, but the outer
// loop in Step() may call it multiple times when hadSubs=true, losing yields.
func TestExternalYieldLostOnSubscribeLoop(t *testing.T) {
	// Script that:
	// 1. Spawns a coroutine that subscribes (yields SubscribeRequest)
	// 2. Main thread yields externally (test_yield)
	// Both yields happen in the same Step, triggering hadSubs=true loop
	script := `
		local result = nil

		-- Spawn a coroutine that subscribes
		coroutine.spawn(function()
			local ch = process.subscribe("test-topic", 10)
			-- Wait on channel after subscribe
			local msg = ch:receive()
			result = "got:" .. tostring(msg)
		end)

		-- Main yields externally - this yield should NOT be lost
		local yield_result = test_yield(100)

		-- If we get here, the yield was properly dispatched and completed
		return "yield_ok:" .. tostring(yield_result)
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(wrapBinder(func(l *lua.LState) { LoadModuleDef(l, ChannelModule) })),
		WithModuleBinder(bindProcessModule),
		WithModuleBinder(bindTestYield),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput

	// Step 1: Both tasks run - one subscribes, one yields externally
	output.Reset()
	if err := proc.Step(nil, &output); err != nil {
		t.Fatalf("Step 1 error: %v", err)
	}
	t.Logf("Step 1: status=%d, yields=%d, threads=%d",
		output.Status(), output.Count(), len(proc.threads))

	// Bug check: If the external yield was lost, we'll get StepIdle instead of StepYield
	if output.Status() == process.StepIdle {
		t.Fatal("BUG REPRODUCED: External yield was lost when Step loop continued due to subscription!")
	}

	// Should have a pending yield (the test_yield)
	if output.Status() != process.StepYield {
		t.Fatalf("Expected StepYield (3), got status=%d", output.Status())
	}

	if output.Count() == 0 {
		t.Fatal("Expected at least one yield to be dispatched")
	}

	// Get the yield tag and complete it
	yields := output.Yields()
	t.Logf("Got %d yields", len(yields))

	// Complete the external yield
	events := make([]process.Event, 0, len(yields))
	for _, y := range yields {
		t.Logf("Completing yield tag=%d, cmd=%d", y.Tag, y.Cmd.CmdID())
		events = append(events, process.Event{
			Type: process.EventYieldComplete,
			Tag:  y.Tag,
			Data: "completed",
		})
	}

	// Step 2: Complete the yield
	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatalf("Step 2 error: %v", err)
	}
	t.Logf("Step 2: status=%d, threads=%d", output.Status(), len(proc.threads))

	// Run until done or stuck
	for i := 0; i < 20; i++ {
		if output.Status() == process.StepDone {
			break
		}
		if output.Status() == process.StepIdle {
			// Idle is okay - waiting for messages on subscription
			break
		}
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step %d error: %v", i+3, err)
		}
		t.Logf("Step %d: status=%d", i+3, output.Status())
	}

	// Check we got to a valid final state
	switch output.Status() {
	case process.StepDone:
		t.Log("Process completed successfully - yield was NOT lost")
	case process.StepIdle:
		// This is expected - main completed but worker is waiting for message
		t.Log("Process is idle (main completed, worker waiting for message) - yield was NOT lost")
	case process.StepContinue, process.StepYield, process.StepUpgrade:
		t.Fatalf("Unexpected final status: %d", output.Status())
	}
}

// HandledYield Tests
//
// These tests verify the yield -> dispatcher -> HandleResult -> resume flow.

const handledYieldTestCmdID dispatcher.CommandID = 9999

// mockHandledYield implements luaapi.HandledYield for testing.
type mockHandledYield struct {
	response any
	err      error
	cmdID    dispatcher.CommandID
}

func (y *mockHandledYield) String() string       { return "<mock_yield>" }
func (y *mockHandledYield) Type() lua.LValueType { return lua.LTUserData }

func (y *mockHandledYield) CmdID() dispatcher.CommandID   { return y.cmdID }
func (y *mockHandledYield) ToCommand() dispatcher.Command { return y }
func (y *mockHandledYield) Release()                      {}
func (y *mockHandledYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}
	if s, ok := data.(string); ok {
		return []lua.LValue{lua.LString(s), lua.LNil}
	}
	if n, ok := data.(int); ok {
		return []lua.LValue{lua.LNumber(n), lua.LNil}
	}
	if tbl, ok := data.(map[string]any); ok {
		t := l.CreateTable(0, len(tbl))
		for k, v := range tbl {
			switch val := v.(type) {
			case string:
				t.RawSetString(k, lua.LString(val))
			case int:
				t.RawSetString(k, lua.LNumber(val))
			}
		}
		return []lua.LValue{t, lua.LNil}
	}
	if ud, ok := data.(*lua.LUserData); ok {
		return []lua.LValue{ud, lua.LNil}
	}
	return []lua.LValue{lua.LNil, lua.LNil}
}

var _ luaapi.HandledYield = (*mockHandledYield)(nil)
var _ luaapi.YieldConverter = (*mockHandledYield)(nil)

// mockYieldResource simulates a resource like sql.Statement
type mockYieldResource struct {
	name string
}

func (r *mockYieldResource) Name() string { return r.name }

// mockYieldPrepareResponse simulates sqlapi.PrepareResponse
type mockYieldPrepareResponse struct {
	Resource *mockYieldResource
	Error    error
}

// sqlLikeHandledYield simulates the exact pattern SQL uses with HandleResult
type sqlLikeHandledYield struct {
	wrapResult func(*mockYieldResource) lua.LValue
	cmdID      dispatcher.CommandID
}

func (y *sqlLikeHandledYield) String() string                { return "<sql_like_yield>" }
func (y *sqlLikeHandledYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *sqlLikeHandledYield) CmdID() dispatcher.CommandID   { return y.cmdID }
func (y *sqlLikeHandledYield) ToCommand() dispatcher.Command { return y }
func (y *sqlLikeHandledYield) Release()                      {}

func (y *sqlLikeHandledYield) HandleResult(_ *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}
	resp, ok := data.(mockYieldPrepareResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.LString("invalid response type")}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, lua.LString(resp.Error.Error())}
	}
	if y.wrapResult == nil {
		return []lua.LValue{lua.LNil, lua.LString("no wrapper")}
	}
	return []lua.LValue{y.wrapResult(resp.Resource), lua.LNil}
}

// sqlLikeYieldModuleBinder binds a function that yields exactly like SQL does.
func sqlLikeYieldModuleBinder(l *lua.LState) error {
	mod := l.CreateTable(0, 1)
	mod.RawSetString("prepare", lua.LGoFunc(func(l *lua.LState) int {
		yield := &sqlLikeHandledYield{
			cmdID: handledYieldTestCmdID,
			wrapResult: func(r *mockYieldResource) lua.LValue {
				ud := l.NewUserData()
				ud.Value = r
				return ud
			},
		}
		l.Push(yield)
		return -1
	}))
	l.SetGlobal("sqlmod", mod)
	return nil
}

// mockHandledYieldModuleBinder binds a test function that yields mockHandledYield.
func mockHandledYieldModuleBinder(response any, err error) ModuleBinder {
	return func(l *lua.LState) error {
		mod := l.CreateTable(0, 1)
		mod.RawSetString("fetch", lua.LGoFunc(func(l *lua.LState) int {
			yield := &mockHandledYield{
				cmdID:    handledYieldTestCmdID,
				response: response,
				err:      err,
			}
			l.Push(yield)
			return -1
		}))
		l.SetGlobal("testmod", mod)
		return nil
	}
}

func TestYieldHandlerSuccessFlow(t *testing.T) {
	script := `
		local result, err = testmod.fetch()
		if err then
			return nil, err
		end
		return result
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(mockHandledYieldModuleBinder("hello world", nil)),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatalf("First Step failed: %v", err)
	}

	if output.Count() != 1 {
		t.Fatalf("Expected 1 yield, got %d", output.Count())
	}

	yields := output.Yields()
	if yields[0].Cmd.CmdID() != handledYieldTestCmdID {
		t.Errorf("Expected command ID %d, got %d", handledYieldTestCmdID, yields[0].Cmd.CmdID())
	}

	events := []process.Event{
		{
			Type:  process.EventYieldComplete,
			Tag:   yields[0].Tag,
			Data:  "hello world",
			Error: nil,
		},
	}

	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatalf("Second Step failed: %v", err)
	}

	if output.Status() != process.StepDone {
		t.Fatalf("Expected StepDone, got %v", output.Status())
	}

	result := output.Result()
	if result == nil {
		t.Fatal("Expected result, got nil")
	}
}

func TestYieldHandlerErrorFlow(t *testing.T) {
	script := `
		local result, err = testmod.fetch()
		if err then
			return nil, "got error: " .. tostring(err)
		end
		return result
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(mockHandledYieldModuleBinder(nil, errors.New("connection failed"))),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatalf("First Step failed: %v", err)
	}

	if output.Count() != 1 {
		t.Fatalf("Expected 1 yield, got %d", output.Count())
	}

	yields := output.Yields()
	events := []process.Event{
		{
			Type:  process.EventYieldComplete,
			Tag:   yields[0].Tag,
			Data:  nil,
			Error: errors.New("connection failed"),
		},
	}

	output.Reset()
	stepErr := proc.Step(events, &output)

	if output.Status() != process.StepDone {
		t.Fatalf("Expected StepDone, got %v", output.Status())
	}

	if stepErr == nil {
		t.Fatal("Expected error from Lua returning error as second value")
	}

	if !containsString(stepErr.Error(), "got error") {
		t.Errorf("Error should contain 'got error', got: %s", stepErr.Error())
	}
}

func TestYieldHandlerTableResponse(t *testing.T) {
	script := `
		local result, err = testmod.fetch()
		if err then
			return nil, err
		end
		if result.name ~= "test" then
			return nil, "name mismatch"
		end
		if result.count ~= 42 then
			return nil, "count mismatch"
		end
		return "success"
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(mockHandledYieldModuleBinder(map[string]any{"name": "test", "count": 42}, nil)),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatalf("First Step failed: %v", err)
	}

	yields := output.Yields()
	events := []process.Event{
		{
			Type:  process.EventYieldComplete,
			Tag:   yields[0].Tag,
			Data:  map[string]any{"name": "test", "count": 42},
			Error: nil,
		},
	}

	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatalf("Second Step failed: %v", err)
	}

	if output.Status() != process.StepDone {
		t.Fatalf("Expected StepDone, got %v", output.Status())
	}
}

func TestYieldHandlerMultipleYields(t *testing.T) {
	script := `
		local a, err1 = testmod.fetch()
		if err1 then return nil, err1 end

		local b, err2 = testmod.fetch()
		if err2 then return nil, err2 end

		return a .. " " .. b
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(mockHandledYieldModuleBinder("first", nil)),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatalf("First Step failed: %v", err)
	}

	if output.Count() != 1 {
		t.Fatalf("Expected 1 yield, got %d", output.Count())
	}

	yields := output.Yields()
	events := []process.Event{
		{
			Type:  process.EventYieldComplete,
			Tag:   yields[0].Tag,
			Data:  "first",
			Error: nil,
		},
	}

	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatalf("Second Step failed: %v", err)
	}

	if output.Count() != 1 {
		t.Fatalf("Expected 1 yield for second fetch, got %d", output.Count())
	}

	yields = output.Yields()
	events = []process.Event{
		{
			Type:  process.EventYieldComplete,
			Tag:   yields[0].Tag,
			Data:  "second",
			Error: nil,
		},
	}

	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatalf("Third Step failed: %v", err)
	}

	if output.Status() != process.StepDone {
		t.Fatalf("Expected StepDone, got %v", output.Status())
	}
}

func TestYieldHandlerUserdataResponse(t *testing.T) {
	script := `
		local resource, err = testmod.fetch()
		if err then
			return nil, "got error: " .. tostring(err)
		end
		if resource == nil then
			return nil, "resource is nil"
		end
		local name = resource:name()
		if name ~= "test_resource" then
			return nil, "expected name 'test_resource', got: " .. tostring(name)
		end
		return "success"
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(mockHandledYieldModuleBinder(nil, nil)),
		WithModuleBinder(wrapBinder(func(l *lua.LState) {
			mt := l.NewTypeMetatable("mockYieldResource")
			l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
				"name": func(l *lua.LState) int {
					ud := l.CheckUserData(1)
					if res, ok := ud.Value.(*mockYieldResource); ok {
						l.Push(lua.LString(res.Name()))
						return 1
					}
					return 0
				},
			}))
		})),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatalf("First Step failed: %v", err)
	}

	if output.Count() != 1 {
		t.Fatalf("Expected 1 yield, got %d", output.Count())
	}

	ud := proc.State().NewUserData()
	ud.Value = &mockYieldResource{name: "test_resource"}
	ud.Metatable = proc.State().GetTypeMetatable("mockYieldResource")

	yields := output.Yields()
	events := []process.Event{
		{
			Type:  process.EventYieldComplete,
			Tag:   yields[0].Tag,
			Data:  ud,
			Error: nil,
		},
	}

	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatalf("Second Step failed: %v", err)
	}

	if output.Status() != process.StepDone {
		t.Fatalf("Expected StepDone, got %v", output.Status())
	}
}

func TestYieldHandlerUserdataWithNilError(t *testing.T) {
	script := `
		local resource, err = testmod.fetch()

		if err ~= nil then
			return nil, "err should be nil but got: " .. type(err) .. " = " .. tostring(err)
		end

		if resource == nil then
			return nil, "resource should not be nil"
		end

		if type(resource) ~= "userdata" then
			return nil, "resource should be userdata, got: " .. type(resource)
		end

		return "success"
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(mockHandledYieldModuleBinder(nil, nil)),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatalf("First Step failed: %v", err)
	}

	ud := proc.State().NewUserData()
	ud.Value = &mockYieldResource{name: "test"}

	yields := output.Yields()
	events := []process.Event{
		{
			Type:  process.EventYieldComplete,
			Tag:   yields[0].Tag,
			Data:  ud,
			Error: nil,
		},
	}

	output.Reset()
	stepErr := proc.Step(events, &output)

	if output.Status() != process.StepDone {
		t.Fatalf("Expected StepDone, got %v", output.Status())
	}

	if stepErr != nil {
		t.Fatalf("Expected no error, got: %v", stepErr)
	}
}

func TestYieldHandlerSQLPattern(t *testing.T) {
	script := `
		local stmt, err = sqlmod.prepare()

		if err ~= nil then
			return nil, "prepare returned error: " .. type(err) .. " = " .. tostring(err)
		end
		if stmt == nil then
			return nil, "stmt is nil"
		end
		if type(stmt) ~= "userdata" then
			return nil, "expected userdata, got " .. type(stmt)
		end
		return "success"
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(sqlLikeYieldModuleBinder),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatalf("First Step failed: %v", err)
	}

	if output.Count() != 1 {
		t.Fatalf("Expected 1 yield, got %d", output.Count())
	}

	yields := output.Yields()
	events := []process.Event{
		{
			Type: process.EventYieldComplete,
			Tag:  yields[0].Tag,
			Data: mockYieldPrepareResponse{
				Resource: &mockYieldResource{name: "test_stmt"},
				Error:    nil,
			},
			Error: nil,
		},
	}

	output.Reset()
	stepErr := proc.Step(events, &output)

	if output.Status() != process.StepDone {
		t.Fatalf("Expected StepDone, got %v", output.Status())
	}

	if stepErr != nil {
		t.Fatalf("Expected no error, got: %v", stepErr)
	}
}

func TestYieldHandlerSQLPatternWithError(t *testing.T) {
	script := `
		local stmt, err = sqlmod.prepare()

		if err == nil then
			return nil, "expected error but got nil"
		end
		if stmt ~= nil then
			return nil, "expected nil stmt, got " .. type(stmt)
		end
		return "success: " .. tostring(err)
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(sqlLikeYieldModuleBinder),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatalf("First Step failed: %v", err)
	}

	yields := output.Yields()
	events := []process.Event{
		{
			Type: process.EventYieldComplete,
			Tag:  yields[0].Tag,
			Data: mockYieldPrepareResponse{
				Resource: nil,
				Error:    errors.New("database connection failed"),
			},
			Error: nil,
		},
	}

	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	if output.Status() != process.StepDone {
		t.Fatalf("Expected StepDone, got %v", output.Status())
	}
}
