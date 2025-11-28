package clock

import (
	"context"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	clockapi "github.com/wippyai/runtime/api/dispatcher/clock"
	"github.com/wippyai/runtime/api/workflow"
)

func TestSleepHandler(t *testing.T) {
	h := NewSleepHandler()
	ctx := context.Background()

	start := time.Now()
	err := h.Handle(ctx, clockapi.SleepCmd{Duration: 10 * time.Millisecond}, func(data any) {})
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if elapsed < 10*time.Millisecond {
		t.Errorf("sleep too short: %v", elapsed)
	}
}

func TestSleepHandlerZeroDuration(t *testing.T) {
	h := NewSleepHandler()
	ctx := context.Background()

	start := time.Now()
	err := h.Handle(ctx, clockapi.SleepCmd{Duration: 0}, func(data any) {})
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if elapsed > time.Millisecond {
		t.Errorf("zero sleep took too long: %v", elapsed)
	}
}

func TestSleepHandlerCancellation(t *testing.T) {
	h := NewSleepHandler()
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	err := h.Handle(ctx, clockapi.SleepCmd{Duration: time.Second}, func(data any) {})
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestTimerHandler(t *testing.T) {
	h := NewTimerHandler()
	ctx := context.Background()

	var emitted any
	start := time.Now()
	err := h.Handle(ctx, clockapi.TimerCmd{Duration: 10 * time.Millisecond}, func(data any) {
		emitted = data
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if elapsed < 10*time.Millisecond {
		t.Errorf("timer too short: %v", elapsed)
	}
	if emitted == nil {
		t.Error("timer did not emit fire time")
	}
}

func TestTimerHandlerZeroDuration(t *testing.T) {
	h := NewTimerHandler()
	ctx := context.Background()

	var emitted any
	err := h.Handle(ctx, clockapi.TimerCmd{Duration: 0}, func(data any) {
		emitted = data
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if emitted == nil {
		t.Error("zero timer did not emit time")
	}
}

func TestNowHandler(t *testing.T) {
	h := NewNowHandler()
	ctx := context.Background()

	var emitted any
	before := time.Now().UnixNano()
	err := h.Handle(ctx, clockapi.NowCmd{}, func(data any) {
		emitted = data
	})
	after := time.Now().UnixNano()

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	nanos, ok := emitted.(int64)
	if !ok {
		t.Fatalf("expected int64, got %T", emitted)
	}
	if nanos < before || nanos > after {
		t.Errorf("emitted time %d not between %d and %d", nanos, before, after)
	}
}

type mockTimeReference struct {
	fixedTime time.Time
}

func (m *mockTimeReference) Now() time.Time       { return m.fixedTime }
func (m *mockTimeReference) StartTime() time.Time { return m.fixedTime }

func TestNowHandlerWithTimeReference(t *testing.T) {
	h := NewNowHandler()
	fixedTime := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	workflow.WithTimeReference(ctx, &mockTimeReference{fixedTime: fixedTime})

	var emitted any
	err := h.Handle(ctx, clockapi.NowCmd{}, func(data any) {
		emitted = data
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	nanos, ok := emitted.(int64)
	if !ok {
		t.Fatalf("expected int64, got %T", emitted)
	}
	if nanos != fixedTime.UnixNano() {
		t.Errorf("expected %d, got %d", fixedTime.UnixNano(), nanos)
	}
}

func TestMockClock(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := NewMockClock(start)

	if got := clock.Now(); !got.Equal(start) {
		t.Errorf("expected %v, got %v", start, got)
	}

	clock.Advance(time.Hour)
	expected := start.Add(time.Hour)
	if got := clock.Now(); !got.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, got)
	}

	newTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock.Set(newTime)
	if got := clock.Now(); !got.Equal(newTime) {
		t.Errorf("expected %v, got %v", newTime, got)
	}
}

func TestMockSleepHandler(t *testing.T) {
	clock := NewMockClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	h := NewMockSleepHandler(clock)

	initial := clock.Now()
	err := h.Handle(context.Background(), clockapi.SleepCmd{Duration: time.Hour}, func(data any) {})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Mock clock should advance
	expected := initial.Add(time.Hour)
	if got := clock.Now(); !got.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, got)
	}

	// Should have recorded the sleep
	sleeps := h.Sleeps()
	if len(sleeps) != 1 || sleeps[0].Duration != time.Hour {
		t.Errorf("expected 1 sleep of 1h, got %v", sleeps)
	}
}

func TestMockTimerHandler(t *testing.T) {
	clock := NewMockClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	h := NewMockTimerHandler(clock)

	var emitted any
	err := h.Handle(context.Background(), clockapi.TimerCmd{Duration: 30 * time.Minute}, func(data any) {
		emitted = data
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Mock clock should advance
	if got := clock.Now(); got.Hour() != 0 || got.Minute() != 30 {
		t.Errorf("expected 00:30, got %v", got)
	}

	// Should emit the new time
	if emitted == nil {
		t.Error("timer did not emit")
	}
}

func TestMockNowHandler(t *testing.T) {
	fixedTime := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	clock := NewMockClock(fixedTime)
	h := NewMockNowHandler(clock)

	var emitted any
	err := h.Handle(context.Background(), clockapi.NowCmd{}, func(data any) {
		emitted = data
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	nanos, ok := emitted.(int64)
	if !ok {
		t.Fatalf("expected int64, got %T", emitted)
	}
	if nanos != fixedTime.UnixNano() {
		t.Errorf("expected %d, got %d", fixedTime.UnixNano(), nanos)
	}
}

func TestMockService(t *testing.T) {
	svc := NewMockService(3)

	// Test that all handlers are initialized
	if svc.Sleep == nil || svc.Timer == nil || svc.Ticker == nil || svc.Now == nil {
		t.Error("mock service has nil handlers")
	}

	// Test clock is shared
	svc.Clock.Advance(time.Hour)
	if got := svc.Clock.Now().Hour(); got != 1 {
		t.Errorf("expected hour 1, got %d", got)
	}
}

func TestServiceRegisterAll(t *testing.T) {
	svc := NewService()
	handlers := make(map[dispatcher.CommandID]bool)

	svc.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = true
	})

	expected := []dispatcher.CommandID{
		clockapi.CmdSleep,
		clockapi.CmdTicker,
		clockapi.CmdTimer,
		clockapi.CmdNow,
	}

	for _, id := range expected {
		if !handlers[id] {
			t.Errorf("handler for command %d not registered", id)
		}
	}
}
