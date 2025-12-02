package clock

import (
	"context"
	"testing"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/workflow"
)

func TestSleepHandler(t *testing.T) {
	d := NewDispatcher()
	defer d.Stop(context.Background())

	var handlers = make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	h := handlers[clockapi.CmdSleep]
	ctx := context.Background()

	done := make(chan struct{})
	start := time.Now()
	err := h.Handle(ctx, clockapi.SleepCmd{Duration: 10 * time.Millisecond}, func(data any) {
		close(done)
	})

	<-done
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if elapsed < 10*time.Millisecond {
		t.Errorf("sleep too short: %v", elapsed)
	}
}

func TestSleepHandlerZeroDuration(t *testing.T) {
	d := NewDispatcher()
	defer d.Stop(context.Background())

	var handlers = make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	h := handlers[clockapi.CmdSleep]
	ctx := context.Background()

	var emitted bool
	err := h.Handle(ctx, clockapi.SleepCmd{Duration: 0}, func(data any) {
		emitted = true
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !emitted {
		t.Error("expected emit for zero duration")
	}
}

func TestTimerStartHandler(t *testing.T) {
	d := NewDispatcher()
	defer d.Stop(context.Background())

	var handlers = make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	h := handlers[clockapi.CmdTimerStart]

	var emitted any
	err := h.Handle(context.Background(), clockapi.TimerStartCmd{Duration: 10 * time.Millisecond}, func(data any) {
		emitted = data
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	id, ok := emitted.(uint64)
	if !ok || id == 0 {
		t.Errorf("expected non-zero timer ID, got %v", emitted)
	}
}

func TestTimerStartHandlerZeroDuration(t *testing.T) {
	d := NewDispatcher()
	defer d.Stop(context.Background())

	var handlers = make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	h := handlers[clockapi.CmdTimerStart]

	var emitted bool
	err := h.Handle(context.Background(), clockapi.TimerStartCmd{Duration: 0}, func(data any) {
		emitted = true
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if emitted {
		t.Error("expected no emit for zero duration")
	}
}

func TestTimerWaitHandler(t *testing.T) {
	d := NewDispatcher()
	defer d.Stop(context.Background())

	var handlers = make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	startHandler := handlers[clockapi.CmdTimerStart]
	waitHandler := handlers[clockapi.CmdTimerWait]

	var timerID uint64
	startHandler.Handle(context.Background(), clockapi.TimerStartCmd{Duration: 10 * time.Millisecond}, func(data any) {
		timerID = data.(uint64)
	})

	var emitted any
	start := time.Now()
	done := make(chan struct{})
	err := waitHandler.Handle(context.Background(), clockapi.TimerWaitCmd{TimerID: timerID}, func(data any) {
		emitted = data
		close(done)
	})

	<-done
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if elapsed < 10*time.Millisecond {
		t.Errorf("timer wait too short: %v", elapsed)
	}
	if emitted == nil {
		t.Error("timer did not emit fire time")
	}
}

func TestTimerStopHandler(t *testing.T) {
	d := NewDispatcher()
	defer d.Stop(context.Background())

	var handlers = make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	startHandler := handlers[clockapi.CmdTimerStart]
	stopHandler := handlers[clockapi.CmdTimerStop]

	var timerID uint64
	startHandler.Handle(context.Background(), clockapi.TimerStartCmd{Duration: time.Hour}, func(data any) {
		timerID = data.(uint64)
	})

	var emitted any
	err := stopHandler.Handle(context.Background(), clockapi.TimerStopCmd{TimerID: timerID}, func(data any) {
		emitted = data
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	stopped, ok := emitted.(bool)
	if !ok {
		t.Fatalf("expected bool, got %T", emitted)
	}
	if !stopped {
		t.Error("expected timer to be stopped")
	}
}

func TestNowHandler(t *testing.T) {
	d := NewDispatcher()
	defer d.Stop(context.Background())

	var handlers = make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	h := handlers[clockapi.CmdNow]
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

type testTimeReference struct {
	fixedTime time.Time
}

func (m *testTimeReference) Now() time.Time       { return m.fixedTime }
func (m *testTimeReference) StartTime() time.Time { return m.fixedTime }

func TestNowHandlerWithTimeReference(t *testing.T) {
	d := NewDispatcher()
	defer d.Stop(context.Background())

	var handlers = make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	h := handlers[clockapi.CmdNow]
	fixedTime := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	workflow.WithTimeReference(ctx, &testTimeReference{fixedTime: fixedTime})

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

func TestTimerResetHandler(t *testing.T) {
	d := NewDispatcher()
	defer d.Stop(context.Background())

	var handlers = make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	startHandler := handlers[clockapi.CmdTimerStart]
	resetHandler := handlers[clockapi.CmdTimerReset]

	var timerID uint64
	startHandler.Handle(context.Background(), clockapi.TimerStartCmd{Duration: time.Hour}, func(data any) {
		timerID = data.(uint64)
	})

	var emitted any
	err := resetHandler.Handle(context.Background(), clockapi.TimerResetCmd{TimerID: timerID, Duration: 10 * time.Millisecond}, func(data any) {
		emitted = data
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	wasActive, ok := emitted.(bool)
	if !ok {
		t.Fatalf("expected bool, got %T", emitted)
	}
	if !wasActive {
		t.Error("expected timer to be active")
	}
}

func TestTimerResetHandlerZeroDuration(t *testing.T) {
	d := NewDispatcher()
	defer d.Stop(context.Background())

	var handlers = make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	startHandler := handlers[clockapi.CmdTimerStart]
	resetHandler := handlers[clockapi.CmdTimerReset]

	var timerID uint64
	startHandler.Handle(context.Background(), clockapi.TimerStartCmd{Duration: time.Hour}, func(data any) {
		timerID = data.(uint64)
	})

	var emitted bool
	err := resetHandler.Handle(context.Background(), clockapi.TimerResetCmd{TimerID: timerID, Duration: 0}, func(data any) {
		emitted = true
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if emitted {
		t.Error("expected no emit for zero duration")
	}
}

func TestTimerResetHandlerNotFound(t *testing.T) {
	d := NewDispatcher()
	defer d.Stop(context.Background())

	var handlers = make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	resetHandler := handlers[clockapi.CmdTimerReset]

	var emitted bool
	err := resetHandler.Handle(context.Background(), clockapi.TimerResetCmd{TimerID: 999, Duration: time.Second}, func(data any) {
		emitted = true
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if emitted {
		t.Error("expected no emit for non-existent timer")
	}
}

func TestDispatcher_RegisterAll(t *testing.T) {
	d := NewDispatcher()
	handlers := make(map[dispatcher.CommandID]bool)

	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = true
	})

	expected := []dispatcher.CommandID{
		clockapi.CmdSleep,
		clockapi.CmdNow,
		clockapi.CmdAfter,
		clockapi.CmdTickerStart,
		clockapi.CmdTickerNext,
		clockapi.CmdTickerStop,
		clockapi.CmdTimerStart,
		clockapi.CmdTimerWait,
		clockapi.CmdTimerStop,
		clockapi.CmdTimerReset,
	}

	for _, id := range expected {
		if !handlers[id] {
			t.Errorf("handler for command %d not registered", id)
		}
	}
}
