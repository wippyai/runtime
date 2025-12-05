package engine

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/runtime/resource"
	scheduler "github.com/wippyai/runtime/system/scheduler/actor"
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

	result, err := proc.Step(nil)
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	if result.Status != scheduler.StepDone {
		t.Errorf("Expected StepDone, got %v", result.Status)
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
	for i := 0; i < 10; i++ {
		result, err := proc.Step(nil)
		if err != nil {
			t.Fatalf("Step %d failed: %v", i, err)
		}
		if result.Status == scheduler.StepDone {
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
	_, err := proc.Step(nil)

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
	for i := 0; i < 10; i++ {
		result, err := proc.Step(nil)
		if err != nil {
			t.Fatalf("Step failed: %v", err)
		}
		if result.Status == scheduler.StepDone {
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

	_, err := proc.Step(nil)

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
	// Test that Step() captures the return value in StepResult.Result
	script := `return {ok = true, value = 42}`

	proc := NewProcess(WithScript(script, "test.lua"))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	result, err := proc.Step(nil)
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	if result.Status != scheduler.StepDone {
		t.Fatalf("Expected StepDone, got %v", result.Status)
	}

	// The result should contain the return value
	if result.Result == nil {
		t.Fatal("Expected result.Result to be non-nil, got nil")
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

	result, err := proc.Step(nil)
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	if result.Status != scheduler.StepDone {
		t.Fatalf("Expected StepDone, got %v", result.Status)
	}

	if result.Result == nil {
		t.Fatal("Expected result.Result to be non-nil")
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

	result, err := proc.Step(nil)
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	if result.Status != scheduler.StepDone {
		t.Fatalf("Expected StepDone, got %v", result.Status)
	}

	if result.Result == nil {
		t.Fatal("Expected result.Result to be non-nil")
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

	result, err := proc.Step(nil)
	if result.Status != scheduler.StepDone {
		t.Fatalf("Expected StepDone, got %v", result.Status)
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
		ModuleBinders: []ModuleBinder{BindErrorsModule},
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

	result, stepErr := proc.Step(nil)
	if result.Status != scheduler.StepDone {
		t.Fatalf("Expected StepDone, got %v", result.Status)
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

	result, err := proc.Step(nil)
	if result.Status != scheduler.StepDone {
		t.Fatalf("Expected StepDone, got %v", result.Status)
	}

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if result.Result == nil {
		t.Fatal("Expected result.Result to be non-nil")
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

	result, err := proc.Step(nil)
	if result.Status != scheduler.StepDone {
		t.Fatalf("Expected StepDone, got %v", result.Status)
	}

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
}
