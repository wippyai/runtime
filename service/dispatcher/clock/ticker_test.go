package clock

import (
	"context"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	clockapi "github.com/wippyai/runtime/api/dispatcher/clock"
)

func TestTickerRegistry(t *testing.T) {
	r := NewTickerRegistry()
	defer r.Close()

	id := r.Start(10 * time.Millisecond)
	if id != 1 {
		t.Errorf("expected first ID to be 1, got %d", id)
	}

	id2 := r.Start(20 * time.Millisecond)
	if id2 != 2 {
		t.Errorf("expected second ID to be 2, got %d", id2)
	}
}

func TestTickerRegistryNext(t *testing.T) {
	r := NewTickerRegistry()
	defer r.Close()

	id := r.Start(5 * time.Millisecond)

	ctx := context.Background()
	tick, err := r.Next(ctx, id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tick.IsZero() {
		t.Error("expected non-zero tick time")
	}
}

func TestTickerRegistryNextNotFound(t *testing.T) {
	r := NewTickerRegistry()
	defer r.Close()

	ctx := context.Background()
	_, err := r.Next(ctx, 999)
	if err != ErrTickerNotFound {
		t.Errorf("expected ErrTickerNotFound, got %v", err)
	}
}

func TestTickerRegistryStop(t *testing.T) {
	r := NewTickerRegistry()

	id := r.Start(10 * time.Millisecond)

	err := r.Stop(id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = r.Stop(id)
	if err != ErrTickerNotFound {
		t.Errorf("expected ErrTickerNotFound on double stop, got %v", err)
	}
}

func TestTickerRegistryClose(t *testing.T) {
	r := NewTickerRegistry()

	r.Start(10 * time.Millisecond)
	r.Start(10 * time.Millisecond)
	r.Start(10 * time.Millisecond)

	r.Close()

	ctx := context.Background()
	_, err := r.Next(ctx, 1)
	if err != ErrTickerNotFound {
		t.Errorf("expected ErrTickerNotFound after close, got %v", err)
	}
}

func TestTickerRegistryContextCancel(t *testing.T) {
	r := NewTickerRegistry()
	defer r.Close()

	id := r.Start(time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	_, err := r.Next(ctx, id)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestTickerStartHandler(t *testing.T) {
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	h := NewTickerStartHandler()

	var emitted any
	err := h.Handle(ctx, clockapi.TickerStartCmd{Duration: 10 * time.Millisecond}, func(data any) {
		emitted = data
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	id, ok := emitted.(uint64)
	if !ok {
		t.Fatalf("expected uint64, got %T", emitted)
	}
	if id != 1 {
		t.Errorf("expected ID 1, got %d", id)
	}

	r := GetTickerRegistry(ctx)
	if r == nil {
		t.Fatal("registry should be created")
	}
	r.Close()
}

func TestTickerNextHandler(t *testing.T) {
	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	startH := NewTickerStartHandler()
	var tickerID uint64
	startH.Handle(ctx, clockapi.TickerStartCmd{Duration: 5 * time.Millisecond}, func(data any) {
		tickerID = data.(uint64)
	})

	nextH := NewTickerNextHandler()
	var emitted any
	err := nextH.Handle(ctx, clockapi.TickerNextCmd{TickerID: tickerID}, func(data any) {
		emitted = data
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nanos, ok := emitted.(int64)
	if !ok {
		t.Fatalf("expected int64, got %T", emitted)
	}
	if nanos <= 0 {
		t.Error("expected positive nanoseconds")
	}

	GetTickerRegistry(ctx).Close()
}

func TestTickerStopHandler(t *testing.T) {
	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	startH := NewTickerStartHandler()
	var tickerID uint64
	startH.Handle(ctx, clockapi.TickerStartCmd{Duration: 10 * time.Millisecond}, func(data any) {
		tickerID = data.(uint64)
	})

	stopH := NewTickerStopHandler()
	err := stopH.Handle(ctx, clockapi.TickerStopCmd{TickerID: tickerID}, func(data any) {})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = stopH.Handle(ctx, clockapi.TickerStopCmd{TickerID: tickerID}, func(data any) {})
	if err != ErrTickerNotFound {
		t.Errorf("expected ErrTickerNotFound on second stop, got %v", err)
	}

	GetTickerRegistry(ctx).Close()
}

func TestTickerFullCycle(t *testing.T) {
	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	startH := NewTickerStartHandler()
	nextH := NewTickerNextHandler()
	stopH := NewTickerStopHandler()

	var tickerID uint64
	startH.Handle(ctx, clockapi.TickerStartCmd{Duration: 2 * time.Millisecond}, func(data any) {
		tickerID = data.(uint64)
	})

	ticks := make([]int64, 0, 3)
	for i := 0; i < 3; i++ {
		var tick int64
		err := nextH.Handle(ctx, clockapi.TickerNextCmd{TickerID: tickerID}, func(data any) {
			tick = data.(int64)
		})
		if err != nil {
			t.Fatalf("tick %d error: %v", i, err)
		}
		ticks = append(ticks, tick)
	}

	for i := 1; i < len(ticks); i++ {
		if ticks[i] <= ticks[i-1] {
			t.Errorf("ticks should be increasing: %v", ticks)
		}
	}

	err := stopH.Handle(ctx, clockapi.TickerStopCmd{TickerID: tickerID}, func(data any) {})
	if err != nil {
		t.Fatalf("stop error: %v", err)
	}

	GetTickerRegistry(ctx).Close()
}

func TestTickerStartHandlerInvalidDuration(t *testing.T) {
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	h := NewTickerStartHandler()

	err := h.Handle(ctx, clockapi.TickerStartCmd{Duration: 0}, func(data any) {})
	if err == nil {
		t.Error("expected error for zero duration")
	}

	err = h.Handle(ctx, clockapi.TickerStartCmd{Duration: -time.Second}, func(data any) {})
	if err == nil {
		t.Error("expected error for negative duration")
	}
}
