package engine

import (
	"context"
	"errors"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/process"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
)

// mockYield implements luaapi.HandledYield for testing the yield flow.
type mockYield struct {
	cmdID    dispatcher.CommandID
	response any
	err      error
}

func (y *mockYield) String() string       { return "<mock_yield>" }
func (y *mockYield) Type() lua.LValueType { return lua.LTUserData }

func (y *mockYield) CmdID() dispatcher.CommandID   { return y.cmdID }
func (y *mockYield) ToCommand() dispatcher.Command { return y }
func (y *mockYield) Release()                      {}
func (y *mockYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
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
	// Handle userdata responses (like SQL Statement/Transaction)
	if ud, ok := data.(*lua.LUserData); ok {
		return []lua.LValue{ud, lua.LNil}
	}
	return []lua.LValue{lua.LNil, lua.LNil}
}

// mockResource simulates a resource like sql.Statement or sql.Transaction
type mockResource struct {
	name string
}

func (r *mockResource) Name() string { return r.name }

// mockPrepareResponse simulates sqlapi.PrepareResponse
type mockPrepareResponse struct {
	Resource *mockResource
	Error    error
}

// sqlLikeYield simulates the exact pattern SQL uses with HandleResult
type sqlLikeYield struct {
	cmdID      dispatcher.CommandID
	wrapResult func(*mockResource) lua.LValue
}

func (y *sqlLikeYield) String() string                { return "<sql_like_yield>" }
func (y *sqlLikeYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *sqlLikeYield) CmdID() dispatcher.CommandID   { return y.cmdID }
func (y *sqlLikeYield) ToCommand() dispatcher.Command { return y }
func (y *sqlLikeYield) Release()                      {}

func (y *sqlLikeYield) HandleResult(_ *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}
	resp, ok := data.(mockPrepareResponse)
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

// sqlLikeYieldModule binds a function that yields exactly like SQL does.
func sqlLikeYieldModule(l *lua.LState) {
	mod := l.CreateTable(0, 1)
	mod.RawSetString("prepare", lua.LGoFunc(func(l *lua.LState) int {
		yield := &sqlLikeYield{
			cmdID: testCmdID,
			wrapResult: func(r *mockResource) lua.LValue {
				ud := l.NewUserData()
				ud.Value = r
				return ud
			},
		}
		l.Push(yield)
		return -1
	}))
	l.SetGlobal("sqlmod", mod)
}

const testCmdID dispatcher.CommandID = 9999

// mockYieldModule binds a test function that yields mockYield.
func mockYieldModule(response any, err error) ModuleBinder {
	return func(l *lua.LState) {
		mod := l.CreateTable(0, 1)
		mod.RawSetString("fetch", lua.LGoFunc(func(l *lua.LState) int {
			yield := &mockYield{
				cmdID:    testCmdID,
				response: response,
				err:      err,
			}
			l.Push(yield)
			return -1
		}))
		l.SetGlobal("testmod", mod)
	}
}

// TestYieldHandlerSuccessFlow tests the complete yield -> dispatcher -> HandleResult -> resume flow
// with a successful response.
func TestYieldHandlerSuccessFlow(t *testing.T) {
	script := `
		local result, err = testmod.fetch()
		if err then
			return nil, err
		end
		return result
	`

	proc := NewProcess(
		WithScript(script, "test.lua"),
		WithModuleBinder(mockYieldModule("hello world", nil)),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	// First step should yield
	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatalf("First Step failed: %v", err)
	}

	if output.Count() != 1 {
		t.Fatalf("Expected 1 yield, got %d", output.Count())
	}

	yields := output.Yields()
	if yields[0].Cmd.CmdID() != testCmdID {
		t.Errorf("Expected command ID %d, got %d", testCmdID, yields[0].Cmd.CmdID())
	}

	// Simulate dispatcher completing the yield with success
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

	// Verify the result
	result := output.Result()
	if result == nil {
		t.Fatal("Expected result, got nil")
	}
}

// TestYieldHandlerErrorFlow tests that errors from the dispatcher are properly
// returned to Lua as the second return value.
func TestYieldHandlerErrorFlow(t *testing.T) {
	script := `
		local result, err = testmod.fetch()
		if err then
			return nil, "got error: " .. tostring(err)
		end
		return result
	`

	proc := NewProcess(
		WithScript(script, "test.lua"),
		WithModuleBinder(mockYieldModule(nil, errors.New("connection failed"))),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	// First step should yield
	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatalf("First Step failed: %v", err)
	}

	if output.Count() != 1 {
		t.Fatalf("Expected 1 yield, got %d", output.Count())
	}

	yields := output.Yields()

	// Simulate dispatcher completing the yield with error
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

	// The Lua code returns the error as second value, which should propagate
	if stepErr == nil {
		t.Fatal("Expected error from Lua returning error as second value")
	}

	if !containsString(stepErr.Error(), "got error") {
		t.Errorf("Error should contain 'got error', got: %s", stepErr.Error())
	}
}

// TestYieldHandlerTableResponse tests that complex responses (tables) are
// correctly converted and returned to Lua.
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

	proc := NewProcess(
		WithScript(script, "test.lua"),
		WithModuleBinder(mockYieldModule(map[string]any{"name": "test", "count": 42}, nil)),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	// First step should yield
	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatalf("First Step failed: %v", err)
	}

	yields := output.Yields()

	// Simulate dispatcher completing with table data
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

// TestYieldHandlerMultipleYields tests multiple sequential yields.
func TestYieldHandlerMultipleYields(t *testing.T) {
	script := `
		local a, err1 = testmod.fetch()
		if err1 then return nil, err1 end

		local b, err2 = testmod.fetch()
		if err2 then return nil, err2 end

		return a .. " " .. b
	`

	proc := NewProcess(
		WithScript(script, "test.lua"),
		WithModuleBinder(mockYieldModule("first", nil)),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	// First yield
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

	// Resume after first yield -> triggers second yield
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

	// Resume after second yield -> should complete
	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatalf("Third Step failed: %v", err)
	}

	if output.Status() != process.StepDone {
		t.Fatalf("Expected StepDone, got %v", output.Status())
	}
}

// TestYieldHandlerUserdataResponse tests that userdata responses (like SQL Statement)
// are correctly returned to Lua as the first value with nil error.
// This mirrors how SQL's db:prepare() returns (Statement, nil).
func TestYieldHandlerUserdataResponse(t *testing.T) {
	script := `
		local resource, err = testmod.fetch()
		if err then
			return nil, "got error: " .. tostring(err)
		end
		if resource == nil then
			return nil, "resource is nil"
		end
		-- Check that we can call a method on the userdata
		local name = resource:name()
		if name ~= "test_resource" then
			return nil, "expected name 'test_resource', got: " .. tostring(name)
		end
		return "success"
	`

	proc := NewProcess(
		WithScript(script, "test.lua"),
		WithModuleBinder(mockYieldModule(nil, nil)), // response set later
		WithModuleBinder(func(l *lua.LState) {
			// Register metatable for mockResource
			mt := l.NewTypeMetatable("mockResource")
			l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
				"name": func(l *lua.LState) int {
					ud := l.CheckUserData(1)
					if res, ok := ud.Value.(*mockResource); ok {
						l.Push(lua.LString(res.Name()))
						return 1
					}
					return 0
				},
			}))
		}),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	// First step should yield
	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatalf("First Step failed: %v", err)
	}

	if output.Count() != 1 {
		t.Fatalf("Expected 1 yield, got %d", output.Count())
	}

	// Create userdata response (simulating SQL handler creating Statement)
	ud := proc.State().NewUserData()
	ud.Value = &mockResource{name: "test_resource"}
	ud.Metatable = proc.State().GetTypeMetatable("mockResource")

	yields := output.Yields()
	events := []process.Event{
		{
			Type:  process.EventYieldComplete,
			Tag:   yields[0].Tag,
			Data:  ud, // Return userdata like SQL does
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

// TestYieldHandlerUserdataWithNilError specifically tests the (userdata, nil) tuple
// to ensure the second return value is actually nil and not the userdata.
func TestYieldHandlerUserdataWithNilError(t *testing.T) {
	script := `
		local resource, err = testmod.fetch()

		-- Explicitly check that err is nil, not the resource
		if err ~= nil then
			return nil, "err should be nil but got: " .. type(err) .. " = " .. tostring(err)
		end

		-- Check resource is not nil
		if resource == nil then
			return nil, "resource should not be nil"
		end

		-- Check resource is userdata
		if type(resource) ~= "userdata" then
			return nil, "resource should be userdata, got: " .. type(resource)
		end

		return "success"
	`

	proc := NewProcess(
		WithScript(script, "test.lua"),
		WithModuleBinder(mockYieldModule(nil, nil)),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	// First step should yield
	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatalf("First Step failed: %v", err)
	}

	// Create userdata response
	ud := proc.State().NewUserData()
	ud.Value = &mockResource{name: "test"}

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

// TestYieldHandlerSQLPattern tests the exact SQL pattern:
// 1. Function yields with yield object on stack
// 2. Dispatcher returns struct response (like PrepareResponse)
// 3. HandleResult wraps the resource and returns [userdata, nil]
// This should result in Lua receiving (stmt, nil)
func TestYieldHandlerSQLPattern(t *testing.T) {
	script := `
		local stmt, err = sqlmod.prepare()

		-- Debug: print what we got
		-- print("stmt type:", type(stmt), "value:", tostring(stmt))
		-- print("err type:", type(err), "value:", tostring(err))

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

	proc := NewProcess(
		WithScript(script, "test.lua"),
		WithModuleBinder(sqlLikeYieldModule),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	// First step should yield
	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatalf("First Step failed: %v", err)
	}

	if output.Count() != 1 {
		t.Fatalf("Expected 1 yield, got %d", output.Count())
	}

	yields := output.Yields()

	// Simulate dispatcher completing with PrepareResponse-like struct
	events := []process.Event{
		{
			Type: process.EventYieldComplete,
			Tag:  yields[0].Tag,
			Data: mockPrepareResponse{
				Resource: &mockResource{name: "test_stmt"},
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

// TestYieldHandlerSQLPatternWithError tests the error case:
// Dispatcher returns struct with Error field set
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

	proc := NewProcess(
		WithScript(script, "test.lua"),
		WithModuleBinder(sqlLikeYieldModule),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	// First step should yield
	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatalf("First Step failed: %v", err)
	}

	yields := output.Yields()

	// Simulate dispatcher completing with error in Response
	events := []process.Event{
		{
			Type: process.EventYieldComplete,
			Tag:  yields[0].Tag,
			Data: mockPrepareResponse{
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

// TestYieldHandlerReturnOrderDebug tests to confirm exactly what Lua receives
// from the tuple (value, nil).
func TestYieldHandlerReturnOrderDebug(t *testing.T) {
	script := `
		local a, b = testmod.fetch()
		-- Return detailed info about what we received
		return {
			a_type = type(a),
			a_str = tostring(a),
			a_is_nil = (a == nil),
			b_type = type(b),
			b_str = tostring(b),
			b_is_nil = (b == nil),
		}
	`

	proc := NewProcess(
		WithScript(script, "test.lua"),
		WithModuleBinder(mockYieldModule(nil, nil)),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	// First step should yield
	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatalf("First Step failed: %v", err)
	}

	// Create userdata response
	ud := proc.State().NewUserData()
	ud.Value = &mockResource{name: "test_resource"}

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

	// Get the result table
	result := output.Result()
	if result == nil {
		t.Fatal("Expected result, got nil")
	}

	// Log what we got
	if lv, ok := result.(lua.LValue); ok {
		if tbl, ok := lv.(*lua.LTable); ok {
			t.Logf("a_type: %v", tbl.RawGetString("a_type"))
			t.Logf("a_str: %v", tbl.RawGetString("a_str"))
			t.Logf("a_is_nil: %v", tbl.RawGetString("a_is_nil"))
			t.Logf("b_type: %v", tbl.RawGetString("b_type"))
			t.Logf("b_str: %v", tbl.RawGetString("b_str"))
			t.Logf("b_is_nil: %v", tbl.RawGetString("b_is_nil"))

			// Check that 'a' is the userdata and 'b' is nil
			aType := tbl.RawGetString("a_type").String()
			bIsNil := tbl.RawGetString("b_is_nil")

			if aType != "userdata" {
				t.Errorf("Expected a to be userdata, got %s", aType)
			}
			if bIsNil != lua.LTrue {
				t.Errorf("Expected b to be nil, got b_is_nil=%v", bIsNil)
			}
		}
	}
}

// Ensure mockYield implements the interface
var _ luaapi.HandledYield = (*mockYield)(nil)
var _ luaapi.YieldConverter = (*mockYield)(nil)
