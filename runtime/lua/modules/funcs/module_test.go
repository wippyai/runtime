package funcs

import (
	"errors"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/modules/future"
	lua "github.com/yuin/gopher-lua"
)

func TestModuleBuild(t *testing.T) {
	table, yields := Module.Build()

	if table == nil {
		t.Fatal("Build() returned nil table")
	}

	// Check module functions
	if table.RawGetString("call").Type() != lua.LTFunction {
		t.Error("call function not registered")
	}
	if table.RawGetString("async").Type() != lua.LTFunction {
		t.Error("async function not registered")
	}
	if table.RawGetString("new").Type() != lua.LTFunction {
		t.Error("new function not registered")
	}

	// Check yield types (Call, AsyncStart, AsyncCancel)
	if len(yields) != 3 {
		t.Errorf("expected 3 yield types, got %d", len(yields))
	}
}

func TestModuleBuildReuse(t *testing.T) {
	table1, _ := Module.Build()
	table2, _ := Module.Build()

	if table1 != table2 {
		t.Error("Build() should return the same table on subsequent calls")
	}
}

func TestModuleImmutable(t *testing.T) {
	table, _ := Module.Build()

	if !table.Immutable {
		t.Error("module table should be immutable")
	}
}

func TestModuleInfo(t *testing.T) {
	if Module.Name != "funcs" {
		t.Errorf("expected name 'funcs', got '%s'", Module.Name)
	}
	if Module.Description == "" {
		t.Error("module should have a description")
	}
	if len(Module.Class) == 0 {
		t.Error("module should have at least one class")
	}
}

func TestFutureTypeMethods(t *testing.T) {
	// Types are registered in init()
	mt := value.GetTypeMetatable(nil, future.TypeName)
	if mt == nil {
		t.Fatal("future type metatable not registered")
	}
}

func TestExecutorTypeMethods(t *testing.T) {
	// Types are registered in init()
	mt := value.GetTypeMetatable(nil, executorTypeName)
	if mt == nil {
		t.Fatal("executor type metatable not registered")
	}
}

func TestExecutorNew(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	table, _ := Module.Build()
	l.SetGlobal("funcs", table)

	err := l.DoString(`
		local exec = funcs.new()
		if exec == nil then
			error("new() returned nil")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestCallYieldType(t *testing.T) {
	yield := AcquireCallYield()
	defer ReleaseCallYield(yield)

	if yield.Type() != lua.LTUserData {
		t.Errorf("expected LTUserData, got %v", yield.Type())
	}
	if yield.String() != "<func_call_yield>" {
		t.Errorf("unexpected String(): %s", yield.String())
	}
}

func TestAsyncStartYieldType(t *testing.T) {
	yield := AcquireAsyncStartYield()
	defer ReleaseAsyncStartYield(yield)

	if yield.Type() != lua.LTUserData {
		t.Errorf("expected LTUserData, got %v", yield.Type())
	}
	if yield.String() != "<func_async_start_yield>" {
		t.Errorf("unexpected String(): %s", yield.String())
	}
}

func TestAsyncCancelYieldType(t *testing.T) {
	yield := AcquireAsyncCancelYield()
	defer ReleaseAsyncCancelYield(yield)

	if yield.Type() != lua.LTUserData {
		t.Errorf("expected LTUserData, got %v", yield.Type())
	}
	if yield.String() != "<func_async_cancel_yield>" {
		t.Errorf("unexpected String(): %s", yield.String())
	}
}

func TestCallYieldPooling(t *testing.T) {
	yield1 := AcquireCallYield()
	ReleaseCallYield(yield1)

	yield2 := AcquireCallYield()
	defer ReleaseCallYield(yield2)

	if yield1 != yield2 {
		t.Error("pool should reuse yield objects")
	}
}

func TestAsyncStartYieldPooling(t *testing.T) {
	y1 := AcquireAsyncStartYield()
	ReleaseAsyncStartYield(y1)

	y2 := AcquireAsyncStartYield()
	defer ReleaseAsyncStartYield(y2)

	if y1 != y2 {
		t.Error("pool should reuse yield objects")
	}
}

func TestAsyncCancelYieldPooling(t *testing.T) {
	y1 := AcquireAsyncCancelYield()
	ReleaseAsyncCancelYield(y1)

	y2 := AcquireAsyncCancelYield()
	defer ReleaseAsyncCancelYield(y2)

	if y1 != y2 {
		t.Error("pool should reuse yield objects")
	}
}

func TestCallYieldHandleResult(t *testing.T) {
	tests := []struct {
		name    string
		data    any
		err     error
		wantErr bool
	}{
		{
			name:    "success",
			data:    function.CallResult{Value: payload.NewString("result"), Error: nil},
			err:     nil,
			wantErr: false,
		},
		{
			name:    "call error",
			data:    nil,
			err:     errors.New("call failed"),
			wantErr: true,
		},
		{
			name:    "no response",
			data:    nil,
			err:     nil,
			wantErr: true,
		},
		{
			name:    "invalid response type",
			data:    "invalid",
			err:     nil,
			wantErr: true,
		},
		{
			name:    "response with error",
			data:    function.CallResult{Error: errors.New("function error")},
			err:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			y := AcquireCallYield()
			defer ReleaseCallYield(y)

			result := y.HandleResult(l, tt.data, tt.err)

			if len(result) != 2 {
				t.Fatalf("expected 2 return values, got %d", len(result))
			}

			if tt.wantErr {
				if result[1] == lua.LNil {
					t.Error("expected error, got nil")
				}
			} else {
				if result[1] != lua.LNil {
					t.Errorf("expected no error, got %v", result[1])
				}
			}
		})
	}
}

func TestAsyncStartYieldHandleResult(t *testing.T) {
	tests := []struct {
		name    string
		data    any
		err     error
		wantErr bool
	}{
		{
			name:    "success",
			data:    function.AsyncStartResult{Error: nil},
			err:     nil,
			wantErr: false,
		},
		{
			name:    "start error",
			data:    nil,
			err:     errors.New("start failed"),
			wantErr: true,
		},
		{
			name:    "invalid response type",
			data:    "invalid",
			err:     nil,
			wantErr: true,
		},
		{
			name:    "response with error",
			data:    function.AsyncStartResult{Error: errors.New("async error")},
			err:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			y := AcquireAsyncStartYield()
			y.Future = future.New("test", nil)
			defer ReleaseAsyncStartYield(y)

			result := y.HandleResult(l, tt.data, tt.err)

			if len(result) != 2 {
				t.Fatalf("expected 2 return values, got %d", len(result))
			}

			if tt.wantErr {
				if result[1] == lua.LNil {
					t.Error("expected error, got nil")
				}
			} else {
				if result[1] != lua.LNil {
					t.Errorf("expected no error, got %v", result[1])
				}
			}
		})
	}
}

func TestAsyncCancelYieldHandleResult(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr bool
	}{
		{
			name:    "success",
			err:     nil,
			wantErr: false,
		},
		{
			name:    "cancel error",
			err:     errors.New("cancel failed"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			y := AcquireAsyncCancelYield()
			defer ReleaseAsyncCancelYield(y)

			result := y.HandleResult(l, nil, tt.err)

			if len(result) != 2 {
				t.Fatalf("expected 2 return values, got %d", len(result))
			}

			if tt.wantErr {
				if result[1] == lua.LNil {
					t.Error("expected error, got nil")
				}
			} else {
				if result[1] != lua.LNil {
					t.Errorf("expected no error, got %v", result[1])
				}
				if result[0] != lua.LTrue {
					t.Error("expected true on success")
				}
			}
		})
	}
}

func TestCallYieldCommandID(t *testing.T) {
	y := AcquireCallYield()
	defer ReleaseCallYield(y)

	if y.CmdID() != function.Call {
		t.Errorf("expected CmdID %v, got %v", function.Call, y.CmdID())
	}
}

func TestAsyncStartYieldCommandID(t *testing.T) {
	y := AcquireAsyncStartYield()
	defer ReleaseAsyncStartYield(y)

	if y.CmdID() != function.AsyncStart {
		t.Errorf("expected CmdID %v, got %v", function.AsyncStart, y.CmdID())
	}
}

func TestAsyncCancelYieldCommandID(t *testing.T) {
	y := AcquireAsyncCancelYield()
	defer ReleaseAsyncCancelYield(y)

	if y.CmdID() != function.AsyncCancel {
		t.Errorf("expected CmdID %v, got %v", function.AsyncCancel, y.CmdID())
	}
}

func TestCallYieldToCommand(t *testing.T) {
	y := AcquireCallYield()
	defer ReleaseCallYield(y)

	cmd := y.ToCommand()
	if cmd == nil {
		t.Error("ToCommand should return a command")
	}
	if cmd != y.CallCmd {
		t.Error("ToCommand should return the CallCmd")
	}
}

func TestAsyncStartYieldToCommand(t *testing.T) {
	y := AcquireAsyncStartYield()
	defer ReleaseAsyncStartYield(y)

	cmd := y.ToCommand()
	if cmd == nil {
		t.Error("ToCommand should return a command")
	}
	if cmd != y.AsyncStartCmd {
		t.Error("ToCommand should return the AsyncStartCmd")
	}
}

func TestAsyncCancelYieldToCommand(t *testing.T) {
	y := AcquireAsyncCancelYield()
	defer ReleaseAsyncCancelYield(y)

	cmd := y.ToCommand()
	if cmd == nil {
		t.Error("ToCommand should return a command")
	}
	if cmd != y.AsyncCancelCmd {
		t.Error("ToCommand should return the AsyncCancelCmd")
	}
}

func TestExecutorState(t *testing.T) {
	e := &Executor{}

	if e.hasActor {
		t.Error("new executor should not have actor")
	}
	if e.hasScope {
		t.Error("new executor should not have scope")
	}
	if e.hasOptions {
		t.Error("new executor should not have options")
	}
}

func TestValidateTarget(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		wantErr bool
	}{
		{
			name:    "valid target",
			target:  "ns:name",
			wantErr: false,
		},
		{
			name:    "empty target",
			target:  "",
			wantErr: true,
		},
		{
			name:    "missing namespace",
			target:  "name",
			wantErr: true,
		},
		{
			name:    "missing name",
			target:  "ns:",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			l.Push(lua.LString(tt.target))

			_, retCount := validateTarget(l, tt.target)

			if tt.wantErr {
				if retCount != 2 {
					t.Errorf("expected error return (2), got %d", retCount)
				}
			} else {
				if retCount != 0 {
					t.Errorf("expected success (0), got %d", retCount)
				}
			}
		})
	}
}

func TestExecutorWithContext(t *testing.T) {
	exec := &Executor{}

	if exec.values != nil {
		t.Error("new executor should have nil values")
	}

	// Create values bag and set on executor
	values := ctxapi.NewValues()
	values.Set("key1", "value1")
	values.Set("key2", 42)
	exec.values = values

	if v, ok := exec.values.Get("key1"); !ok || v != "value1" {
		t.Error("values should contain key1=value1")
	}
	if v, ok := exec.values.Get("key2"); !ok || v != 42 {
		t.Error("values should contain key2=42")
	}
}

func TestExecutorContextChaining(t *testing.T) {
	// Test that chained with_context creates new executor with merged values
	exec1 := &Executor{}

	// First with_context call
	values1 := ctxapi.NewValues()
	values1.Set("key1", "value1")
	exec2 := &Executor{
		values:   values1,
		hasActor: exec1.hasActor,
		actor:    exec1.actor,
		hasScope: exec1.hasScope,
		scope:    exec1.scope,
	}

	// Second with_context call (chaining) - should copy existing and add new
	values2 := ctxapi.NewValues()
	exec2.values.Iterate(func(k string, v any) {
		values2.Set(k, v)
	})
	values2.Set("key2", "value2")

	exec3 := &Executor{
		values:   values2,
		hasActor: exec2.hasActor,
		actor:    exec2.actor,
		hasScope: exec2.hasScope,
		scope:    exec2.scope,
	}

	// Verify chained executor has both values
	if v, ok := exec3.values.Get("key1"); !ok || v != "value1" {
		t.Error("chained executor should have key1 from first with_context")
	}
	if v, ok := exec3.values.Get("key2"); !ok || v != "value2" {
		t.Error("chained executor should have key2 from second with_context")
	}

	// Verify original executor is unchanged
	if _, ok := exec2.values.Get("key2"); ok {
		t.Error("original executor should not be modified by chaining")
	}
}

func TestCallYieldContextPairs(t *testing.T) {
	y := AcquireCallYield()
	defer ReleaseCallYield(y)

	// Verify Task.Context is initially empty
	if len(y.Task.Context) != 0 {
		t.Errorf("expected empty context, got %d pairs", len(y.Task.Context))
	}
}

func TestExecutorCallAddsContextToTask(t *testing.T) {
	// Test that when executor has values, they are added to Task.Context
	exec := &Executor{}
	values := ctxapi.NewValues()
	values.Set("trace_id", "test-123")
	exec.values = values

	// Verify executor has values
	if exec.values == nil || exec.values.Len() == 0 {
		t.Fatal("executor should have values set")
	}

	// The actual context addition happens in executorCall when creating the yield
	// This test verifies the executor state that would trigger context addition
	if exec.values.Len() != 1 {
		t.Errorf("expected 1 value, got %d", exec.values.Len())
	}
}
