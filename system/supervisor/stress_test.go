package supervisor

import (
	"context"
	"errors"
	"math/rand"
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
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

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
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

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
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	logger := zap.NewNop()
	seq := NewSequencer(logger)
	ctx := context.Background()

	const serviceCount = 100

	// Create a diamond dependency pattern
	ops := make([]Operation, serviceCount)
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

		ops[i] = Operation{
			Type:         OperationStart,
			ID:           id,
			Controller:   &benchControllable{},
			Dependencies: deps,
		}
	}

	err := seq.Transition(ctx, ops...)
	require.NoError(t, err)
}

// TestStressSequencerConcurrentTransitions tests concurrent transitions
func TestStressSequencerConcurrentTransitions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	logger := zap.NewNop()

	const goroutines = 10
	const transitionsPerGoroutine = 50

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			seq := NewSequencer(logger)
			ctx := context.Background()

			for i := 0; i < transitionsPerGoroutine; i++ {
				ops := []Operation{
					{Type: OperationStart, ID: "svc-a", Controller: &benchControllable{}},
					{Type: OperationStart, ID: "svc-b", Controller: &benchControllable{}, Dependencies: []string{"svc-a"}},
					{Type: OperationStart, ID: "svc-c", Controller: &benchControllable{}, Dependencies: []string{"svc-b"}},
				}
				_ = seq.Transition(ctx, ops...)
			}
		}()
	}

	wg.Wait()
}

// TestStressSupervisorManyServices tests supervisor with many services
func TestStressSupervisorManyServices(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

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
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

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
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	state := newServiceState()

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
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	logger := zap.NewNop()

	const iterations = 100
	const servicesPerIteration = 100

	for iter := 0; iter < iterations; iter++ {
		tx := newTransactionHelper(logger)
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
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

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
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	logger := zap.NewNop()
	seq := NewSequencer(logger)
	ctx := context.Background()

	var failingCtrl = &failingControllable{shouldFail: true}
	var workingCtrl = &benchControllable{}

	ops := []Operation{
		{Type: OperationStop, ID: "svc-a", Controller: workingCtrl},
		{Type: OperationStop, ID: "svc-b", Controller: failingCtrl},
		{Type: OperationStop, ID: "svc-c", Controller: workingCtrl},
	}

	err := seq.Transition(ctx, ops...)
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
