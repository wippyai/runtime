package supervisor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// fastService is a minimal service for benchmarking
type fastService struct {
	ch chan any
}

func newFastService() *fastService {
	return &fastService{ch: make(chan any, 1)}
}

func (s *fastService) Start(_ context.Context) (<-chan any, error) {
	return s.ch, nil
}

func (s *fastService) Stop(_ context.Context) error {
	close(s.ch)
	s.ch = make(chan any, 1)
	return nil
}

// State Benchmarks - these are safe and don't spawn goroutines

func BenchmarkInternalStateUpdate(b *testing.B) {
	state := newServiceState()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state.updateState(supervisor.StatusRunning, "details")
	}
}

func BenchmarkInternalStateUpdateParallel(b *testing.B) {
	state := newServiceState()

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			state.updateState(supervisor.StatusRunning, "details")
		}
	})
}

func BenchmarkInternalStateSnapshot(b *testing.B) {
	state := newServiceState()
	state.updateState(supervisor.StatusRunning, "test details")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = state.getSnapshot()
	}
}

func BenchmarkInternalStateSnapshotParallel(b *testing.B) {
	state := newServiceState()
	state.updateState(supervisor.StatusRunning, "test details")

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = state.getSnapshot()
		}
	})
}

func BenchmarkStateAllocations(b *testing.B) {
	state := newServiceState()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state.updateState(supervisor.StatusRunning, nil)
		_ = state.publicState()
	}
}

// Transaction Benchmarks - safe, no goroutines

func BenchmarkTransactionRegister(b *testing.B) {
	logger := zap.NewNop()
	tx := newTransactionHelper(logger)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx.begin()
		for j := 0; j < 10; j++ {
			_ = tx.registerService("service", &supervisor.Entry{})
		}
		tx.reset()
	}
}

func BenchmarkTransactionCommit(b *testing.B) {
	logger := zap.NewNop()
	tx := newTransactionHelper(logger)

	removeFn := func(_ string) error { return nil }
	registerFn := func(_ string, _ *supervisor.Entry) error { return nil }

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx.begin()
		for j := 0; j < 5; j++ {
			_ = tx.registerService("svc"+string(rune('0'+j)), &supervisor.Entry{})
		}
		for j := 0; j < 3; j++ {
			_ = tx.removeService("old" + string(rune('0'+j)))
		}
		_ = tx.commit(removeFn, registerFn)
	}
}

// Sequencer Benchmarks

type benchControllable struct{}

func (c *benchControllable) Start() error { return nil }
func (c *benchControllable) Stop() error  { return nil }

func BenchmarkSequencerLinearChain(b *testing.B) {
	logger := zap.NewNop()
	seq := NewSequencer(logger)
	ctx := context.Background()

	sizes := []int{5, 10, 20}
	for _, size := range sizes {
		name := ""
		if size >= 10 {
			name = string(rune('0' + size/10))
		}
		name += string(rune('0' + size%10))

		b.Run(name+"_services", func(b *testing.B) {
			ops := make([]Operation, size)
			for i := 0; i < size; i++ {
				deps := []string{}
				if i > 0 {
					deps = []string{"svc" + string(rune('0'+i-1))}
				}
				ops[i] = Operation{
					Type:         OperationStart,
					ID:           "svc" + string(rune('0'+i)),
					Controller:   &benchControllable{},
					Dependencies: deps,
				}
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = seq.Transition(ctx, ops...)
			}
		})
	}
}

func BenchmarkSequencerParallelServices(b *testing.B) {
	logger := zap.NewNop()
	seq := NewSequencer(logger)
	ctx := context.Background()

	sizes := []int{10, 20, 50}
	for _, size := range sizes {
		name := ""
		if size >= 10 {
			name = string(rune('0' + size/10))
		}
		name += string(rune('0' + size%10))

		b.Run(name+"_parallel", func(b *testing.B) {
			ops := make([]Operation, size)
			for i := 0; i < size; i++ {
				ops[i] = Operation{
					Type:         OperationStart,
					ID:           "svc" + string(rune('A'+i%26)) + string(rune('0'+i/26)),
					Controller:   &benchControllable{},
					Dependencies: nil,
				}
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = seq.Transition(ctx, ops...)
			}
		})
	}
}

func BenchmarkSequencerMixedOperations(b *testing.B) {
	logger := zap.NewNop()
	seq := NewSequencer(logger)
	ctx := context.Background()

	ops := []Operation{
		{Type: OperationStop, ID: "stop1", Controller: &benchControllable{}},
		{Type: OperationStop, ID: "stop2", Controller: &benchControllable{}},
		{Type: OperationStart, ID: "start1", Controller: &benchControllable{}},
		{Type: OperationStart, ID: "start2", Controller: &benchControllable{}, Dependencies: []string{"start1"}},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = seq.Transition(ctx, ops...)
	}
}

// Controller Benchmarks - use a single controller with proper cleanup

func BenchmarkControllerStateRead(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := newFastService()
	ctrl := NewController(ctx, svc, supervisor.LifecycleConfig{
		StartTimeout: time.Second,
		StopTimeout:  time.Second,
	}, nil)
	_ = ctrl.Start()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ctrl.State()
	}
	b.StopTimer()

	_ = ctrl.Stop()
}

func BenchmarkControllerStateReadParallel(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := newFastService()
	ctrl := NewController(ctx, svc, supervisor.LifecycleConfig{
		StartTimeout: time.Second,
		StopTimeout:  time.Second,
	}, nil)
	_ = ctrl.Start()

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = ctrl.State()
		}
	})
	b.StopTimer()

	_ = ctrl.Stop()
}

func BenchmarkControllerStartStop(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	config := supervisor.LifecycleConfig{
		StartTimeout: time.Second,
		StopTimeout:  time.Second,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		svc := newFastService()
		ctrl := NewController(ctx, svc, config, nil)
		_ = ctrl.Start()
		_ = ctrl.Stop()
	}
}

// Supervisor Benchmarks

func BenchmarkSupervisorGetState(b *testing.B) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	sup := NewSupervisor(bus, logger)

	ctx := context.Background()
	_ = sup.Start(ctx)

	// Register a service directly
	sup.mu.Lock()
	svc := newFastService()
	sup.controllers["test-service"] = NewController(ctx, svc, supervisor.LifecycleConfig{
		StartTimeout: time.Second,
		StopTimeout:  time.Second,
	}, nil)
	sup.mu.Unlock()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = sup.GetState("test-service")
	}
	b.StopTimer()

	_ = sup.Stop()
}

func BenchmarkSupervisorGetStateParallel(b *testing.B) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	sup := NewSupervisor(bus, logger)

	ctx := context.Background()
	_ = sup.Start(ctx)

	sup.mu.Lock()
	svc := newFastService()
	sup.controllers["test-service"] = NewController(ctx, svc, supervisor.LifecycleConfig{
		StartTimeout: time.Second,
		StopTimeout:  time.Second,
	}, nil)
	sup.mu.Unlock()

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = sup.GetState("test-service")
		}
	})
	b.StopTimer()

	_ = sup.Stop()
}

func BenchmarkSupervisorGetAllStates(b *testing.B) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	sup := NewSupervisor(bus, logger)

	ctx := context.Background()
	_ = sup.Start(ctx)

	sup.mu.Lock()
	for i := 0; i < 20; i++ {
		svc := newFastService()
		id := "service-" + string(rune('A'+i%26)) + string(rune('0'+i/26))
		sup.controllers[id] = NewController(ctx, svc, supervisor.LifecycleConfig{
			StartTimeout: time.Second,
			StopTimeout:  time.Second,
		}, nil)
	}
	sup.mu.Unlock()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sup.GetAllStates()
	}
	b.StopTimer()

	_ = sup.Stop()
}

// Concurrent Operations Benchmark

func BenchmarkConcurrentControllerOperations(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	config := supervisor.LifecycleConfig{
		StartTimeout: time.Second,
		StopTimeout:  time.Second,
		RetryPolicy: supervisor.RetryPolicy{
			MaxAttempts:  1,
			InitialDelay: time.Millisecond,
		},
	}

	// Pre-create controllers to avoid goroutine explosion
	const poolSize = 100
	controllers := make([]*Controller, poolSize)
	services := make([]*fastService, poolSize)
	for i := 0; i < poolSize; i++ {
		services[i] = newFastService()
		controllers[i] = NewController(ctx, services[i], config, nil)
	}

	var mu sync.Mutex
	idx := 0

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			mu.Lock()
			i := idx % poolSize
			idx++
			ctrl := controllers[i]
			mu.Unlock()

			_ = ctrl.Start()
			_ = ctrl.State()
			_ = ctrl.Stop()
		}
	})

	b.StopTimer()
	cancel()
}
