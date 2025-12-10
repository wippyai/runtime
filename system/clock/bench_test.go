package clock

import (
	"context"
	"sync"
	"testing"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/relay"
)

type mockRelayNode struct{}

func (m *mockRelayNode) Send(_ *relay.Package) error                     { return nil }
func (m *mockRelayNode) ID() relay.NodeID                                { return "" }
func (m *mockRelayNode) RegisterHost(_ relay.HostID, _ relay.Host) error { return nil }
func (m *mockRelayNode) UnregisterHost(_ relay.HostID)                   {}
func (m *mockRelayNode) GetHost(_ relay.HostID) (relay.Host, bool)       { return nil, false }
func (m *mockRelayNode) Attach(_ relay.PID, _ chan *relay.Package) (context.CancelFunc, error) {
	return func() {}, nil
}
func (m *mockRelayNode) Detach(_ relay.PID) {}

var mockPID = relay.PID{}

func setupTickerTestContext() context.Context {
	ctx := ctxapi.NewRootContext()
	node := &mockRelayNode{}
	return relay.WithNode(ctx, node)
}

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
		_, _ = r.Stop(id)
	}
}

func BenchmarkWheelTimerStartStopParallel(b *testing.B) {
	r := NewWheelTimerRegistry()
	defer r.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			id := r.Start(time.Hour)
			_, _ = r.Stop(id)
		}
	})
}

func BenchmarkWheelTimerReset(b *testing.B) {
	r := NewWheelTimerRegistry()
	defer r.Close()

	id := r.Start(time.Hour)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = r.Reset(id, time.Hour)
	}
}

func BenchmarkWheelTimerWaitShort(b *testing.B) {
	r := NewWheelTimerRegistry()
	defer r.Close()

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := r.Start(time.Microsecond)
		_, _ = r.Wait(ctx, id)
	}
}

// Ticker Benchmarks

func BenchmarkTickerStartStop(b *testing.B) {
	r := NewTickerRegistry()
	defer r.Close()

	ctx := context.Background()
	node := &mockRelayNode{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := r.Start(ctx, time.Hour, mockPID, "test", node)
		_ = r.Stop(id)
	}
}

func BenchmarkTickerStartStopParallel(b *testing.B) {
	r := NewTickerRegistry()
	defer r.Close()

	ctx := context.Background()
	node := &mockRelayNode{}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			id := r.Start(ctx, time.Hour, mockPID, "test", node)
			_ = r.Stop(id)
		}
	})
}

// Dispatcher Benchmarks

func BenchmarkDispatcherSleep(b *testing.B) {
	d := NewDispatcher()
	defer func() { _ = d.Stop(context.Background()) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})
	h := handlers[clockapi.Sleep]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		done := make(chan struct{})
		_ = h.Handle(context.Background(), clockapi.SleepCmd{Duration: time.Microsecond}, 0, &testReceiver{fn: func(_ any, _ error) {
			close(done)
		}})
		<-done
	}
}

func BenchmarkDispatcherSleepZero(b *testing.B) {
	d := NewDispatcher()
	defer func() { _ = d.Stop(context.Background()) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})
	h := handlers[clockapi.Sleep]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.Handle(context.Background(), clockapi.SleepCmd{Duration: 0}, 0, &testReceiver{fn: func(_ any, _ error) {}})
	}
}

func BenchmarkDispatcherTimerStartWait(b *testing.B) {
	d := NewDispatcher()
	defer func() { _ = d.Stop(context.Background()) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})
	startH := handlers[clockapi.TimerStart]
	waitH := handlers[clockapi.TimerWait]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var timerID uint64
		_ = startH.Handle(context.Background(), clockapi.TimerStartCmd{Duration: time.Microsecond}, 0, &testReceiver{fn: func(data any, _ error) {
			timerID = data.(clockapi.TimerStartResult).ID
		}})
		done := make(chan struct{})
		_ = waitH.Handle(context.Background(), clockapi.TimerWaitCmd{TimerID: timerID}, 0, &testReceiver{fn: func(_ any, _ error) {
			close(done)
		}})
		<-done
	}
}

func BenchmarkDispatcherTimerStartStop(b *testing.B) {
	d := NewDispatcher()
	defer func() { _ = d.Stop(context.Background()) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})
	startH := handlers[clockapi.TimerStart]
	stopH := handlers[clockapi.TimerStop]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var timerID uint64
		_ = startH.Handle(context.Background(), clockapi.TimerStartCmd{Duration: time.Hour}, 0, &testReceiver{fn: func(data any, _ error) {
			timerID = data.(clockapi.TimerStartResult).ID
		}})
		_ = stopH.Handle(context.Background(), clockapi.TimerStopCmd{TimerID: timerID}, 0, &testReceiver{fn: func(_ any, _ error) {}})
	}
}

func BenchmarkDispatcherTickerStartStop(b *testing.B) {
	d := NewDispatcher()
	defer func() { _ = d.Stop(context.Background()) }()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})
	startH := handlers[clockapi.TickerStart]
	stopH := handlers[clockapi.TickerStop]

	ctx := setupTickerTestContext()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var tickerID uint64
		_ = startH.Handle(ctx, clockapi.TickerStartCmd{Duration: time.Hour, PID: mockPID, Topic: "bench"}, 0, &testReceiver{fn: func(data any, _ error) {
			tickerID = data.(clockapi.TickerStartResult).ID
		}})
		_ = stopH.Handle(ctx, clockapi.TickerStopCmd{TickerID: tickerID}, 0, &testReceiver{fn: func(_ any, _ error) {}})
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
				_, _ = r.Stop(id)
			}()
		}
		wg.Wait()
	}
}

func BenchmarkTicker1000Concurrent(b *testing.B) {
	r := NewTickerRegistry()
	defer r.Close()

	ctx := context.Background()
	node := &mockRelayNode{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		for j := 0; j < 1000; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				id := r.Start(ctx, time.Hour, mockPID, "test", node)
				_ = r.Stop(id)
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
		_, _ = r.Stop(id)
	}
}

func BenchmarkTickerAllocations(b *testing.B) {
	r := NewTickerRegistry()
	defer r.Close()

	ctx := context.Background()
	node := &mockRelayNode{}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := r.Start(ctx, time.Hour, mockPID, "test", node)
		_ = r.Stop(id)
	}
}
