package engine2

import (
	"context"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/low-engine-v2/scheduler"
)

func TestProcessBasicExecution(t *testing.T) {
	script := `
		local result = 1 + 2
		return result
	`

	proc := NewProcess(WithScript(script, "test.lua"))

	// Create frame context
	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Start(ctx, nil); err != nil {
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

func TestProcessWithTimeSleep(t *testing.T) {
	script := `
		time.sleep(time.MILLISECOND * 10)
		return "done"
	`

	proc := NewProcess(WithScript(script, "test.lua"))

	// Create frame context
	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Start(ctx, nil); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer proc.Close()

	// Bind time module
	BindTimeSleep(proc.State())

	result, err := proc.Step(nil)
	if err != nil {
		t.Fatalf("Step failed: %v", err)
	}

	if result.Status != scheduler.StepContinue {
		t.Fatalf("Expected StepContinue for sleep yield, got %v", result.Status)
	}

	if result.YieldCount == 0 {
		t.Fatal("Expected at least one yield command")
	}

	// Verify it's a sleep command
	yields := result.GetYields()
	if len(yields) == 0 {
		t.Fatal("No yields returned")
	}

	// Simulate sleep completion
	time.Sleep(15 * time.Millisecond)

	// Resume with no results (sleep just completes)
	result, err = proc.Step(&scheduler.YieldResults{})
	if err != nil {
		t.Fatalf("Step after sleep failed: %v", err)
	}

	if result.Status != scheduler.StepDone {
		t.Errorf("Expected StepDone after sleep, got %v", result.Status)
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

	if err := proc.Start(ctx, nil); err != nil {
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

	if err := proc.Start(ctx, nil); err != nil {
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
