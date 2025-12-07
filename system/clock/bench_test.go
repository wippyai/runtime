package clock

import (
	"context"
	"sync"
	"testing"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
)

// Wheel Timer Benchmarks

func BenchmarkWheelTimerStart(b *testing.B) {
	r := NewWheelTimerRegistry()
	defer r.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Start(time.Hour)
	}
}

func BenchmarkWheelTimerStartStop(b *testing.B) {
	r := NewWheelTimerRegistry()
	defer r.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := r.Start(time.Hour)
		r.Stop(id)
	}
}

func BenchmarkWheelTimerStartStopParallel(b *testing.B) {
	r := NewWheelTimerRegistry()
	defer r.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			id := r.Start(time.Hour)
			r.Stop(id)
		}
	})
}

func BenchmarkWheelTimerReset(b *testing.B) {
	r := NewWheelTimerRegistry()
	defer r.Close()

	id := r.Start(time.Hour)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Reset(id, time.Hour)
	}
}

func BenchmarkWheelTimerWaitShort(b *testing.B) {
	r := NewWheelTimerRegistry()
	defer r.Close()

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := r.Start(time.Microsecond)
		r.Wait(ctx, id)
	}
}

// Ticker Benchmarks

func BenchmarkTickerStart(b *testing.B) {
	r := NewTickerRegistry()
	defer r.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Start(time.Hour)
	}
}

func BenchmarkTickerStartStop(b *testing.B) {
	r := NewTickerRegistry()
	defer r.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := r.Start(time.Hour)
		r.Stop(id)
	}
}

func BenchmarkTickerStartStopParallel(b *testing.B) {
	r := NewTickerRegistry()
	defer r.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			id := r.Start(time.Hour)
			r.Stop(id)
		}
	})
}

func BenchmarkTickerNext(b *testing.B) {
	r := NewTickerRegistry()
	defer r.Close()

	id := r.Start(time.Nanosecond)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Next(ctx, id)
	}
}

// Dispatcher Benchmarks

func BenchmarkDispatcherSleep(b *testing.B) {
	d := NewDispatcher()
	defer d.Stop(context.Background())

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})
	h := handlers[clockapi.Sleep]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		done := make(chan struct{})
		h.Handle(context.Background(), clockapi.SleepCmd{Duration: time.Microsecond}, 0, &testReceiver{fn: func(_ any, _ error) {
			close(done)
		}})
		<-done
	}
}

func BenchmarkDispatcherSleepZero(b *testing.B) {
	d := NewDispatcher()
	defer d.Stop(context.Background())

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})
	h := handlers[clockapi.Sleep]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Handle(context.Background(), clockapi.SleepCmd{Duration: 0}, 0, &testReceiver{fn: func(_ any, _ error) {}})
	}
}

func BenchmarkDispatcherTimerStartWait(b *testing.B) {
	d := NewDispatcher()
	defer d.Stop(context.Background())

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})
	startH := handlers[clockapi.TimerStart]
	waitH := handlers[clockapi.TimerWait]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var timerID uint64
		startH.Handle(context.Background(), clockapi.TimerStartCmd{Duration: time.Microsecond}, 0, &testReceiver{fn: func(data any, _ error) {
			timerID = data.(clockapi.TimerStartResult).ID
		}})
		done := make(chan struct{})
		waitH.Handle(context.Background(), clockapi.TimerWaitCmd{TimerID: timerID}, 0, &testReceiver{fn: func(_ any, _ error) {
			close(done)
		}})
		<-done
	}
}

func BenchmarkDispatcherTimerStartStop(b *testing.B) {
	d := NewDispatcher()
	defer d.Stop(context.Background())

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})
	startH := handlers[clockapi.TimerStart]
	stopH := handlers[clockapi.TimerStop]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var timerID uint64
		startH.Handle(context.Background(), clockapi.TimerStartCmd{Duration: time.Hour}, 0, &testReceiver{fn: func(data any, _ error) {
			timerID = data.(clockapi.TimerStartResult).ID
		}})
		stopH.Handle(context.Background(), clockapi.TimerStopCmd{TimerID: timerID}, 0, &testReceiver{fn: func(_ any, _ error) {}})
	}
}

func BenchmarkDispatcherTickerStartNext(b *testing.B) {
	d := NewDispatcher()
	defer d.Stop(context.Background())

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})
	startH := handlers[clockapi.TickerStart]
	nextH := handlers[clockapi.TickerNext]
	stopH := handlers[clockapi.TickerStop]

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	var tickerID uint64
	startH.Handle(ctx, clockapi.TickerStartCmd{Duration: time.Nanosecond}, 0, &testReceiver{fn: func(data any, _ error) {
		tickerID = data.(clockapi.TickerStartResult).ID
	}})
	defer stopH.Handle(ctx, clockapi.TickerStopCmd{TickerID: tickerID}, 0, &testReceiver{fn: func(_ any, _ error) {}})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		done := make(chan struct{})
		nextH.Handle(ctx, clockapi.TickerNextCmd{TickerID: tickerID}, 0, &testReceiver{fn: func(_ any, _ error) {
			close(done)
		}})
		<-done
	}
}

// Concurrency Benchmarks

func BenchmarkWheelTimer1000Concurrent(b *testing.B) {
	r := NewWheelTimerRegistry()
	defer r.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		for j := 0; j < 1000; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				id := r.Start(time.Hour)
				r.Stop(id)
			}()
		}
		wg.Wait()
	}
}

func BenchmarkTicker1000Concurrent(b *testing.B) {
	r := NewTickerRegistry()
	defer r.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		for j := 0; j < 1000; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				id := r.Start(time.Hour)
				r.Stop(id)
			}()
		}
		wg.Wait()
	}
}

// Memory Allocation Benchmarks

func BenchmarkWheelTimerAllocations(b *testing.B) {
	r := NewWheelTimerRegistry()
	defer r.Close()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := r.Start(time.Millisecond)
		r.Stop(id)
	}
}

func BenchmarkTickerAllocations(b *testing.B) {
	r := NewTickerRegistry()
	defer r.Close()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := r.Start(time.Hour)
		r.Stop(id)
	}
}
