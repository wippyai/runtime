package terminal

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/registry"
	"go.uber.org/zap"
)

// DummyProcess implements process.Process for testing purposes.
type DummyProcess struct {
	stepCount int
	maxSteps  int
}

func (dp *DummyProcess) Start(ctx context.Context, pid process.PID, input payload.Payloads) error {
	// No-op startup.
	return nil
}

func (dp *DummyProcess) Step() error {
	dp.stepCount++
	// After maxSteps, return an error to simulate process failure.
	if dp.stepCount >= dp.maxSteps {
		return errors.New("dummy step error")
	}
	time.Sleep(10 * time.Millisecond)
	return nil
}

func (dp *DummyProcess) Send(msg ...*process.Message) error {
	// Accept all messages.
	return nil
}

func TestTerminalRunnerStopsOnStepError(t *testing.T) {
	// Set up a dummy process that fails after 3 steps.
	dp := &DummyProcess{maxSteps: 3}

	// Create a dummy PID. Note that registry.ID is typically a string.
	dummyPID := process.PID{
		Host: "dummy",
		ID:   registry.ID{Name: "test-id"},
		Name: "test",
	}

	lp := &process.LaunchProcess{
		PID:     dummyPID,
		Process: dp,
		Input:   nil,
	}

	logger := zap.NewNop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	runner, err := NewTerminalRunner(ctx, DefaultRunnerConfig(), logger, lp)
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

	dummyPID := process.PID{
		Host: "dummy",
		ID:   registry.ID{Name: "test-id"},
		Name: "test",
	}

	lp := &process.LaunchProcess{
		PID:     dummyPID,
		Process: dp,
		Input:   nil,
	}

	logger := zap.NewNop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	runner, err := NewTerminalRunner(ctx, DefaultRunnerConfig(), logger, lp)
	if err != nil {
		t.Fatalf("expected no error starting runner, got: %v", err)
	}

	// Test the Send method.
	err = runner.Send(&process.Message{Topic: "test"})
	if err != nil {
		t.Errorf("expected no error on Send, got: %v", err)
	}

	// Stop the runner explicitly.
	runner.Stop()

	select {
	case <-runner.Wait():
		// Success: runner has stopped.
	case <-time.After(2 * time.Second):
		t.Fatalf("runner did not stop as expected after Stop()")
	}
}
