package clock

import (
	"context"
	"sync"
	"testing"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
	"github.com/wippyai/runtime/api/dispatcher"
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

func getTickerHandlers(t *testing.T) (start, next, stop dispatcher.Handler, cleanup func()) {
	d := NewDispatcher()
	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})
	return handlers[clockapi.TickerStart],
		handlers[clockapi.TickerNext],
		handlers[clockapi.TickerStop],
		func() { d.Stop(context.Background()) }
}

func TestTickerStartHandler(t *testing.T) {
	ctx := context.Background()
	startH, _, _, cleanup := getTickerHandlers(t)
	defer cleanup()

	var emitted any
	err := startH.Handle(ctx, clockapi.TickerStartCmd{Duration: 10 * time.Millisecond}, 0, &testReceiver{fn: func(data any, _ error) {
		emitted = data
	}})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, ok := emitted.(clockapi.TickerStartResult)
	if !ok {
		t.Fatalf("expected TickerStartResult, got %T", emitted)
	}
	if result.ID != 1 {
		t.Errorf("expected ID 1, got %d", result.ID)
	}
	if result.Stop == nil {
		t.Error("expected Stop callback to be set")
	}
}

func TestTickerNextHandler(t *testing.T) {
	ctx := context.Background()
	startH, nextH, _, cleanup := getTickerHandlers(t)
	defer cleanup()

	var tickerID uint64
	startH.Handle(ctx, clockapi.TickerStartCmd{Duration: 5 * time.Millisecond}, 0, &testReceiver{fn: func(data any, _ error) {
		tickerID = data.(clockapi.TickerStartResult).ID
	}})

	var emitted any
	done := make(chan struct{})
	err := nextH.Handle(ctx, clockapi.TickerNextCmd{TickerID: tickerID}, 0, &testReceiver{fn: func(data any, _ error) {
		emitted = data
		close(done)
	}})
	<-done

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
}

func TestTickerStopHandler(t *testing.T) {
	ctx := context.Background()
	startH, _, stopH, cleanup := getTickerHandlers(t)
	defer cleanup()

	var tickerID uint64
	startH.Handle(ctx, clockapi.TickerStartCmd{Duration: 10 * time.Millisecond}, 0, &testReceiver{fn: func(data any, _ error) {
		tickerID = data.(clockapi.TickerStartResult).ID
	}})

	var emitted bool
	err := stopH.Handle(ctx, clockapi.TickerStopCmd{TickerID: tickerID}, 0, &testReceiver{fn: func(data any, _ error) {
		emitted = true
	}})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !emitted {
		t.Error("expected emit on stop")
	}
}

func TestTickerFullCycle(t *testing.T) {
	ctx := context.Background()
	startH, nextH, stopH, cleanup := getTickerHandlers(t)
	defer cleanup()

	var tickerID uint64
	startH.Handle(ctx, clockapi.TickerStartCmd{Duration: 2 * time.Millisecond}, 0, &testReceiver{fn: func(data any, _ error) {
		tickerID = data.(clockapi.TickerStartResult).ID
	}})

	ticks := make([]int64, 0, 3)
	for i := 0; i < 3; i++ {
		var tick int64
		done := make(chan struct{})
		err := nextH.Handle(ctx, clockapi.TickerNextCmd{TickerID: tickerID}, 0, &testReceiver{fn: func(data any, _ error) {
			tick = data.(int64)
			close(done)
		}})
		<-done
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

	err := stopH.Handle(ctx, clockapi.TickerStopCmd{TickerID: tickerID}, 0, &testReceiver{fn: func(data any, _ error) {}})
	if err != nil {
		t.Fatalf("stop error: %v", err)
	}
}

func TestTickerStartHandlerInvalidDuration(t *testing.T) {
	ctx := context.Background()
	startH, _, _, cleanup := getTickerHandlers(t)
	defer cleanup()

	var emitted bool
	err := startH.Handle(ctx, clockapi.TickerStartCmd{Duration: 0}, 0, &testReceiver{fn: func(data any, _ error) {
		emitted = true
	}})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if emitted {
		t.Error("expected no emit for zero duration")
	}

	err = startH.Handle(ctx, clockapi.TickerStartCmd{Duration: -time.Second}, 0, &testReceiver{fn: func(data any, _ error) {
		emitted = true
	}})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if emitted {
		t.Error("expected no emit for negative duration")
	}
}

func TestTickerRegistryScalability(t *testing.T) {
	const numTickers = 10000

	registry := NewTickerRegistry()
	defer registry.Close()

	ids := make([]uint64, numTickers)
	start := time.Now()
	for i := 0; i < numTickers; i++ {
		ids[i] = registry.Start(time.Hour)
	}
	createTime := time.Since(start)
	t.Logf("Created %d tickers in %v", numTickers, createTime)

	if count := registry.Count(); count != numTickers {
		t.Errorf("expected %d tickers, got %d", numTickers, count)
	}

	start = time.Now()
	var wg sync.WaitGroup
	for i := 0; i < numTickers; i++ {
		wg.Add(1)
		go func(id uint64) {
			defer wg.Done()
			registry.Stop(id)
		}(ids[i])
	}
	wg.Wait()
	stopTime := time.Since(start)
	t.Logf("Stopped %d tickers in %v", numTickers, stopTime)

	if remaining := registry.Count(); remaining != 0 {
		t.Errorf("expected 0 tickers after stop, got %d", remaining)
	}
}

func TestTickerRegistryConcurrentOperations(t *testing.T) {
	const goroutines = 100
	const opsPerGoroutine = 100

	registry := NewTickerRegistry()
	defer registry.Close()

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids := make([]uint64, 0, opsPerGoroutine)

			for i := 0; i < opsPerGoroutine; i++ {
				id := registry.Start(time.Hour)
				ids = append(ids, id)
			}

			for _, id := range ids {
				registry.Stop(id)
			}
		}()
	}

	wg.Wait()

	if remaining := registry.Count(); remaining != 0 {
		t.Errorf("expected 0 tickers after concurrent ops, got %d", remaining)
	}
}
