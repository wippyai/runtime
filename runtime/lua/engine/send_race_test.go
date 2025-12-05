package engine

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/relay"
	scheduler "github.com/wippyai/runtime/system/scheduler/actor"
)

// TestProcessResetSafety tests that Reset() is safe to call after Step() completes.
// This simulates what pool schedulers do: Step() → active.Delete() → Reset()
// The key property being tested is that Reset() is idempotent and safe.
func TestProcessResetSafety(t *testing.T) {
	script := `return 1`

	for iteration := 0; iteration < 1000; iteration++ {
		proc := NewProcess(WithScript(script, "test.lua"))
		ctx, _ := ctxapi.OpenFrameContext(context.Background())

		if err := proc.Init(ctx, "", nil); err != nil {
			t.Fatalf("Init failed: %v", err)
		}

		// Step to completion - this calls clearExecution() but NOT Reset()
		result, err := proc.Step(nil)
		if err != nil {
			t.Fatalf("Step failed: %v", err)
		}
		if result.Status != scheduler.StepDone {
			t.Fatalf("Expected StepDone, got %v", result.Status)
		}

		// Simulate pool behavior: call Reset() after completion
		proc.Reset()

		// Reset should be idempotent - safe to call multiple times
		proc.Reset()
		proc.Reset()

		proc.Close()
	}
}

// TestProcessSendDuringExecution tests that Send() is safe during execution.
// This is the valid case where messages arrive while process is running.
func TestProcessSendDuringExecution(t *testing.T) {
	script := `return 1`

	for iteration := 0; iteration < 100; iteration++ {
		proc := NewProcess(WithScript(script, "test.lua"))
		ctx, _ := ctxapi.OpenFrameContext(context.Background())

		if err := proc.Init(ctx, "", nil); err != nil {
			t.Fatalf("Init failed: %v", err)
		}

		// Send some messages during execution (this is valid and expected)
		pkg := &relay.Package{
			Target:   relay.PID{UniqID: "test"},
			Messages: []*relay.Message{{Topic: "test-topic"}},
		}
		_ = proc.Send(pkg)

		// Run to completion
		result, err := proc.Step(nil)
		if err != nil {
			t.Fatalf("Step failed: %v", err)
		}
		if result.Status != scheduler.StepDone {
			t.Fatalf("Expected StepDone, got %v", result.Status)
		}

		proc.Reset()
		proc.Close()
	}
}
