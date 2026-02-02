package runner

import (
	"context"
	"net/http"
	"net/http/httptest"
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
	payloadconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/evalhost"
	"github.com/wippyai/runtime/runtime/lua/modules/httpclient"
	"github.com/wippyai/runtime/runtime/lua/modules/json"
	timemod "github.com/wippyai/runtime/runtime/lua/modules/time"
	httpclienthandler "github.com/wippyai/runtime/service/http/client"
	"github.com/wippyai/runtime/system/clock"
	"github.com/wippyai/runtime/system/scheduler"
	"github.com/wippyai/runtime/system/scheduler/actor"
	lua "github.com/wippyai/go-lua"
	"go.uber.org/zap"
)

// testScheduler wraps actor.Scheduler for testing
type testScheduler struct {
	registry dispatcher.Registry
	*actor.Scheduler
	clock      *clock.Dispatcher
	eval       *evalhost.Dispatcher
	httpClient *httpclienthandler.Dispatcher
	pending    map[string]chan *runtime.Result
	mu         sync.Mutex
}

func (ts *testScheduler) Stop() {
	ts.Scheduler.Stop(context.Background())
	if ts.clock != nil {
		_ = ts.clock.Stop(context.Background())
	}
	if ts.httpClient != nil {
		_ = ts.httpClient.Stop(context.Background())
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

	_, err := ts.Submit(ctx, p, proc, method, input)
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

// newTestContext creates a context suitable for testing with security disabled
func newTestContext() context.Context {
	ctx := ctxapi.NewRootContext()
	ctx = security.SetStrictMode(ctx, false)
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	return ctx
}

// Context creates a context with the dispatcher registry for tests that need async operations
func (ts *testScheduler) Context() context.Context {
	ctx := ctxapi.NewRootContext()
	_ = dispatcher.WithRegistry(ctx, ts.registry)
	ctx = security.SetStrictMode(ctx, false)
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	return ctx
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

	// Register HTTP client handlers
	httpSvc := httpclienthandler.NewDispatcher()
	httpSvc.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		reg.Register(id, h)
	})
	ts.httpClient = httpSvc

	// Register eval handlers with http_client module
	modules := []*luaapi.ModuleDef{json.Module, timemod.Module, httpclient.Module, Module}
	host := evalhost.NewHost(zap.NewNop(), func() []*luaapi.ModuleDef { return modules })
	evalSvc := evalhost.NewDispatcher(host)
	evalSvc.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		reg.Register(id, h)
	})
	ts.eval = evalSvc

	reg.Freeze()
	ts.registry = reg

	opts := []actor.Option{
		actor.WithWorkers(4),
		actor.WithLifecycle(ts),
	}
	ts.Scheduler = actor.NewScheduler(reg, opts...)
	return ts
}

func bindAllModules(l *lua.LState) error {
	engine.LoadModuleDef(l, json.Module)
	engine.LoadModuleDef(l, timemod.Module)
	engine.LoadModuleDef(l, httpclient.Module)
	engine.LoadModuleDef(l, Module)
	return nil
}

func newLuaProcess(t *testing.T, script string) *engine.Process {
	t.Helper()
	proto, _ := lua.CompileString(script, "test.lua")
	proc, err := engine.NewProcess(
		engine.WithProto(proto),
		engine.WithModuleBinder(bindAllModules),
	)
	if err != nil {
		t.Fatalf("NewProcess failed: %v", err)
	}
	return proc
}

// TestRunner_CompileYield_HandleResult tests HandleResult for CompileYield
func TestRunner_CompileYield_HandleResult(t *testing.T) {
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

// TestRunner_RunYield_HandleResult tests HandleResult for RunYield
func TestRunner_RunYield_HandleResult(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := AcquireRunYield()
	defer ReleaseRunYield(y)

	// Test with error
	results := y.HandleResult(l, nil, assert.AnError)
	require.Len(t, results, 2)
	assert.Equal(t, lua.LNil, results[0])
	assert.NotEqual(t, lua.LNil, results[1])

	// Test with nil data (no error)
	results = y.HandleResult(l, nil, nil)
	require.Len(t, results, 2)
	assert.Equal(t, lua.LNil, results[0])
	assert.Equal(t, lua.LNil, results[1])

	// Test with various data types
	testCases := []struct {
		input    any
		name     string
		expected lua.LValueType
	}{
		{true, "bool", lua.LTBool},
		{42, "int", lua.LTInteger},
		{int64(100), "int64", lua.LTInteger},
		{3.14, "float64", lua.LTNumber},
		{"hello", "string", lua.LTString},
		{[]byte("world"), "bytes", lua.LTString},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			results := y.HandleResult(l, tc.input, nil)
			require.Len(t, results, 2)
			assert.Equal(t, tc.expected, results[0].Type())
			assert.Equal(t, lua.LNil, results[1])
		})
	}
}

// TestRunner_GoToLua tests the payloadconv.GoToLua conversion function
func TestRunner_GoToLua(t *testing.T) {
	testCases := []struct {
		input    any
		name     string
		expected lua.LValueType
	}{
		{nil, "nil", lua.LTNil},
		{true, "bool_true", lua.LTBool},
		{false, "bool_false", lua.LTBool},
		{42, "int", lua.LTInteger},
		{int64(100), "int64", lua.LTInteger},
		{3.14, "float64", lua.LTNumber},
		{"hello", "string", lua.LTString},
		{[]byte("world"), "bytes", lua.LTString},
		{assert.AnError, "error", lua.LTUserData}, // errors become LuaError userdata
		{[]any{1, 2, 3}, "slice", lua.LTTable},
		{map[string]any{"key": "value"}, "map", lua.LTTable},
		{struct{ Name string }{"test"}, "struct", lua.LTTable}, // structs become tables
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := payloadconv.GoToLua(tc.input)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, result.Type())
		})
	}

	// Test nested structures
	t.Run("nested_slice", func(t *testing.T) {
		input := []any{1, []any{2, 3}, "four"}
		result, err := payloadconv.GoToLua(input)
		require.NoError(t, err)
		assert.Equal(t, lua.LTTable, result.Type())
		tbl := result.(*lua.LTable)
		assert.Equal(t, lua.LTInteger, tbl.RawGetInt(1).Type())
		assert.Equal(t, lua.LTTable, tbl.RawGetInt(2).Type())
		assert.Equal(t, lua.LTString, tbl.RawGetInt(3).Type())
	})

	t.Run("nested_map", func(t *testing.T) {
		input := map[string]any{
			"num":   42,
			"inner": map[string]any{"a": 1},
		}
		result, err := payloadconv.GoToLua(input)
		require.NoError(t, err)
		assert.Equal(t, lua.LTTable, result.Type())
		tbl := result.(*lua.LTable)
		assert.Equal(t, lua.LTInteger, tbl.RawGetString("num").Type())
		assert.Equal(t, lua.LTTable, tbl.RawGetString("inner").Type())
	})

	// Test LValue passthrough
	t.Run("lvalue_passthrough", func(t *testing.T) {
		input := lua.LString("already lua")
		result, err := payloadconv.GoToLua(input)
		require.NoError(t, err)
		assert.Equal(t, input, result)
	})
}

// TestRunner_YieldRelease tests that Release methods work correctly
func TestRunner_YieldRelease(t *testing.T) {
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

	// RunYield
	ry := AcquireRunYield()
	ry.Source = "test"
	ry.Method = "handle"
	ry.Args = payload.Payloads{payload.NewPayload(1, payload.JSON), payload.NewPayload(2, payload.JSON)}
	ry.Modules = []string{"json"}
	ry.Context = map[string]any{"k": "v"}
	ry.Release()

	ry2 := AcquireRunYield()
	assert.Empty(t, ry2.Source)
	assert.Empty(t, ry2.Method)
	assert.Nil(t, ry2.Args)
	assert.Nil(t, ry2.Modules)
	assert.Nil(t, ry2.Context)
	ReleaseRunYield(ry2)
}

// TestRunner_YieldStringAndType tests String() and Type() methods
func TestRunner_YieldStringAndType(t *testing.T) {
	cy := AcquireCompileYield()
	defer ReleaseCompileYield(cy)
	assert.Equal(t, "<runner_compile_yield>", cy.String())
	assert.Equal(t, lua.LTUserData, cy.Type())

	ry := AcquireRunYield()
	defer ReleaseRunYield(ry)
	assert.Equal(t, "<runner_run_yield>", ry.String())
	assert.Equal(t, lua.LTUserData, ry.Type())
}

// TestRunner_Integration_Compile tests the full compile flow via scheduler
func TestRunner_Integration_Compile(t *testing.T) {
	sched := newTestScheduler()
	sched.Start()
	defer sched.Stop()

	script := `
		local runner = require("eval_runner")

		local program, err = runner.compile([[
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

	ctx := newTestContext()
	proc := newLuaProcess(t, script)

	result, err := sched.Execute(ctx, uniqueTestPID(), proc, "", nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should return "handle"
	if result.Value != nil {
		t.Logf("Result: %v", result.Value.Data())
	}
}

// TestRunner_Integration_CompileSyntaxError tests compile with syntax error
func TestRunner_Integration_CompileSyntaxError(t *testing.T) {
	sched := newTestScheduler()
	sched.Start()
	defer sched.Stop()

	script := `
		local runner = require("eval_runner")

		local program, err = runner.compile([[
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

	ctx := newTestContext()
	proc := newLuaProcess(t, script)

	result, err := sched.Execute(ctx, uniqueTestPID(), proc, "", nil)
	require.NoError(t, err)
	require.NotNil(t, result)
}

// TestRunner_Integration_Run tests the full run flow
func TestRunner_Integration_Run(t *testing.T) {
	sched := newTestScheduler()
	sched.Start()
	defer sched.Stop()

	script := `
		local runner = require("eval_runner")
		local json = require("json")

		local result, err = runner.run({
			source = [[
				local function handle(x)
					return x * 2
				end
				return { handle = handle }
			]],
			method = "handle",
			args = { 21 },
			modules = { "json" }
		})

		if err then
			error(tostring(err))
		end

		return result
	`

	ctx := newTestContext()
	proc := newLuaProcess(t, script)

	result, err := sched.Execute(ctx, uniqueTestPID(), proc, "", nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	if result.Value != nil {
		t.Logf("Result: %v", result.Value.Data())
	}
}

// TestRunner_ProgramMethods tests Program userdata methods
func TestRunner_ProgramMethods(t *testing.T) {
	sched := newTestScheduler()
	sched.Start()
	defer sched.Stop()

	script := `
		local runner = require("eval_runner")

		local program, err = runner.compile([[
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

	ctx := newTestContext()
	proc := newLuaProcess(t, script)

	result, err := sched.Execute(ctx, uniqueTestPID(), proc, "", nil)
	require.NoError(t, err)
	require.NotNil(t, result)
}

// TestRunner_PoolConcurrency tests pool under concurrent access
func TestRunner_PoolConcurrency(_ *testing.T) {
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

	// Test RunYield pool
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				y := AcquireRunYield()
				y.Source = "test"
				y.Args = payload.Payloads{
					payload.NewPayload(1, payload.JSON),
					payload.NewPayload(2, payload.JSON),
					payload.NewPayload(3, payload.JSON),
				}
				ReleaseRunYield(y)
			}
		}()
	}

	wg.Wait()
}

// TestRunner_AllowYields_HTTPRequest tests that HTTP requests work in eval (yields auto-detected from modules)
func TestRunner_AllowYields_HTTPRequest(t *testing.T) {
	// Create a test HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","method":"` + r.Method + `"}`))
	}))
	defer server.Close()

	sched := newTestScheduler()
	sched.Start()
	defer sched.Stop()

	script := `
		local runner = require("eval_runner")

		local result, err = runner.run({
			source = [[
				local http = require("http_client")
				local json = require("json")

				local function run(url)
					local resp, err = http.get(url)
					if err then
						return nil, err
					end
					return json.decode(resp.body)
				end
				return { run = run }
			]],
			method = "run",
			args = { "` + server.URL + `" },
			modules = { "http_client", "json" },
			allow_classes = { "network", "io" }
		})

		if err then
			error("eval failed: " .. tostring(err))
		end

		return result
	`

	ctx := sched.Context()
	proc := newLuaProcess(t, script)

	result, err := sched.Execute(ctx, uniqueTestPID(), proc, "", nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	require.NotNil(t, result.Value, "expected result value")
	luaData := result.Value.Data()
	goData := value.ToGoAny(luaData.(lua.LValue))
	t.Logf("Result: %v", goData)
	resultMap, ok := goData.(map[string]any)
	require.True(t, ok, "expected map result, got %T", goData)
	assert.Equal(t, "ok", resultMap["status"])
	assert.Equal(t, "GET", resultMap["method"])
}

// TestRunner_AllowYields_Blocked tests that yields are blocked when not allowed
func TestRunner_AllowYields_Blocked(t *testing.T) {
	sched := newTestScheduler()
	sched.Start()
	defer sched.Stop()

	script := `
		local runner = require("eval_runner")

		-- Try to run eval with http_client but NO allow_yields
		local result, err = runner.run({
			source = [[
				local http = require("http_client")
				local resp, err = http.get("http://example.com")
				return resp
			]],
			method = "",
			modules = { "http_client" },
			allow_classes = { "network", "io" }
			-- Note: no allow_yields specified
		})

		if err == nil then
			error("expected error but got none")
		end

		-- Error should mention yield not allowed
		local errStr = tostring(err)
		return { got_error = true, error_msg = errStr }
	`

	ctx := newTestContext()
	proc := newLuaProcess(t, script)

	result, err := sched.Execute(ctx, uniqueTestPID(), proc, "", nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.NotNil(t, result.Value, "expected result value")
	luaData := result.Value.Data()
	goData := value.ToGoAny(luaData.(lua.LValue))
	t.Logf("Result: %v", goData)
	resultMap, ok := goData.(map[string]any)
	require.True(t, ok, "expected map result, got %T", goData)
	assert.Equal(t, true, resultMap["got_error"])
}

// TestRunner_AllowYields_EmptyList tests that empty allow_yields blocks all yields
func TestRunner_AllowYields_EmptyList(t *testing.T) {
	sched := newTestScheduler()
	sched.Start()
	defer sched.Stop()

	script := `
		local runner = require("eval_runner")

		-- Try to run eval with http_client but empty allow_yields
		local result, err = runner.run({
			source = [[
				local http = require("http_client")
				local resp, err = http.get("http://example.com")
				return resp
			]],
			method = "",
			modules = { "http_client" },
			allow_classes = { "network", "io" },
			allow_yields = {}  -- Empty list = nothing allowed
		})

		if err == nil then
			error("expected error but got none")
		end

		return { got_error = true }
	`

	ctx := newTestContext()
	proc := newLuaProcess(t, script)

	result, err := sched.Execute(ctx, uniqueTestPID(), proc, "", nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.NotNil(t, result.Value, "expected result value")
	luaData := result.Value.Data()
	goData := value.ToGoAny(luaData.(lua.LValue))
	resultMap, ok := goData.(map[string]any)
	require.True(t, ok, "expected map result, got %T", goData)
	assert.Equal(t, true, resultMap["got_error"])
}
