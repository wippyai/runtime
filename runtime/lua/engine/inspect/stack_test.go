package inspect

import (
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestStackInspector(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	script := `
		-- Global variable
		global_var = "global"

		-- Function with upvalue
		local function make_counter()
			local count = 0
			return function()
				count = count + 1
				return count
			end
		end

		-- Create counter
		local counter = make_counter()

		function test_func(x)
			local local_var = "local"
			inspect_main()
			return x
		end

		-- Create a coroutine
		local co = coroutine.create(function()
			local co_var = "coroutine local"
			inspect_co()
			return "done"
		end)

		-- Call test_func first
		test_func(42)

		-- Then run coroutine
		coroutine.resume(co)
	`

	// Register inspector functions
	L.SetGlobal("inspect_main", L.NewFunction(makeInspectFunc("main_trace")))
	L.SetGlobal("inspect_co", L.NewFunction(makeInspectFunc("co_trace")))

	if err := L.DoString(script); err != nil {
		t.Fatalf("Failed to run script: %v", err)
	}

	registry := L.Get(lua.RegistryIndex).(*lua.LTable)
	mainTrace := registry.RawGetString("main_trace").String()
	coTrace := registry.RawGetString("co_trace").String()

	// Verify main trace
	if !strings.Contains(mainTrace, "test_func") {
		t.Error("Main trace missing test_func frame")
	}
	if !strings.Contains(mainTrace, "x = 42") {
		t.Error("Main trace missing x parameter")
	}
	if !strings.Contains(mainTrace, "local_var = local") {
		t.Error("Main trace missing local_var")
	}

	// Verify coroutine trace
	if !strings.Contains(coTrace, "co_var = coroutine local") {
		t.Error("Coroutine trace missing co_var")
	}
}

func TestSimpleStack(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	L.SetGlobal("inspect_stack", L.NewFunction(makeInspectFunc("simple_trace")))

	script := `
		function foo(x)
			local y = x + 1
			inspect_stack()
			return y
		end
		foo(42)
	`

	if err := L.DoString(script); err != nil {
		t.Fatalf("Failed to run script: %v", err)
	}

	registry := L.Get(lua.RegistryIndex).(*lua.LTable)
	trace := registry.RawGetString("simple_trace").String()

	if !strings.Contains(trace, "foo") {
		t.Error("Stack trace missing foo function")
	}
	if !strings.Contains(trace, "x = 42") {
		t.Error("Stack trace missing parameter x")
	}
	if !strings.Contains(trace, "y = 43") {
		t.Error("Stack trace missing local y")
	}
}

func TestInspectYieldedCoroutines(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	script := `
		function worker(id)
			local data = "some data for " .. id
			local counter = 0
			local function increment()
				counter = counter + 1
				return counter
			end
			coroutine.yield(increment())
			return "done"
		end

		co1 = coroutine.create(function() return worker(1) end)
		co2 = coroutine.create(function() return worker(2) end)
		coroutine.resume(co1)
		coroutine.resume(co2)
	`

	if err := L.DoString(script); err != nil {
		t.Fatalf("Failed to run script: %v", err)
	}

	traces := GetAllCoroutineStacks(L)
	foundCo1, foundCo2 := false, false

	for _, trace := range traces {
		if len(trace.Frames) > 0 {
			frame := trace.Frames[0]
			if hasLocal(frame, "id", "1") {
				foundCo1 = true
				validateWorkerFrame(t, frame, 1)
			} else if hasLocal(frame, "id", "2") {
				foundCo2 = true
				validateWorkerFrame(t, frame, 2)
			}
		}
	}

	if !foundCo1 {
		t.Error("Could not find coroutine 1")
	}
	if !foundCo2 {
		t.Error("Could not find coroutine 2")
	}
}

func TestInspectNestedYield(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	script := `
		function deep_func(depth)
			local current = depth
			local msg = "yielding at depth " .. current
			coroutine.yield(msg)
			return current + 1
		end

		function mid_func(val)
			local temp = val * 2
			local result = deep_func(temp)
			return result * 2
		end

		co = coroutine.create(function() 
			return mid_func(5)
		end)
		coroutine.resume(co)
	`

	if err := L.DoString(script); err != nil {
		t.Fatalf("Failed to run script: %v", err)
	}

	traces := GetAllCoroutineStacks(L)
	var foundDeepFunc bool

	for _, trace := range traces {
		if len(trace.Frames) >= 2 { // We expect at least deep_func and mid_func frames
			deepFrame := trace.Frames[0]
			if hasLocal(deepFrame, "current", "10") {
				foundDeepFunc = true
				validateDeepFrame(t, deepFrame)
				validateMidFrame(t, trace.Frames[1])
			}
		}
	}

	if !foundDeepFunc {
		t.Error("Could not find yielded deep_func coroutine")
	}
}

// Helper function to check for a local variable with specific value
func hasLocal(frame StackFrame, name, expectedValue string) bool {
	for _, local := range frame.Locals {
		if local.Name == name && local.Value.String() == expectedValue {
			return true
		}
	}
	return false
}

// Helper function to validate worker frame
func validateWorkerFrame(t *testing.T, frame StackFrame, id int) {
	expectedData := "some data for " + string(rune('0'+id))
	if !hasLocal(frame, "data", expectedData) {
		t.Errorf("Worker %d missing expected data local", id)
	}
	if !hasLocal(frame, "counter", "1") {
		t.Errorf("Worker %d missing expected counter value", id)
	}
}

// Helper function to validate deep_func frame
func validateDeepFrame(t *testing.T, frame StackFrame) {
	if !hasLocal(frame, "depth", "10") {
		t.Error("deep_func missing expected depth parameter")
	}
	if !hasLocal(frame, "msg", "yielding at depth 10") {
		t.Error("deep_func missing expected msg local")
	}
}

// Helper function to validate mid_func frame
func validateMidFrame(t *testing.T, frame StackFrame) {
	if !hasLocal(frame, "val", "5") {
		t.Error("mid_func missing expected val parameter")
	}
	if !hasLocal(frame, "temp", "10") {
		t.Error("mid_func missing expected temp local")
	}
}

// Helper function that stores stack trace with a given key
func makeInspectFunc(key string) lua.LGFunction {
	return func(L *lua.LState) int {
		trace := GetStackTrace(L)
		L.SetField(L.Get(lua.RegistryIndex).(*lua.LTable), key, lua.LString(trace.String()))
		return 0
	}
}
