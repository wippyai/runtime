package supervisor

import (
	"context"
	"errors"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

func skipUnlessStress(t *testing.T) {
	if os.Getenv("WIPPY_STRESS_TESTS") != "1" {
		t.Skip("Skipping stress test (set WIPPY_STRESS_TESTS=1 to run)")
	}
}

// stressService is a configurable service for stress testing
type stressService struct {
	mu           sync.Mutex
	ch           chan any
	startCount   int32
	stopCount    int32
	startLatency time.Duration
	stopLatency  time.Duration
	failStart    bool
	failStop     bool
}

func newStressService() *stressService {
	return &stressService{
		ch: make(chan any, 10),
	}
}

func (s *stressService) Start(ctx context.Context) (<-chan any, error) {
	atomic.AddInt32(&s.startCount, 1)

	s.mu.Lock()
	failStart := s.failStart
	latency := s.startLatency
	s.mu.Unlock()

	if latency > 0 {
		select {
		case <-time.After(latency):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if failStart {
		return nil, errors.New("intentional start failure")
	}

	s.mu.Lock()
	s.ch = make(chan any, 10)
	ch := s.ch
	s.mu.Unlock()

	return ch, nil
}

func (s *stressService) Stop(ctx context.Context) error {
	atomic.AddInt32(&s.stopCount, 1)

	s.mu.Lock()
	failStop := s.failStop
	latency := s.stopLatency
	ch := s.ch
	s.mu.Unlock()

	if latency > 0 {
		select {
		case <-time.After(latency):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if ch != nil {
		close(ch)
	}

	if failStop {
		return errors.New("intentional stop failure")
	}

	return nil
}

func (s *stressService) StartCount() int32 { return atomic.LoadInt32(&s.startCount) }
func (s *stressService) StopCount() int32  { return atomic.LoadInt32(&s.stopCount) }

// TestStressControllerRapidStartStop tests rapid start/stop cycles
func TestStressControllerRapidStartStop(t *testing.T) {
	skipUnlessStress(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := newStressService()
	ctrl := NewController(ctx, svc, supervisor.LifecycleConfig{
		StartTimeout: time.Second,
		StopTimeout:  time.Second,
		RetryPolicy: supervisor.RetryPolicy{
			MaxAttempts:  1,
			InitialDelay: time.Millisecond,
		},
	}, nil)

	const cycles = 100
	for i := 0; i < cycles; i++ {
		err := ctrl.Start()
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("cycle %d: start failed: %v", i, err)
		}

		state := ctrl.State()
		if state.Status != supervisor.StatusRunning {
			t.Fatalf("cycle %d: expected Running, got %v", i, state.Status)
		}

		err = ctrl.Stop()
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("cycle %d: stop failed: %v", i, err)
		}
	}

	require.Equal(t, int32(cycles), svc.StartCount())
	require.Equal(t, int32(cycles), svc.StopCount())
}

// TestStressConcurrentStateReads tests concurrent state reads during operations
func TestStressConcurrentStateReads(t *testing.T) {
	skipUnlessStress(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := newStressService()
	ctrl := NewController(ctx, svc, supervisor.LifecycleConfig{
		StartTimeout: time.Second,
		StopTimeout:  time.Second,
	}, nil)

	var wg sync.WaitGroup

	// Start concurrent readers
	const readers = 10
	const readsPerReader = 1000
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < readsPerReader; j++ {
				_ = ctrl.State()
				time.Sleep(time.Microsecond)
			}
		}()
	}

	// Perform operations while readers are active
	const cycles = 20
	for i := 0; i < cycles; i++ {
		_ = ctrl.Start()
		time.Sleep(time.Millisecond)
		_ = ctrl.Stop()
	}

	wg.Wait()
}

// TestStressSequencerManyOperations tests sequencer with many operations
func TestStressSequencerManyOperations(t *testing.T) {
	skipUnlessStress(t)

	logger := zap.NewNop()
	seq := newSequencer(logger)
	ctx := context.Background()

	const serviceCount = 100

	// Create a diamond dependency pattern
	ops := make([]operation, serviceCount)
	for i := 0; i < serviceCount; i++ {
		deps := []string{}
		if i > 0 && i < serviceCount-1 {
			deps = []string{"svc-root"}
		}
		if i == serviceCount-1 {
			for j := 1; j < serviceCount-1; j++ {
				deps = append(deps, "svc-"+string(rune('A'+j%26))+string(rune('0'+j/26)))
			}
		}

		var id string
		switch {
		case i == 0:
			id = "svc-root"
		case i == serviceCount-1:
			id = "svc-final"
		default:
			id = "svc-" + string(rune('A'+i%26)) + string(rune('0'+i/26))
		}

		ops[i] = operation{
			kind:         opStart,
			id:           id,
			controller:   &benchControllable{},
			dependencies: deps,
		}
	}

	err := seq.transition(ctx, ops...)
	require.NoError(t, err)
}

// TestStressSequencerConcurrentTransitions tests concurrent transitions
func TestStressSequencerConcurrentTransitions(t *testing.T) {
	skipUnlessStress(t)

	logger := zap.NewNop()

	const goroutines = 10
	const transitionsPerGoroutine = 50

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			seq := newSequencer(logger)
			ctx := context.Background()

			for i := 0; i < transitionsPerGoroutine; i++ {
				ops := []operation{
					{kind: opStart, id: "svc-a", controller: &benchControllable{}},
					{kind: opStart, id: "svc-b", controller: &benchControllable{}, dependencies: []string{"svc-a"}},
					{kind: opStart, id: "svc-c", controller: &benchControllable{}, dependencies: []string{"svc-b"}},
				}
				_ = seq.transition(ctx, ops...)
			}
		}()
	}

	wg.Wait()
}

// TestStressSupervisorManyServices tests supervisor with many services
func TestStressSupervisorManyServices(t *testing.T) {
	skipUnlessStress(t)

	logger := zap.NewNop()
	bus := eventbus.NewBus()
	sup := NewSupervisor(bus, logger)

	ctx := context.Background()
	err := sup.Start(ctx)
	require.NoError(t, err)

	const serviceCount = 50
	services := make(map[string]*stressService)

	// Register many services
	sup.handleEvent(event.Event{System: registry.System, Kind: registry.Begin})
	for i := 0; i < serviceCount; i++ {
		id := "stress-svc-" + string(rune('A'+i%26)) + string(rune('0'+i/26))
		svc := newStressService()
		services[id] = svc

		sup.handleEvent(event.Event{
			System: supervisor.System,
			Kind:   supervisor.ServiceRegister,
			Path:   id,
			Data: &supervisor.Entry{
				Service: svc,
				Config: supervisor.LifecycleConfig{
					AutoStart:    true,
					StartTimeout: 5 * time.Second,
					StopTimeout:  5 * time.Second,
				},
			},
		})
	}
	sup.handleEvent(event.Event{System: registry.System, Kind: registry.Commit})

	// Wait for services to start
	time.Sleep(500 * time.Millisecond)

	// Verify all states
	states := sup.GetAllStates()
	require.Len(t, states, serviceCount)

	runningCount := 0
	for _, state := range states {
		if state.Status == supervisor.StatusRunning {
			runningCount++
		}
	}
	require.Equal(t, serviceCount, runningCount, "all services should be running")

	// Stop supervisor
	err = sup.Stop()
	require.NoError(t, err)
}

// TestStressSupervisorConcurrentEvents tests concurrent event handling
func TestStressSupervisorConcurrentEvents(t *testing.T) {
	skipUnlessStress(t)

	logger := zap.NewNop()
	bus := eventbus.NewBus()
	sup := NewSupervisor(bus, logger)

	ctx := context.Background()
	err := sup.Start(ctx)
	require.NoError(t, err)

	// Register some services first
	sup.handleEvent(event.Event{System: registry.System, Kind: registry.Begin})
	for i := 0; i < 10; i++ {
		id := "concurrent-svc-" + string(rune('0'+i))
		svc := newStressService()
		sup.handleEvent(event.Event{
			System: supervisor.System,
			Kind:   supervisor.ServiceRegister,
			Path:   id,
			Data: &supervisor.Entry{
				Service: svc,
				Config: supervisor.LifecycleConfig{
					AutoStart:    false,
					StartTimeout: time.Second,
					StopTimeout:  time.Second,
				},
			},
		})
	}
	sup.handleEvent(event.Event{System: registry.System, Kind: registry.Commit})

	time.Sleep(100 * time.Millisecond)

	// Send concurrent start/stop events
	const goroutines = 5
	const eventsPerGoroutine = 20

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < eventsPerGoroutine; i++ {
				id := "concurrent-svc-" + string(rune('0'+rand.Intn(10))) //nolint:gosec
				if rand.Float32() < 0.5 {                                 //nolint:gosec
					sup.handleEvent(event.Event{
						System: supervisor.System,
						Kind:   supervisor.ServiceStart,
						Path:   id,
					})
				} else {
					sup.handleEvent(event.Event{
						System: supervisor.System,
						Kind:   supervisor.ServiceStop,
						Path:   id,
					})
				}
				time.Sleep(time.Millisecond)
			}
		}()
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond)

	// Verify supervisor is still functional
	states := sup.GetAllStates()
	require.NotEmpty(t, states)

	err = sup.Stop()
	require.NoError(t, err)
}

// TestStressInternalStateConcurrentAccess tests concurrent state access patterns
func TestStressInternalStateConcurrentAccess(t *testing.T) {
	skipUnlessStress(t)

	state := newInternalState()

	const goroutines = 20
	const opsPerGoroutine = 1000

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				op := i % 6
				switch op {
				case 0:
					state.updateState(supervisor.StatusRunning, "details")
				case 1:
					state.updateDetails("new details")
				case 2:
					_ = state.getSnapshot()
				case 3:
					_ = state.publicState()
				case 4:
					state.incRetryCount()
				case 5:
					state.resetRetryCount()
				}
			}
		}()
	}

	wg.Wait()
}

// TestStressTransactionManyOperations tests transaction handling many operations sequentially
func TestStressTransactionManyOperations(t *testing.T) {
	skipUnlessStress(t)

	logger := zap.NewNop()

	const iterations = 100
	const servicesPerIteration = 100

	for iter := 0; iter < iterations; iter++ {
		tx := newRegTx(logger)
		tx.begin()

		// Register many services
		for i := 0; i < servicesPerIteration; i++ {
			id := "svc-" + string(rune('A'+i%26)) + string(rune('0'+i/26))
			_ = tx.registerService(id, &supervisor.Entry{})
		}

		// Remove some
		for i := 0; i < servicesPerIteration/2; i++ {
			id := "svc-" + string(rune('A'+i%26)) + string(rune('0'+i/26))
			_ = tx.removeService(id)
		}

		// Verify counts
		require.Equal(t, servicesPerIteration/2, len(tx.register))
		require.Equal(t, servicesPerIteration/2, len(tx.remove))

		tx.reset()

		// Verify reset clears everything
		require.Equal(t, 0, len(tx.register))
		require.Equal(t, 0, len(tx.remove))
	}
}

// retryableService allows configuring how many starts should fail
type retryableService struct {
	mu             sync.Mutex
	ch             chan any
	startAttempts  int32
	failUntilCount int32
}

func newRetryableService(failUntil int32) *retryableService {
	return &retryableService{
		ch:             make(chan any, 10),
		failUntilCount: failUntil,
	}
}

func (s *retryableService) Start(_ context.Context) (<-chan any, error) {
	attempt := atomic.AddInt32(&s.startAttempts, 1)
	if attempt < s.failUntilCount {
		return nil, errors.New("temporary failure")
	}

	s.mu.Lock()
	s.ch = make(chan any, 10)
	ch := s.ch
	s.mu.Unlock()

	return ch, nil
}

func (s *retryableService) Stop(_ context.Context) error {
	s.mu.Lock()
	if s.ch != nil {
		close(s.ch)
	}
	s.mu.Unlock()
	return nil
}

func (s *retryableService) StartAttempts() int32 {
	return atomic.LoadInt32(&s.startAttempts)
}

// TestStressControllerRetryUnderLoad tests retry behavior under load
func TestStressControllerRetryUnderLoad(t *testing.T) {
	skipUnlessStress(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	failUntil := int32(3)
	svc := newRetryableService(failUntil)

	stateChanges := make([]supervisor.Status, 0, 20)
	var statesMu sync.Mutex

	ctrl := NewController(ctx, svc, supervisor.LifecycleConfig{
		StartTimeout: time.Second,
		StopTimeout:  time.Second,
		RetryPolicy: supervisor.RetryPolicy{
			MaxAttempts:  5,
			InitialDelay: 10 * time.Millisecond,
		},
	}, func(status supervisor.Status, _ any) {
		statesMu.Lock()
		stateChanges = append(stateChanges, status)
		statesMu.Unlock()
	})

	err := ctrl.Start()
	require.NoError(t, err)

	// Wait for retries to complete
	time.Sleep(200 * time.Millisecond)

	state := ctrl.State()
	require.Equal(t, supervisor.StatusRunning, state.Status)
	require.GreaterOrEqual(t, svc.StartAttempts(), failUntil)

	_ = ctrl.Stop()
}

// TestStressSequencerWithFailures tests sequencer behavior when operations fail
func TestStressSequencerWithFailures(t *testing.T) {
	skipUnlessStress(t)

	logger := zap.NewNop()
	seq := newSequencer(logger)
	ctx := context.Background()

	var failingCtrl = &failingControllable{shouldFail: true}
	var workingCtrl = &benchControllable{}

	ops := []operation{
		{kind: opStop, id: "svc-a", controller: workingCtrl},
		{kind: opStop, id: "svc-b", controller: failingCtrl},
		{kind: opStop, id: "svc-c", controller: workingCtrl},
	}

	err := seq.transition(ctx, ops...)
	require.Error(t, err)
	require.Contains(t, err.Error(), "svc-b")
}

type failingControllable struct {
	shouldFail bool
}

func (c *failingControllable) Start() error {
	if c.shouldFail {
		return errors.New("intentional failure")
	}
	return nil
}

func (c *failingControllable) Stop() error {
	if c.shouldFail {
		return errors.New("intentional failure")
	}
	return nil
}

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

// benchControllable is a minimal controllable for benchmarking
type benchControllable struct{}

func (c *benchControllable) Start() error { return nil }
func (c *benchControllable) Stop() error  { return nil }

// State Benchmarks

func BenchmarkInternalStateUpdate(b *testing.B) {
	state := newInternalState()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state.updateState(supervisor.StatusRunning, "details")
	}
}

func BenchmarkInternalStateUpdateParallel(b *testing.B) {
	state := newInternalState()

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			state.updateState(supervisor.StatusRunning, "details")
		}
	})
}

func BenchmarkInternalStateSnapshot(b *testing.B) {
	state := newInternalState()
	state.updateState(supervisor.StatusRunning, "test details")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = state.getSnapshot()
	}
}

func BenchmarkInternalStateSnapshotParallel(b *testing.B) {
	state := newInternalState()
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
	state := newInternalState()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state.updateState(supervisor.StatusRunning, nil)
		_ = state.publicState()
	}
}

// Transaction Benchmarks

func BenchmarkTransactionRegister(b *testing.B) {
	logger := zap.NewNop()
	tx := newRegTx(logger)

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
	tx := newRegTx(logger)

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

func BenchmarkSequencerLinearChain(b *testing.B) {
	logger := zap.NewNop()
	seq := newSequencer(logger)
	ctx := context.Background()

	sizes := []int{5, 10, 20}
	for _, size := range sizes {
		name := ""
		if size >= 10 {
			name = string(rune('0' + size/10))
		}
		name += string(rune('0' + size%10))

		b.Run(name+"_services", func(b *testing.B) {
			ops := make([]operation, size)
			for i := 0; i < size; i++ {
				deps := []string{}
				if i > 0 {
					deps = []string{"svc" + string(rune('0'+i-1))}
				}
				ops[i] = operation{
					kind:         opStart,
					id:           "svc" + string(rune('0'+i)),
					controller:   &benchControllable{},
					dependencies: deps,
				}
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = seq.transition(ctx, ops...)
			}
		})
	}
}

func BenchmarkSequencerParallelServices(b *testing.B) {
	logger := zap.NewNop()
	seq := newSequencer(logger)
	ctx := context.Background()

	sizes := []int{10, 20, 50}
	for _, size := range sizes {
		name := ""
		if size >= 10 {
			name = string(rune('0' + size/10))
		}
		name += string(rune('0' + size%10))

		b.Run(name+"_parallel", func(b *testing.B) {
			ops := make([]operation, size)
			for i := 0; i < size; i++ {
				ops[i] = operation{
					kind:         opStart,
					id:           "svc" + string(rune('A'+i%26)) + string(rune('0'+i/26)),
					controller:   &benchControllable{},
					dependencies: nil,
				}
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = seq.transition(ctx, ops...)
			}
		})
	}
}

func BenchmarkSequencerMixedOperations(b *testing.B) {
	logger := zap.NewNop()
	seq := newSequencer(logger)
	ctx := context.Background()

	ops := []operation{
		{kind: opStop, id: "stop1", controller: &benchControllable{}},
		{kind: opStop, id: "stop2", controller: &benchControllable{}},
		{kind: opStart, id: "start1", controller: &benchControllable{}},
		{kind: opStart, id: "start2", controller: &benchControllable{}, dependencies: []string{"start1"}},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = seq.transition(ctx, ops...)
	}
}

// Controller Benchmarks

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
