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

// After Benchmarks

func BenchmarkAfterCreate(b *testing.B) {
	r := NewAfterRegistry()
	defer r.Close()

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Create(ctx, time.Hour)
	}
}

func BenchmarkAfterCreateParallel(b *testing.B) {
	r := NewAfterRegistry()
	defer r.Close()

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			r.Create(ctx, time.Hour)
		}
	})
}

// Dispatcher Benchmarks

func BenchmarkDispatcherSleep(b *testing.B) {
	d := NewDispatcher()
	defer d.Stop(context.Background())

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})
	h := handlers[clockapi.CmdSleep]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		done := make(chan struct{})
		h.Handle(context.Background(), clockapi.SleepCmd{Duration: time.Microsecond}, emitFunc(func(_ any, _ error) {
			close(done)
		}))
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
	h := handlers[clockapi.CmdSleep]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Handle(context.Background(), clockapi.SleepCmd{Duration: 0}, emitFunc(func(_ any, _ error) {}))
	}
}

func BenchmarkDispatcherNow(b *testing.B) {
	d := NewDispatcher()
	defer d.Stop(context.Background())

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})
	h := handlers[clockapi.CmdNow]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Handle(context.Background(), clockapi.NowCmd{}, emitFunc(func(_ any, _ error) {}))
	}
}

func BenchmarkDispatcherTimerStartWait(b *testing.B) {
	d := NewDispatcher()
	defer d.Stop(context.Background())

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})
	startH := handlers[clockapi.CmdTimerStart]
	waitH := handlers[clockapi.CmdTimerWait]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var id uint64
		startH.Handle(context.Background(), clockapi.TimerStartCmd{Duration: time.Microsecond}, emitFunc(func(data any, _ error) {
			id = data.(uint64)
		}))
		done := make(chan struct{})
		waitH.Handle(context.Background(), clockapi.TimerWaitCmd{TimerID: id}, emitFunc(func(_ any, _ error) {
			close(done)
		}))
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
	startH := handlers[clockapi.CmdTimerStart]
	stopH := handlers[clockapi.CmdTimerStop]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var id uint64
		startH.Handle(context.Background(), clockapi.TimerStartCmd{Duration: time.Hour}, emitFunc(func(data any, _ error) {
			id = data.(uint64)
		}))
		stopH.Handle(context.Background(), clockapi.TimerStopCmd{TimerID: id}, emitFunc(func(_ any, _ error) {}))
	}
}

func BenchmarkDispatcherTickerStartNext(b *testing.B) {
	d := NewDispatcher()
	defer d.Stop(context.Background())

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})
	startH := handlers[clockapi.CmdTickerStart]
	nextH := handlers[clockapi.CmdTickerNext]
	stopH := handlers[clockapi.CmdTickerStop]

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	var tickerID uint64
	startH.Handle(ctx, clockapi.TickerStartCmd{Duration: time.Nanosecond}, emitFunc(func(data any, _ error) {
		tickerID = data.(uint64)
	}))
	defer stopH.Handle(ctx, clockapi.TickerStopCmd{TickerID: tickerID}, emitFunc(func(_ any, _ error) {}))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		done := make(chan struct{})
		nextH.Handle(ctx, clockapi.TickerNextCmd{TickerID: tickerID}, emitFunc(func(_ any, _ error) {
			close(done)
		}))
		<-done
	}
}

func BenchmarkDispatcherAfter(b *testing.B) {
	d := NewDispatcher()
	defer d.Stop(context.Background())

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})
	h := handlers[clockapi.CmdAfter]

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Handle(ctx, clockapi.AfterCmd{Duration: time.Hour}, emitFunc(func(_ any, _ error) {}))
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

func BenchmarkAfterAllocations(b *testing.B) {
	r := NewAfterRegistry()
	defer r.Close()

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Create(ctx, time.Hour)
	}
}
