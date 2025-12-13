package clock

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

// Timer benchmarks

func BenchmarkTimerRegistry_StartStop(b *testing.B) {
	r := newTimerRegistry()
	defer r.close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := r.startWithCallback(time.Hour, nil)
		_, _ = r.stop(id)
	}
}

func BenchmarkTimerRegistry_StartStopParallel(b *testing.B) {
	r := newTimerRegistry()
	defer r.close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			id := r.startWithCallback(time.Hour, nil)
			_, _ = r.stop(id)
		}
	})
}

func BenchmarkTimerRegistry_Reset(b *testing.B) {
	r := newTimerRegistry()
	defer r.close()

	id := r.startWithCallback(time.Hour, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = r.reset(id, time.Hour)
	}
}

func BenchmarkTimerRegistry_GetShard(b *testing.B) {
	r := newTimerRegistry()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.getShard(uint64(i))
	}
}

// Ticker benchmarks

type benchNode struct{}

func (n *benchNode) Send(_ *relay.Package) error                       { return nil }
func (n *benchNode) ID() pid.NodeID                                    { return "" }
func (n *benchNode) RegisterHost(_ pid.HostID, _ relay.Receiver) error { return nil }
func (n *benchNode) UnregisterHost(_ pid.HostID)                       {}
func (n *benchNode) GetHost(_ pid.HostID) (relay.Receiver, bool)       { return nil, false }
func (n *benchNode) Attach(_ pid.PID, _ chan *relay.Package) (context.CancelFunc, error) {
	return func() {}, nil
}
func (n *benchNode) Detach(_ pid.PID) {}

func BenchmarkTickerRegistry_StartStop(b *testing.B) {
	r := newTickerRegistry()
	defer r.close()

	ctx := context.Background()
	node := &benchNode{}
	testPID := pid.PID{Node: "bench", Host: "bench", UniqID: "bench"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := r.start(ctx, time.Hour, testPID, "topic", node)
		_ = r.stop(id)
	}
}

func BenchmarkTickerRegistry_StartStopParallel(b *testing.B) {
	r := newTickerRegistry()
	defer r.close()

	ctx := context.Background()
	node := &benchNode{}
	testPID := pid.PID{Node: "bench", Host: "bench", UniqID: "bench"}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			id := r.start(ctx, time.Hour, testPID, "topic", node)
			_ = r.stop(id)
		}
	})
}

func BenchmarkTickerRegistry_GetShard(b *testing.B) {
	r := newTickerRegistry()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.getShard(uint64(i))
	}
}

// Concurrent contention benchmark

func BenchmarkTimerRegistry_HighContention(b *testing.B) {
	r := newTimerRegistry()
	defer r.close()

	// Pre-create timers
	ids := make([]uint64, 1000)
	for i := range ids {
		ids[i] = r.startWithCallback(time.Hour, nil)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			id := ids[i%len(ids)]
			_, _ = r.reset(id, time.Hour)
			i++
		}
	})
}

func BenchmarkTickerRegistry_Count(b *testing.B) {
	r := newTickerRegistry()
	defer r.close()

	ctx := context.Background()
	node := &benchNode{}
	testPID := pid.PID{Node: "bench", Host: "bench", UniqID: "bench"}

	// Create 100 tickers
	for i := 0; i < 100; i++ {
		r.start(ctx, time.Hour, testPID, "topic", node)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.count()
	}
}

func BenchmarkTimerRegistry_Count(b *testing.B) {
	r := newTimerRegistry()
	defer r.close()

	// Create 100 timers
	for i := 0; i < 100; i++ {
		r.startWithCallback(time.Hour, nil)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.count()
	}
}

// Memory allocation benchmarks

func BenchmarkTimerRegistry_Allocs(b *testing.B) {
	r := newTimerRegistry()
	defer r.close()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := r.startWithCallback(time.Hour, func() {})
		_, _ = r.stop(id)
	}
}

func BenchmarkTickerRegistry_Allocs(b *testing.B) {
	r := newTickerRegistry()
	defer r.close()

	ctx := context.Background()
	node := &benchNode{}
	testPID := pid.PID{Node: "bench", Host: "bench", UniqID: "bench"}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := r.start(ctx, time.Hour, testPID, "topic", node)
		_ = r.stop(id)
	}
}

// Sharding effectiveness benchmark

func BenchmarkTimerRegistry_ShardDistribution(b *testing.B) {
	r := newTimerRegistry()
	defer r.close()

	var wg sync.WaitGroup
	workers := 64

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wg.Add(workers)
		for w := 0; w < workers; w++ {
			go func() {
				defer wg.Done()
				id := r.startWithCallback(time.Hour, nil)
				_, _ = r.stop(id)
			}()
		}
		wg.Wait()
	}
}
