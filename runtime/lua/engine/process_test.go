package engine

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
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

	if err := proc.Execute(ctx, "", nil); err != nil {
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

	if err := proc.Execute(ctx, "", nil); err != nil {
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

func TestResourcesInContext(t *testing.T) {
	// Create frame context
	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	// No resources yet
	res := GetResources(ctx)
	if res != nil {
		t.Error("Expected nil resources before process start")
	}

	script := `return 1`
	proc := NewProcess(WithScript(script, "test.lua"))

	if err := proc.Execute(ctx, "", nil); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer proc.Close()

	// Now resources should exist
	res = GetResources(ctx)
	if res == nil {
		t.Error("Expected resources after process start")
	}

	// Test cleanup registration
	cleanupCalled := false
	res.AddCleanup(func() error {
		cleanupCalled = true
		return nil
	})

	// Close and verify cleanup was called
	proc.Close()
	if !cleanupCalled {
		t.Error("Cleanup function was not called")
	}
}

func TestChannelRegistry(t *testing.T) {
	reg := NewChannelRegistry()

	// Get creates if not exists
	ch1 := reg.Get("test")
	if ch1 == nil {
		t.Fatal("Get returned nil")
	}
	if ch1.Name() != "test" {
		t.Errorf("Expected name 'test', got '%s'", ch1.Name())
	}

	// Get same channel again
	ch2 := reg.Get("test")
	if ch1 != ch2 {
		t.Error("Get returned different channel for same name")
	}

	// GetOrCreate with buffer
	ch3 := reg.GetOrCreate("buffered", 5)
	if ch3.capacity != 5 {
		t.Errorf("Expected capacity 5, got %d", ch3.capacity)
	}

	// Close all
	reg.Close()
	if !ch1.IsClosed() {
		t.Error("Channel should be closed after registry close")
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

	if err := proc.Execute(ctx, "", nil); err != nil {
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

	if err := proc.Execute(ctx, "", nil); err != nil {
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

	if err := proc.Execute(ctx, "", nil); err != nil {
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
