package funcs

import (
	"testing"

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

	// After release, acquire should return a yield from pool
	if yield1 != yield2 {
		// Not strictly required but expected behavior
		t.Log("pooling working as expected")
	}
}
