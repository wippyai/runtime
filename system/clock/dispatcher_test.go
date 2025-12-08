package clock

import (
	"context"
	"errors"
	"testing"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
	"github.com/wippyai/runtime/api/dispatcher"
)

type testReceiver struct {
	fn func(data any, err error)
}

func (r *testReceiver) CompleteYield(_ uint64, data any, err error) {
	r.fn(data, err)
}

func TestSleepHandler(t *testing.T) {
	d := NewDispatcher()
	defer func() { _ = d.Stop(context.Background()) }()

	var handlers = make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	h := handlers[clockapi.Sleep]
	ctx := context.Background()

	done := make(chan struct{})
	start := time.Now()
	err := h.Handle(ctx, clockapi.SleepCmd{Duration: 10 * time.Millisecond}, 0, &testReceiver{fn: func(_ any, _ error) {
		close(done)
	}})

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
	defer func() { _ = d.Stop(context.Background()) }()

	var handlers = make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	h := handlers[clockapi.Sleep]
	ctx := context.Background()

	var emitted bool
	err := h.Handle(ctx, clockapi.SleepCmd{Duration: 0}, 0, &testReceiver{fn: func(_ any, _ error) {
		emitted = true
	}})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !emitted {
		t.Error("expected emit for zero duration")
	}
}

func TestTimerStartHandler(t *testing.T) {
	d := NewDispatcher()
	defer func() { _ = d.Stop(context.Background()) }()

	var handlers = make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	h := handlers[clockapi.TimerStart]

	var emitted any
	err := h.Handle(context.Background(), clockapi.TimerStartCmd{Duration: 10 * time.Millisecond}, 0, &testReceiver{fn: func(data any, _ error) {
		emitted = data
	}})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	result, ok := emitted.(clockapi.TimerStartResult)
	if !ok || result.ID == 0 {
		t.Errorf("expected non-zero timer ID, got %v", emitted)
	}
	if result.Stop == nil {
		t.Error("expected Stop callback to be set")
	}
}

func TestTimerStartHandlerZeroDuration(t *testing.T) {
	d := NewDispatcher()
	defer func() { _ = d.Stop(context.Background()) }()

	var handlers = make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	h := handlers[clockapi.TimerStart]

	var emitted bool
	err := h.Handle(context.Background(), clockapi.TimerStartCmd{Duration: 0}, 0, &testReceiver{fn: func(_ any, _ error) {
		emitted = true
	}})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if emitted {
		t.Error("expected no emit for zero duration")
	}
}

func TestTimerWaitHandler(t *testing.T) {
	d := NewDispatcher()
	defer func() { _ = d.Stop(context.Background()) }()

	var handlers = make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	startHandler := handlers[clockapi.TimerStart]
	waitHandler := handlers[clockapi.TimerWait]

	var timerID uint64
	_ = startHandler.Handle(context.Background(), clockapi.TimerStartCmd{Duration: 10 * time.Millisecond}, 0, &testReceiver{fn: func(data any, _ error) {
		timerID = data.(clockapi.TimerStartResult).ID
	}})

	var emitted any
	start := time.Now()
	done := make(chan struct{})
	err := waitHandler.Handle(context.Background(), clockapi.TimerWaitCmd{TimerID: timerID}, 0, &testReceiver{fn: func(data any, _ error) {
		emitted = data
		close(done)
	}})

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
	defer func() { _ = d.Stop(context.Background()) }()

	var handlers = make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	startHandler := handlers[clockapi.TimerStart]
	stopHandler := handlers[clockapi.TimerStop]

	var timerID uint64
	_ = startHandler.Handle(context.Background(), clockapi.TimerStartCmd{Duration: time.Hour}, 0, &testReceiver{fn: func(data any, _ error) {
		timerID = data.(clockapi.TimerStartResult).ID
	}})

	var emitted any
	err := stopHandler.Handle(context.Background(), clockapi.TimerStopCmd{TimerID: timerID}, 0, &testReceiver{fn: func(data any, _ error) {
		emitted = data
	}})

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

func TestTimerResetHandler(t *testing.T) {
	d := NewDispatcher()
	defer func() { _ = d.Stop(context.Background()) }()

	var handlers = make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	startHandler := handlers[clockapi.TimerStart]
	resetHandler := handlers[clockapi.TimerReset]

	var timerID uint64
	_ = startHandler.Handle(context.Background(), clockapi.TimerStartCmd{Duration: time.Hour}, 0, &testReceiver{fn: func(data any, _ error) {
		timerID = data.(clockapi.TimerStartResult).ID
	}})

	var emitted any
	err := resetHandler.Handle(context.Background(), clockapi.TimerResetCmd{TimerID: timerID, Duration: 10 * time.Millisecond}, 0, &testReceiver{fn: func(data any, _ error) {
		emitted = data
	}})

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
	defer func() { _ = d.Stop(context.Background()) }()

	var handlers = make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	startHandler := handlers[clockapi.TimerStart]
	resetHandler := handlers[clockapi.TimerReset]

	var timerID uint64
	_ = startHandler.Handle(context.Background(), clockapi.TimerStartCmd{Duration: time.Hour}, 0, &testReceiver{fn: func(data any, _ error) {
		timerID = data.(clockapi.TimerStartResult).ID
	}})

	var emitted bool
	err := resetHandler.Handle(context.Background(), clockapi.TimerResetCmd{TimerID: timerID, Duration: 0}, 0, &testReceiver{fn: func(_ any, _ error) {
		emitted = true
	}})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if emitted {
		t.Error("expected no emit for zero duration")
	}
}

func TestTimerResetHandlerNotFound(t *testing.T) {
	d := NewDispatcher()
	defer func() { _ = d.Stop(context.Background()) }()

	var handlers = make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	resetHandler := handlers[clockapi.TimerReset]

	var emitErr error
	err := resetHandler.Handle(context.Background(), clockapi.TimerResetCmd{TimerID: 999, Duration: time.Second}, 0, &testReceiver{fn: func(_ any, e error) {
		emitErr = e
	}})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !errors.Is(emitErr, ErrTimerNotFound) {
		t.Errorf("expected ErrTimerNotFound, got %v", emitErr)
	}
}

func TestDispatcher_RegisterAll(t *testing.T) {
	d := NewDispatcher()
	handlers := make(map[dispatcher.CommandID]bool)

	d.RegisterAll(func(id dispatcher.CommandID, _ dispatcher.Handler) {
		handlers[id] = true
	})

	expected := []dispatcher.CommandID{
		clockapi.Sleep,
		clockapi.TickerStart,
		clockapi.TickerNext,
		clockapi.TickerStop,
		clockapi.TimerStart,
		clockapi.TimerWait,
		clockapi.TimerStop,
		clockapi.TimerReset,
	}

	for _, id := range expected {
		if !handlers[id] {
			t.Errorf("handler for command %d not registered", id)
		}
	}
}
