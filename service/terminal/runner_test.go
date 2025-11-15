package terminal

import (
	"context"
	"errors"
	ctxapi "github.com/wippyai/runtime/api/context"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"

	"github.com/wippyai/runtime/api/payload"
)

// DummyProcess implements process.Process for testing purposes.
type DummyProcess struct {
	stepCount int
	maxSteps  int
}

func (dp *DummyProcess) Start(_ context.Context, _ relay.PID, _ payload.Payloads) error {
	// No-op startup.
	return nil
}

func (dp *DummyProcess) Step() (process.StepResult, error) {
	dp.stepCount++
	// AddCleanup maxSteps, return an error to simulate process failure.
	if dp.stepCount >= dp.maxSteps {
		return process.StepDone, errors.New("dummy step error")
	}
	time.Sleep(10 * time.Millisecond)
	return process.StepIdle, nil
}

func (dp *DummyProcess) Send(_ *relay.Package) error {
	// Accept all messages.
	return nil
}

func (dp *DummyProcess) Terminate() {}

func TestTerminalRunnerStopsOnStepError(t *testing.T) {
	// Set up a dummy process that fails after 3 steps.
	dp := &DummyProcess{maxSteps: 3}

	// Create a dummy pid. Note that registry.Process is typically a string.
	dummyPID := relay.PID{
		Host:   "dummy",
		UniqID: "test",
	}

	lp := &process.Launch{
		PID:     dummyPID,
		Process: dp,
		Input:   nil,
	}

	rootCtx := ctxapi.NewRootContext()
	ctx, _ := ctxapi.OpenFrameContext(rootCtx)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	runner, err := NewTerminalRunner(ctx, DefaultRunnerConfig(), lp)
	if err != nil {
		t.Fatalf("expected no error starting runner, got: %v", err)
	}

	// Wait for the runner to cancel due to the dummy error.
	select {
	case <-runner.Wait():
		// Expected cancellation.
	case <-time.After(2 * time.Second):
		t.Fatalf("runner did not stop as expected")
	}
}

func TestTerminalRunnerSendAndStop(t *testing.T) {
	// Set up a dummy process that will not error on steps.
	dp := &DummyProcess{maxSteps: 100}

	dummyPID := relay.PID{
		Host:   "dummy",
		UniqID: "test",
	}

	lp := &process.Launch{
		PID:     dummyPID,
		Process: dp,
		Input:   nil,
	}

	rootCtx := ctxapi.NewRootContext()
	ctx, _ := ctxapi.OpenFrameContext(rootCtx)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	runner, err := NewTerminalRunner(ctx, DefaultRunnerConfig(), lp)
	if err != nil {
		t.Fatalf("expected no error starting runner, got: %v", err)
	}

	// Test the send method.
	err = runner.Send(&relay.Package{Messages: []*relay.Message{{Topic: "test"}}})
	if err != nil {
		t.Errorf("expected no error on send, got: %v", err)
	}

	// close the runner explicitly.
	runner.Stop()

	select {
	case <-runner.Wait():
		// Success: runner has stopped.
	case <-time.After(2 * time.Second):
		t.Fatalf("runner did not stop as expected after close()")
	}
}
