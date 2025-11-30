package clock

import (
	"context"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"github.com/wippyai/runtime/api/dispatcher"
	clockapi "github.com/wippyai/runtime/api/dispatcher/clock"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
)

func TestMakeSleepCmd(t *testing.T) {
	stack := []uint64{uint64(100 * time.Millisecond)}
	cmd := makeSleepCmd(stack)

	sleepCmd, ok := cmd.(clockapi.SleepCmd)
	if !ok {
		t.Fatalf("expected SleepCmd, got %T", cmd)
	}

	if sleepCmd.Duration != 100*time.Millisecond {
		t.Errorf("expected 100ms, got %v", sleepCmd.Duration)
	}

	if cmd.CmdID() != clockapi.CmdSleep {
		t.Errorf("expected CmdSleep (%d), got %d", clockapi.CmdSleep, cmd.CmdID())
	}
}

func TestMakeNowCmd(t *testing.T) {
	cmd := makeNowCmd(nil)

	_, ok := cmd.(clockapi.NowCmd)
	if !ok {
		t.Fatalf("expected NowCmd, got %T", cmd)
	}

	if cmd.CmdID() != clockapi.CmdNow {
		t.Errorf("expected CmdNow (%d), got %d", clockapi.CmdNow, cmd.CmdID())
	}
}

func TestMakeTimerStartCmd(t *testing.T) {
	stack := []uint64{uint64(50 * time.Millisecond)}
	cmd := makeTimerStartCmd(stack)

	timerCmd, ok := cmd.(clockapi.TimerStartCmd)
	if !ok {
		t.Fatalf("expected TimerStartCmd, got %T", cmd)
	}

	if timerCmd.Duration != 50*time.Millisecond {
		t.Errorf("expected 50ms, got %v", timerCmd.Duration)
	}

	if cmd.CmdID() != clockapi.CmdTimerStart {
		t.Errorf("expected CmdTimerStart (%d), got %d", clockapi.CmdTimerStart, cmd.CmdID())
	}
}

func TestMakeTimerWaitCmd(t *testing.T) {
	stack := []uint64{42}
	cmd := makeTimerWaitCmd(stack)

	timerCmd, ok := cmd.(clockapi.TimerWaitCmd)
	if !ok {
		t.Fatalf("expected TimerWaitCmd, got %T", cmd)
	}

	if timerCmd.TimerID != 42 {
		t.Errorf("expected timer ID 42, got %v", timerCmd.TimerID)
	}

	if cmd.CmdID() != clockapi.CmdTimerWait {
		t.Errorf("expected CmdTimerWait (%d), got %d", clockapi.CmdTimerWait, cmd.CmdID())
	}
}

func TestMakeTimerStopCmd(t *testing.T) {
	stack := []uint64{123}
	cmd := makeTimerStopCmd(stack)

	timerCmd, ok := cmd.(clockapi.TimerStopCmd)
	if !ok {
		t.Fatalf("expected TimerStopCmd, got %T", cmd)
	}

	if timerCmd.TimerID != 123 {
		t.Errorf("expected timer ID 123, got %v", timerCmd.TimerID)
	}

	if cmd.CmdID() != clockapi.CmdTimerStop {
		t.Errorf("expected CmdTimerStop (%d), got %d", clockapi.CmdTimerStop, cmd.CmdID())
	}
}

func TestHostInfo(t *testing.T) {
	h := New()
	info := h.Info()

	if info.Namespace != Namespace {
		t.Errorf("expected namespace %q, got %q", Namespace, info.Namespace)
	}
}

func TestHostRegister(t *testing.T) {
	h := New()
	reg := h.Register()

	if len(reg.Functions) != 5 {
		t.Errorf("expected 5 functions, got %d", len(reg.Functions))
	}

	for _, name := range []string{"sleep", "now", "timer_start", "timer_wait", "timer_stop"} {
		if _, ok := reg.Functions[name]; !ok {
			t.Errorf("missing function %q", name)
		}
	}

	if len(reg.YieldTypes) != 5 {
		t.Errorf("expected 5 yield types, got %d", len(reg.YieldTypes))
	}
}

// TestSleepHandlerWithAsyncify tests the sleep handler with asyncify context
func TestSleepHandlerWithAsyncify(_ *testing.T) {
	ctx := context.Background()

	// Create minimal wazero runtime for testing
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	// Create asyncify and scheduler
	asyncify := wasmengine.NewAsyncify()
	scheduler := wasmengine.NewScheduler(asyncify)

	// Add to context
	ctx = wasmengine.WithAsyncify(ctx, asyncify)
	ctx = wasmengine.WithScheduler(ctx, scheduler)

	// Create stack with sleep duration
	stack := []uint64{uint64(10 * time.Millisecond)}

	// Call the handler - should suspend
	SleepHandler(ctx, nil, stack)
}

// TestClockIntegration tests the full clock host integration
func TestClockIntegration(t *testing.T) {
	h := New()
	reg := h.Register()

	// Verify all functions implement api.GoModuleFunc
	for name, fn := range reg.Functions {
		if _, ok := fn.(api.GoModuleFunc); !ok {
			t.Errorf("function %q is not api.GoModuleFunc, got %T", name, fn)
		}
	}

	// Verify yield types match commands
	expectedYields := map[dispatcher.CommandID]bool{
		clockapi.CmdSleep:      true,
		clockapi.CmdNow:        true,
		clockapi.CmdTimerStart: true,
		clockapi.CmdTimerWait:  true,
		clockapi.CmdTimerStop:  true,
	}

	for _, yt := range reg.YieldTypes {
		if !expectedYields[yt.CmdID] {
			t.Errorf("unexpected yield type: %d", yt.CmdID)
		}
		delete(expectedYields, yt.CmdID)
	}

	if len(expectedYields) > 0 {
		t.Errorf("missing yield types: %v", expectedYields)
	}
}

func TestSleepCmdZeroDuration(t *testing.T) {
	stack := []uint64{0}
	cmd := makeSleepCmd(stack)

	sleepCmd := cmd.(clockapi.SleepCmd)
	if sleepCmd.Duration != 0 {
		t.Errorf("expected 0, got %v", sleepCmd.Duration)
	}
}

func TestSleepCmdEmptyStack(t *testing.T) {
	cmd := makeSleepCmd(nil)

	sleepCmd := cmd.(clockapi.SleepCmd)
	if sleepCmd.Duration != 0 {
		t.Errorf("expected 0 for empty stack, got %v", sleepCmd.Duration)
	}
}

func TestTimerStartCmdEmptyStack(t *testing.T) {
	cmd := makeTimerStartCmd(nil)

	timerCmd := cmd.(clockapi.TimerStartCmd)
	if timerCmd.Duration != 0 {
		t.Errorf("expected 0 for empty stack, got %v", timerCmd.Duration)
	}
}

func TestTimerWaitCmdEmptyStack(t *testing.T) {
	cmd := makeTimerWaitCmd(nil)

	timerCmd := cmd.(clockapi.TimerWaitCmd)
	if timerCmd.TimerID != 0 {
		t.Errorf("expected 0 for empty stack, got %v", timerCmd.TimerID)
	}
}

func TestTimerStopCmdEmptyStack(t *testing.T) {
	cmd := makeTimerStopCmd(nil)

	timerCmd := cmd.(clockapi.TimerStopCmd)
	if timerCmd.TimerID != 0 {
		t.Errorf("expected 0 for empty stack, got %v", timerCmd.TimerID)
	}
}
