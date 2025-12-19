package sandbox

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/evalhost"
	"github.com/wippyai/runtime/runtime/lua/modules/json"
	timemod "github.com/wippyai/runtime/runtime/lua/modules/time"
	"github.com/wippyai/runtime/system/clock"
	"github.com/wippyai/runtime/system/scheduler"
	"github.com/wippyai/runtime/system/scheduler/actor"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// testScheduler wraps actor.Scheduler for testing
type testScheduler struct {
	*actor.Scheduler
	clock   *clock.Dispatcher
	eval    *evalhost.Dispatcher
	mu      sync.Mutex
	pending map[string]chan *runtime.Result
}

func (ts *testScheduler) Stop() {
	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	ts.Scheduler.Stop(ctx)
	if ts.clock != nil {
		_ = ts.clock.Stop(ctx)
	}
}

func (ts *testScheduler) OnStart(_ context.Context, _ pid.PID, _ process.Process) error { return nil }

func (ts *testScheduler) OnComplete(_ context.Context, p pid.PID, result *runtime.Result) {
	ts.mu.Lock()
	ch, ok := ts.pending[p.UniqID]
	if ok {
		delete(ts.pending, p.UniqID)
	}
	ts.mu.Unlock()
	if ok {
		ch <- result
	}
}

func (ts *testScheduler) Execute(ctx context.Context, p pid.PID, proc process.Process, method string, input payload.Payloads) (*runtime.Result, error) {
	resultCh := make(chan *runtime.Result, 1)

	ts.mu.Lock()
	ts.pending[p.UniqID] = resultCh
	ts.mu.Unlock()

	_, err := ts.Scheduler.Submit(ctx, p, proc, method, input)
	if err != nil {
		ts.mu.Lock()
		delete(ts.pending, p.UniqID)
		ts.mu.Unlock()
		return nil, err
	}

	select {
	case result := <-resultCh:
		return result, nil
	case <-ctx.Done():
		ts.mu.Lock()
		delete(ts.pending, p.UniqID)
		ts.mu.Unlock()
		return nil, ctx.Err()
	}
}

var testPIDCounter atomic.Int64

func uniqueTestPID() pid.PID {
	return pid.PID{UniqID: time.Now().Format("20060102150405.000000000") + "-" + string(rune(testPIDCounter.Add(1)))}
}

func newTestScheduler() *testScheduler {
	ts := &testScheduler{
		pending: make(map[string]chan *runtime.Result),
	}

	reg := scheduler.NewRegistry()

	// Register clock handlers
	clockSvc := clock.NewDispatcher()
	clockSvc.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		reg.Register(id, h)
	})
	ts.clock = clockSvc

	// Register eval handlers
	modules := []luaapi.Module{json.Module, timemod.Module, Module}
	host := evalhost.NewHost(zap.NewNop(), modules, nil)
	evalSvc := evalhost.NewDispatcher(host)
	evalSvc.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		reg.Register(id, h)
	})
	ts.eval = evalSvc

	opts := []actor.Option{
		actor.WithWorkers(4),
		actor.WithLifecycle(ts),
	}
	ts.Scheduler = actor.NewScheduler(reg, opts...)
	return ts
}

func bindAllModules(l *lua.LState) {
	luaapi.LoadModule(l, json.Module)
	luaapi.LoadModule(l, timemod.Module)
	luaapi.LoadModule(l, Module)
}

func newLuaProcess(script string) *engine.Process {
	proto, _ := lua.CompileString(script, "test.lua")
	return engine.NewProcess(
		engine.WithProto(proto),
		engine.WithModuleBinder(bindAllModules),
	)
}

// TestSandbox_CompileYield_HandleResult tests HandleResult for CompileYield
func TestSandbox_CompileYield_HandleResult(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := AcquireCompileYield()
	defer ReleaseCompileYield(y)

	// Test with error
	results := y.HandleResult(l, nil, assert.AnError)
	require.Len(t, results, 2)
	assert.Equal(t, lua.LNil, results[0])
	assert.NotEqual(t, lua.LNil, results[1])

	// Test with nil data
	results = y.HandleResult(l, nil, nil)
	require.Len(t, results, 2)
	assert.Equal(t, lua.LNil, results[0])
	assert.NotEqual(t, lua.LNil, results[1])

	// Test with wrong type
	results = y.HandleResult(l, "wrong type", nil)
	require.Len(t, results, 2)
	assert.Equal(t, lua.LNil, results[0])
	assert.NotEqual(t, lua.LNil, results[1])
}

// TestSandbox_CreateProcessYield_HandleResult tests HandleResult for CreateProcessYield
func TestSandbox_CreateProcessYield_HandleResult(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := AcquireCreateProcessYield()
	defer ReleaseCreateProcessYield(y)

	// Test with error
	results := y.HandleResult(l, nil, assert.AnError)
	require.Len(t, results, 2)
	assert.Equal(t, lua.LNil, results[0])
	assert.NotEqual(t, lua.LNil, results[1])

	// Test with nil data
	results = y.HandleResult(l, nil, nil)
	require.Len(t, results, 2)
	assert.Equal(t, lua.LNil, results[0])
	assert.NotEqual(t, lua.LNil, results[1])

	// Test with wrong type
	results = y.HandleResult(l, "wrong type", nil)
	require.Len(t, results, 2)
	assert.Equal(t, lua.LNil, results[0])
	assert.NotEqual(t, lua.LNil, results[1])
}

// TestSandbox_AnyToLua tests anyToLua conversion function
func TestSandbox_AnyToLua(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	testCases := []struct {
		name     string
		input    any
		expected lua.LValueType
	}{
		{"nil", nil, lua.LTNil},
		{"bool_true", true, lua.LTBool},
		{"bool_false", false, lua.LTBool},
		{"int", 42, lua.LTNumber},
		{"int64", int64(100), lua.LTNumber},
		{"float64", 3.14, lua.LTNumber},
		{"string", "hello", lua.LTString},
		{"bytes", []byte("world"), lua.LTString},
		{"error", assert.AnError, lua.LTString},
		{"unknown", struct{}{}, lua.LTString},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := anyToLua(l, tc.input)
			assert.Equal(t, tc.expected, result.Type())
		})
	}

	// Test LValue passthrough
	t.Run("lvalue_passthrough", func(t *testing.T) {
		input := lua.LString("already lua")
		result := anyToLua(l, input)
		assert.Equal(t, input, result)
	})
}

// TestSandbox_YieldRelease tests that Release methods work correctly
func TestSandbox_YieldRelease(t *testing.T) {
	// CompileYield
	cy := AcquireCompileYield()
	cy.Source = "test"
	cy.Method = "handle"
	cy.Modules = []string{"json"}
	cy.Release()

	cy2 := AcquireCompileYield()
	assert.Empty(t, cy2.Source)
	assert.Empty(t, cy2.Method)
	assert.Nil(t, cy2.Modules)
	ReleaseCompileYield(cy2)

	// CreateProcessYield
	cpy := AcquireCreateProcessYield()
	cpy.Clock = NewMockClock(0)
	cpy.Release()

	cpy2 := AcquireCreateProcessYield()
	assert.Nil(t, cpy2.Program)
	assert.Nil(t, cpy2.Clock)
	assert.Nil(t, cpy2.Ctx)
	ReleaseCreateProcessYield(cpy2)
}

// TestSandbox_YieldStringAndType tests String() and Type() methods
func TestSandbox_YieldStringAndType(t *testing.T) {
	cy := AcquireCompileYield()
	defer ReleaseCompileYield(cy)
	assert.Equal(t, "<sandbox_compile_yield>", cy.String())
	assert.Equal(t, lua.LTUserData, cy.Type())

	cpy := AcquireCreateProcessYield()
	defer ReleaseCreateProcessYield(cpy)
	assert.Equal(t, "<sandbox_create_process_yield>", cpy.String())
	assert.Equal(t, lua.LTUserData, cpy.Type())
}

// TestSandbox_Process tests the Process wrapper directly
func TestSandbox_Process(t *testing.T) {
	clk := NewMockClock(0)

	// Create a mock process for testing
	mockProc := &mockProcess{}
	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)

	sp := NewProcess(ctx, mockProc, clk)
	require.NotNil(t, sp)

	// Test Clock
	assert.Equal(t, clk, sp.Clock())

	// Test IsClosed initial state
	assert.False(t, sp.IsClosed())

	// Test Close
	sp.Close()
	assert.True(t, sp.IsClosed())

	// Test double close is safe
	sp.Close()
	assert.True(t, sp.IsClosed())

	// Test operations on closed process
	err := sp.Init("test", nil)
	assert.Equal(t, ErrProcessClosed, err)

	_, err = sp.Step(nil)
	assert.Equal(t, ErrProcessClosed, err)
}

// mockProcess is a simple mock for process.Process
type mockProcess struct {
	initCalled  bool
	stepCalled  bool
	closeCalled bool
}

func (m *mockProcess) Init(_ context.Context, _ string, _ payload.Payloads) error {
	m.initCalled = true
	return nil
}

func (m *mockProcess) Step(_ []process.Event, out *process.StepOutput) error {
	m.stepCalled = true
	out.Done(nil)
	return nil
}

func (m *mockProcess) Close() {
	m.closeCalled = true
}

// TestSandbox_Process_WithMockProcess tests Process with mock process
func TestSandbox_Process_WithMockProcess(t *testing.T) {
	clk := NewMockClock(time.Now().UnixNano())
	mockProc := &mockProcess{}
	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)

	sp := NewProcess(ctx, mockProc, clk)
	defer sp.Close()

	// Test Init
	err := sp.Init("handle", nil)
	require.NoError(t, err)
	assert.True(t, mockProc.initCalled)

	// Test Step
	result, err := sp.Step(nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, mockProc.stepCalled)
	assert.Equal(t, process.StepDone, result.Status)
}

// TestSandbox_Integration_Compile tests the full compile flow via scheduler
func TestSandbox_Integration_Compile(t *testing.T) {
	sched := newTestScheduler()
	sched.Scheduler.Start()
	defer sched.Stop()

	script := `
		local sandbox = require("eval_sandbox")

		local program, err = sandbox.compile([[
			local function handle(x)
				return x * 2
			end
			return { handle = handle }
		]], "handle", { modules = { "json" } })

		if err then
			error(tostring(err))
		end

		return program:method()
	`

	rootCtx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	ctx, _ := ctxapi.OpenFrameContext(rootCtx)
	proc := newLuaProcess(script)

	result, err := sched.Execute(ctx, uniqueTestPID(), proc, "", nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should return "handle"
	if result.Value != nil {
		t.Logf("Result: %v", result.Value.Data())
	}
}

// TestSandbox_Integration_CompileSyntaxError tests compile with syntax error
func TestSandbox_Integration_CompileSyntaxError(t *testing.T) {
	sched := newTestScheduler()
	sched.Scheduler.Start()
	defer sched.Stop()

	script := `
		local sandbox = require("eval_sandbox")

		local program, err = sandbox.compile([[
			this is not valid lua!!!
		]], "handle", { modules = { "json" } })

		if program ~= nil then
			error("expected nil program")
		end

		if err == nil then
			error("expected error")
		end

		return "got_error"
	`

	rootCtx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	ctx, _ := ctxapi.OpenFrameContext(rootCtx)
	proc := newLuaProcess(script)

	result, err := sched.Execute(ctx, uniqueTestPID(), proc, "", nil)
	require.NoError(t, err)
	require.NotNil(t, result)
}

// TestSandbox_Integration_Clock tests clock operations via scheduler
func TestSandbox_Integration_Clock(t *testing.T) {
	sched := newTestScheduler()
	sched.Scheduler.Start()
	defer sched.Stop()

	script := `
		local sandbox = require("eval_sandbox")
		local time = require("time")

		local clock = sandbox.clock(1704067200000000000)

		if clock:now() ~= 1704067200000000000 then
			error("unexpected initial time: " .. clock:now())
		end

		clock:advance(100 * time.MILLISECOND)
		local expected = 1704067200000000000 + 100 * time.MILLISECOND
		if clock:now() ~= expected then
			error("unexpected time after advance: " .. clock:now())
		end

		return true
	`

	rootCtx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	ctx, _ := ctxapi.OpenFrameContext(rootCtx)
	proc := newLuaProcess(script)

	result, err := sched.Execute(ctx, uniqueTestPID(), proc, "", nil)
	require.NoError(t, err)
	require.NotNil(t, result)
}

// TestSandbox_ProgramMethods tests Program userdata methods
func TestSandbox_ProgramMethods(t *testing.T) {
	sched := newTestScheduler()
	sched.Scheduler.Start()
	defer sched.Stop()

	script := `
		local sandbox = require("eval_sandbox")

		local program, err = sandbox.compile([[
			return { handle = function() end }
		]], "handle", { modules = { "json", "time" } })

		if err then
			error(tostring(err))
		end

		local method = program:method()
		local modules = program:modules()

		if method ~= "handle" then
			error("wrong method: " .. tostring(method))
		end

		if #modules ~= 2 then
			error("wrong module count: " .. #modules)
		end

		return true
	`

	rootCtx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	ctx, _ := ctxapi.OpenFrameContext(rootCtx)
	proc := newLuaProcess(script)

	result, err := sched.Execute(ctx, uniqueTestPID(), proc, "", nil)
	require.NoError(t, err)
	require.NotNil(t, result)
}

// TestSandbox_PoolConcurrency tests pool under concurrent access
func TestSandbox_PoolConcurrency(_ *testing.T) {
	const goroutines = 100
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Test CompileYield pool
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				y := AcquireCompileYield()
				y.Source = "test"
				y.Method = "handle"
				y.Modules = []string{"json"}
				ReleaseCompileYield(y)
			}
		}()
	}

	// Test CreateProcessYield pool
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				y := AcquireCreateProcessYield()
				y.Clock = NewMockClock(0)
				ReleaseCreateProcessYield(y)
			}
		}()
	}

	wg.Wait()
}

// TestSandbox_TranscodeToLua tests transcodeToLua function
func TestSandbox_TranscodeToLua(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	// Test nil payload
	result := transcodeToLua(l, nil)
	assert.Equal(t, lua.LNil, result)

	// Test Lua payload passthrough
	luaPayload := payload.NewPayload(lua.LString("test"), payload.Lua)
	result = transcodeToLua(l, luaPayload)
	assert.Equal(t, lua.LTString, result.Type())
	assert.Equal(t, "test", string(result.(lua.LString)))
}

// TestSandbox_StepResult tests StepResult structure
func TestSandbox_StepResult(t *testing.T) {
	result := &StepResult{
		Status:     process.StepContinue,
		YieldCount: 2,
		Yields: []YieldInfo{
			{CmdID: 1, Tag: 1},
			{CmdID: 2, Tag: 2},
		},
	}

	assert.Equal(t, process.StepContinue, result.Status)
	assert.Equal(t, 2, result.YieldCount)
	assert.Len(t, result.Yields, 2)
}

// TestSandbox_ClockConcurrency tests MockClock under concurrent access
func TestSandbox_ClockConcurrency(_ *testing.T) {
	clk := NewMockClock(time.Now().UnixNano())

	var wg sync.WaitGroup
	wg.Add(100)

	for i := 0; i < 100; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				clk.Now()
				clk.NowNano()
				clk.Elapsed()
				clk.Advance(time.Millisecond)
			}
		}()
	}

	wg.Wait()
}

// TestSandbox_Integration_StepWithSleep tests stepping a process with time.sleep
func TestSandbox_Integration_StepWithSleep(t *testing.T) {
	sched := newTestScheduler()
	sched.Scheduler.Start()
	defer sched.Stop()

	script := `
		local sandbox = require("eval_sandbox")
		local time = require("time")

		-- Compile a program that sleeps
		local program, err = sandbox.compile([[
			local time = require("time")
			local function handle()
				time.sleep(100 * time.MILLISECOND)
				return "done"
			end
			return { handle = handle }
		]], "handle", { modules = { "time" } })

		if err then
			error("compile: " .. tostring(err))
		end

		-- Create process with mock clock
		local clock = sandbox.clock(1704067200000000000)
		local proc, proc_err = sandbox.create_process(program, clock)

		if proc_err then
			error("create_process: " .. tostring(proc_err))
		end

		-- Init the process
		local ok, init_err = proc:init("handle")
		if init_err then
			error("init: " .. tostring(init_err))
		end

		-- First step should yield for sleep
		local result = proc:step()
		if result.status ~= "waiting" then
			error("expected waiting, got: " .. tostring(result.status))
		end
		if result.yield_count == 0 then
			error("expected yields")
		end

		-- Get yield info
		local yield = result.yields[1]
		if not yield.tag then
			error("no tag in yield")
		end

		-- Advance clock
		clock:advance(100 * time.MILLISECOND)

		-- Complete the yield
		local result2 = proc:step({
			{ tag = yield.tag, data = clock:now() }
		})

		-- Keep stepping until done
		local steps = 0
		while result2.status == "continue" and steps < 10 do
			result2 = proc:step()
			steps = steps + 1
		end

		if result2.status ~= "done" then
			error("expected done, got: " .. result2.status .. " after " .. steps .. " extra steps")
		end

		proc:close()
		return true
	`

	rootCtx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	ctx, _ := ctxapi.OpenFrameContext(rootCtx)
	proc := newLuaProcess(script)

	result, err := sched.Execute(ctx, uniqueTestPID(), proc, "", nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.Error, "unexpected error: %v", result.Error)
}
