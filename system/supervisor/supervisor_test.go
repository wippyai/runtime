package supervisor

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/system/eventbus"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// ---- helpers

type testService struct {
	mu            sync.Mutex
	started       bool
	stopped       bool
	startErr      error
	stopErr       error
	startDelay    time.Duration
	stopDelay     time.Duration
	statusUpdates chan any
}

func newTestService() *testService {
	return &testService{}
}

func (s *testService) Start(ctx context.Context) (<-chan any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.startErr != nil {
		return nil, s.startErr
	}

	if s.startDelay > 0 {
		select {
		case <-time.After(s.startDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// Spawn new channel for status updates
	s.statusUpdates = make(chan any, 10)
	s.started = true
	s.stopped = false

	return s.statusUpdates, nil
}

func (s *testService) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopErr != nil {
		return s.stopErr
	}

	if s.stopDelay > 0 {
		select {
		case <-time.After(s.stopDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if s.statusUpdates != nil {
		close(s.statusUpdates)
		s.statusUpdates = nil
	}

	s.started = false
	s.stopped = true

	return nil
}

func (s *testService) IsStarted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started
}

func (s *testService) IsStopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

func (s *testService) WaitForStart(t testing.TB) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if s.IsStarted() {
			time.Sleep(10 * time.Millisecond) // let it propagate
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("timeout waiting for service to start")
}

func (s *testService) WaitForStop(t testing.TB) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if s.IsStopped() {
			time.Sleep(10 * time.Millisecond) // let it propagate
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timeout waiting for service to stop")
}

// testSupervisorHarness is a test helper for supervisor operations
type testSupervisorHarness struct {
	t        testing.TB
	sup      *Supervisor
	logs     *observer.ObservedLogs
	services map[string]*testService
}

func newTestHarness(t testing.TB) *testSupervisorHarness {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	bus := eventbus.NewBus()

	h := &testSupervisorHarness{
		t:        t,
		sup:      NewSupervisor(bus, logger),
		logs:     logs,
		services: make(map[string]*testService),
	}

	return h
}

func (h *testSupervisorHarness) start(ctx context.Context) {
	err := h.sup.Start(ctx)
	require.NoError(h.t, err, "Failed to start supervisor")
}

func (h *testSupervisorHarness) stop() {
	err := h.sup.Stop()
	require.NoError(h.t, err, "Failed to stop supervisor")
}

func (h *testSupervisorHarness) service(serviceID string) *testService {
	if svc, ok := h.services[serviceID]; ok {
		return svc
	}

	svc := newTestService()
	h.services[serviceID] = svc
	return svc
}

func (h *testSupervisorHarness) registerServices(services map[string]bool) {
	h.sup.handleEvent(events.Event{System: registry.System, Kind: registry.Begin})

	for serviceID, autoStart := range services {
		svc := h.service(serviceID)
		h.sup.handleEvent(events.Event{
			System: supervisor.System,
			Kind:   supervisor.Register,
			Path:   events.Path(serviceID),
			Data: &supervisor.Entry{
				Service: svc,
				Config: supervisor.LifecycleConfig{
					AutoStart:    autoStart,
					StartTimeout: 5 * time.Second,
					StopTimeout:  5 * time.Second,
					RetryPolicy: supervisor.RetryPolicy{
						MaxAttempts:  3,
						InitialDelay: 100 * time.Millisecond,
					},
				},
			},
		})
	}

	h.sup.handleEvent(events.Event{System: registry.System, Kind: registry.Commit})
}

func (h *testSupervisorHarness) registerServiceWithDeps(serviceID string, autoStart bool, dependencies []string) {
	h.sup.handleEvent(events.Event{
		System: supervisor.System,
		Kind:   supervisor.Register,
		Path:   events.Path(serviceID),
		Data: &supervisor.Entry{
			Service: h.service(serviceID),
			Config: supervisor.LifecycleConfig{
				AutoStart:    autoStart,
				DependsOn:    dependencies,
				StartTimeout: 5 * time.Second,
				StopTimeout:  5 * time.Second,
				RetryPolicy: supervisor.RetryPolicy{
					MaxAttempts:  3,
					InitialDelay: 100 * time.Millisecond,
				},
			},
		},
	})
}

func (h *testSupervisorHarness) waitForAllServices(state supervisor.Status) {
	h.t.Helper()
	for id, svc := range h.services {
		switch state {
		case supervisor.Running:
			svc.WaitForStart(h.t)
			require.True(h.t, svc.IsStarted(), "Lifecycle %s should be started", id)
		case supervisor.Stopped:
			svc.WaitForStop(h.t)
			require.True(h.t, svc.IsStopped(), "Lifecycle %s should be stopped", id)
		case supervisor.Unknown, supervisor.Starting, supervisor.Stopping, supervisor.Exited, supervisor.Failed:
			panic("not implemented")
		}
	}
}

func (h *testSupervisorHarness) assertLog(message string) {
	h.t.Helper()
	for _, log := range h.logs.All() {
		if log.Message == message {
			return
		}
	}
	h.t.Errorf("Expected log message not found: %s", message)
}

func (h *testSupervisorHarness) removeService(serviceID string) {
	h.sup.handleEvent(events.Event{
		System: supervisor.System,
		Kind:   supervisor.Remove,
		Path:   events.Path(serviceID),
	})
}

func (h *testSupervisorHarness) assertServiceState(serviceID string, expectedStatus supervisor.Status) {
	h.t.Helper()
	state, err := h.sup.GetState(serviceID)
	require.NoError(h.t, err, "Failed to get service state")
	require.Equal(h.t, expectedStatus, state.Status, "Unexpected service status")
}

func (h *testSupervisorHarness) assertServiceNotFound(serviceID string) {
	h.t.Helper()
	_, err := h.sup.GetState(serviceID)
	require.Error(h.t, err, "Lifecycle should not exist")
	require.Contains(h.t, err.Error(), "not found")
}

// ---- end of helpers

func TestSupervisor_BasicLifecycle(t *testing.T) {
	h := newTestHarness(t)

	// Launch supervisor and register service
	ctx := context.Background()
	h.start(ctx)
	h.registerServices(map[string]bool{
		"test-service": true,
	})

	// wait for service startup
	service := h.services["test-service"]
	service.WaitForStart(t)
	require.True(t, service.IsStarted(), "Lifecycle should be started")

	// stop supervisor and wait for service shutdown
	h.stop()
	service.WaitForStop(t)
	require.True(t, service.IsStopped(), "Lifecycle should be stopped")

	// Verify logs
	h.assertLog("supervisor started")
	h.assertLog("supervisor stopped")
}

func TestSupervisor_MultipleServices(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Register two services with different autostart settings
	h.registerServices(map[string]bool{
		"service-1": true,  // autostart
		"service-2": false, // manual start
	})

	// Only service-1 should be started automatically
	svc1 := h.services["service-1"]
	svc1.WaitForStart(t)
	require.True(t, svc1.IsStarted(), "Lifecycle 1 should be started automatically")

	svc2 := h.services["service-2"]
	require.False(t, svc2.IsStarted(), "Lifecycle 2 should not be started automatically")

	// Launch service-2 manually
	h.sup.actions <- action{
		kind:      actionStart,
		serviceID: "service-2",
	}

	// Both services should be running
	h.waitForAllServices(supervisor.Running)

	// stop and verify shutdown
	h.stop()
	h.waitForAllServices(supervisor.Stopped)
}

func TestSupervisor_ServiceRemoval(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Register a service
	h.registerServices(map[string]bool{
		"test-service": true,
	})

	service := h.services["test-service"]
	service.WaitForStart(t)
	require.True(t, service.IsStarted(), "Lifecycle should be started")

	// Begin transaction and remove the service
	h.sup.handleEvent(events.Event{System: registry.System, Kind: registry.Begin})
	h.removeService("test-service")
	h.sup.handleEvent(events.Event{System: registry.System, Kind: registry.Commit})

	// Verify service is stopped and removed
	service.WaitForStop(t)
	require.True(t, service.IsStopped(), "Lifecycle should be stopped")
	h.assertServiceNotFound("test-service")
}

func TestSupervisor_TransactionValidation(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Attempt operations outside of transaction
	svc := h.service("test-service")
	h.sup.handleEvent(events.Event{
		System: supervisor.System,
		Kind:   supervisor.Register,
		Path:   "test-service",
		Data: &supervisor.Entry{
			Service: svc,
			Config:  supervisor.LifecycleConfig{},
		},
	})

	// Lifecycle should not be registered without transaction
	time.Sleep(100 * time.Millisecond) // wait for event processing
	h.assertServiceNotFound("test-service")

	// Now register properly within transaction
	h.registerServices(map[string]bool{
		"test-service": false, // Don't auto-start
	})

	// Verify service is registered but not started
	time.Sleep(100 * time.Millisecond) // wait for event processing
	state, err := h.sup.GetState("test-service")
	require.NoError(t, err)
	require.Equal(t, supervisor.Unknown, state.Status)
}

func TestSupervisor_TargetedServiceControl(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Register services without auto-start
	h.registerServices(map[string]bool{
		"service-1": false,
		"service-2": false,
	})

	// Launch both services manually
	h.sup.actions <- action{
		kind:      actionStart,
		serviceID: "service-1",
	}
	h.sup.actions <- action{
		kind:      actionStart,
		serviceID: "service-2",
	}

	// wait for services to start
	svc1 := h.services["service-1"]
	svc2 := h.services["service-2"]
	svc1.WaitForStart(t)
	svc2.WaitForStart(t)

	// stop service-1 specifically
	h.sup.actions <- action{
		kind:      actionStop,
		serviceID: "service-1",
	}

	// wait for service-1 to stop
	svc1.WaitForStop(t)
	require.True(t, svc1.IsStopped(), "Lifecycle 1 should be stopped")
	require.True(t, svc2.IsStarted(), "Lifecycle 2 should still be running")

	// Launch service-1 again
	h.sup.actions <- action{
		kind:      actionStart,
		serviceID: "service-1",
	}

	// wait for service-1 to start again
	svc1.WaitForStart(t)
	require.True(t, svc1.IsStarted(), "Lifecycle 1 should be started again")
}

func TestSupervisor_ServiceFailureAndRetry(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Spawn service that fails on first start attempt
	svc := newTestService()
	var startAttempts int32
	svc.startErr = fmt.Errorf("startup failure")

	h.sup.handleEvent(events.Event{System: registry.System, Kind: registry.Begin})
	h.sup.handleEvent(events.Event{
		System: supervisor.System,
		Kind:   supervisor.Register,
		Path:   "failing-service",
		Data: &supervisor.Entry{
			Service: svc,
			Config: supervisor.LifecycleConfig{
				AutoStart:    true,
				StartTimeout: time.Second,
				RetryPolicy: supervisor.RetryPolicy{
					MaxAttempts:  2,
					InitialDelay: 100 * time.Millisecond,
				},
			},
		},
	})
	h.sup.handleEvent(events.Event{System: registry.System, Kind: registry.Commit})

	// wait for retry attempts to complete
	time.Sleep(500 * time.Millisecond)

	// Verify service state
	state, err := h.sup.GetState("failing-service")
	require.NoError(t, err)
	require.Equal(t, supervisor.Failed, state.Status)
	require.True(t, atomic.LoadInt32(&startAttempts) <= 2, "Should not exceed max retry attempts")
}

func TestSupervisor_TransactionDiscard(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Register initial service
	h.registerServices(map[string]bool{
		"service-1": true,
	})

	// wait for service to start
	service1 := h.services["service-1"]
	service1.WaitForStart(t)

	// Begin transaction for changes
	h.sup.handleEvent(events.Event{System: registry.System, Kind: registry.Begin})

	// Add new service and remove existing one
	svc2 := h.service("service-2")
	h.sup.handleEvent(events.Event{
		System: supervisor.System,
		Kind:   supervisor.Register,
		Path:   "service-2",
		Data: &supervisor.Entry{
			Service: svc2,
			Config:  supervisor.LifecycleConfig{AutoStart: true},
		},
	})
	h.removeService("service-1")

	// Discard transaction
	h.sup.handleEvent(events.Event{System: registry.System, Kind: registry.Discard})

	// Verify original state is maintained
	time.Sleep(100 * time.Millisecond) // wait for event processing

	require.True(t, service1.IsStarted(), "Lifecycle 1 should still be running")
	h.assertServiceNotFound("service-2")
}

func TestSupervisor_ConcurrentTransactions(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Launch first transaction
	h.sup.handleEvent(events.Event{System: registry.System, Kind: registry.Begin})

	// Attempt to start another transaction while first is open
	h.sup.handleEvent(events.Event{System: registry.System, Kind: registry.Begin})

	time.Sleep(100 * time.Millisecond) // wait for event processing

	// Check logs for warning about nested transaction
	var found bool
	for _, log := range h.logs.All() {
		if log.Message == "received begin transaction while already in transaction, resetting state" {
			found = true
			break
		}
	}
	require.True(t, found, "Expected warning about nested transaction")
}

func TestSupervisor_RemoveRunningService(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Register and start a service
	h.registerServices(map[string]bool{
		"running-service": true,
	})

	// wait for service to start
	service := h.services["running-service"]
	service.WaitForStart(t)
	require.True(t, service.IsStarted())

	// Add long stop delay to test proper shutdown
	service.stopDelay = 200 * time.Millisecond

	// Remove the running service
	h.sup.handleEvent(events.Event{System: registry.System, Kind: registry.Begin})
	h.removeService("running-service")
	h.sup.handleEvent(events.Event{System: registry.System, Kind: registry.Commit})

	// Verify service is properly stopped and removed
	service.WaitForStop(t)
	require.True(t, service.IsStopped())
	h.assertServiceNotFound("running-service")
}

func TestSupervisor_ServiceStoppingStates(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Register service with delayed stop
	svc := h.service("slow-stop")
	svc.stopDelay = 200 * time.Millisecond

	h.registerServices(map[string]bool{
		"slow-stop": true,
	})

	// wait for service to start and reach Running state
	svc.WaitForStart(t)
	h.assertServiceState("slow-stop", supervisor.Running)

	// Introduce a small delay to allow service's helpers state to update
	time.Sleep(50 * time.Millisecond)

	// Initiate stop and immediately check state
	h.sup.actions <- action{
		kind:      actionStop,
		serviceID: "slow-stop",
	}

	// wait for full stop
	svc.WaitForStop(t)
	time.Sleep(100 * time.Millisecond) // wait for stop to propagate
	h.assertServiceState("slow-stop", supervisor.Stopped)
}

func TestSupervisor_InvalidRegistrationPayload(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Test cases for invalid payloads
	invalidPayloads := []struct {
		name string
		data any
	}{
		{"nil-payload", nil},
		{"empty-entry", &supervisor.Entry{}},
		{"missing-service", &supervisor.Entry{Config: supervisor.LifecycleConfig{}}},
		{"string-payload", "invalid"},
	}

	for _, tc := range invalidPayloads {
		t.Run(tc.name, func(_ *testing.T) {
			h.sup.handleEvent(events.Event{
				System: supervisor.System,
				Kind:   supervisor.Register,
				Path:   "test-service",
				Data:   tc.data,
			})

			// Verify service was not registered
			time.Sleep(50 * time.Millisecond)
			h.assertServiceNotFound("test-service")
		})
	}
}

func TestSupervisor_GetAllStates(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Register multiple services with different states
	h.registerServices(map[string]bool{
		"auto-start":      true,
		"manual-start":    false,
		"failing-service": true,
	})

	// Configure failing service using the mutex
	failingSvc := h.services["failing-service"]
	failingSvc.mu.Lock()
	failingSvc.startErr = fmt.Errorf("startup failure")
	failingSvc.mu.Unlock()

	// Launch manual service
	h.sup.actions <- action{
		kind:      actionStart,
		serviceID: "manual-start",
	}

	// wait for services to reach their states
	time.Sleep(500 * time.Millisecond)

	// Spawn all states
	states := h.sup.GetAllStates()

	// Verify expected states
	require.Len(t, states, 3)

	// Auto-start service should be running
	require.Equal(t, supervisor.Running, states["auto-start"].Status)

	// Manual start service should be running
	require.Equal(t, supervisor.Running, states["manual-start"].Status)

	// Failing service should be in failed state
	require.Equal(t, supervisor.Failed, states["failing-service"].Status)
}

func TestSupervisor_BusEventControl(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Spawn a service to be controlled via events
	svc := newTestService()
	serviceID := "event-controlled-service"

	// send registration events through the bus
	h.sup.bus.Send(ctx, events.Event{
		System: registry.System,
		Kind:   registry.Begin,
	})

	h.sup.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Register,
		Path:   events.Path(serviceID),
		Data: &supervisor.Entry{
			Service: svc,
			Config: supervisor.LifecycleConfig{
				AutoStart:    false, // Don't start automatically
				StartTimeout: 5 * time.Second,
				StopTimeout:  5 * time.Second,
				RetryPolicy: supervisor.RetryPolicy{
					MaxAttempts:  3,
					InitialDelay: 100 * time.Millisecond,
				},
			},
		},
	})

	h.sup.bus.Send(ctx, events.Event{
		System: registry.System,
		Kind:   registry.Commit,
	})

	// wait for registration to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify service is registered but not started
	state, err := h.sup.GetState(serviceID)
	require.NoError(t, err, "Lifecycle should be registered")
	require.Equal(t, supervisor.Unknown, state.Status, "Lifecycle should be in Unknown state")
	require.False(t, svc.IsStarted(), "Lifecycle should not be started")

	// Launch the service via bus event
	h.sup.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Start,
		Path:   events.Path(serviceID),
	})

	// wait for service to start
	svc.WaitForStart(t)
	require.True(t, svc.IsStarted(), "Lifecycle should be started")

	// Verify running state
	state, err = h.sup.GetState(serviceID)
	require.NoError(t, err)
	require.Equal(t, supervisor.Running, state.Status, "Lifecycle should be in Running state")

	// stop the service via bus event
	h.sup.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Stop,
		Path:   events.Path(serviceID),
	})

	// wait for service to stop
	svc.WaitForStop(t)
	require.True(t, svc.IsStopped(), "Lifecycle should be stopped")

	// Verify stopped state
	state, err = h.sup.GetState(serviceID)
	require.NoError(t, err)
	require.Equal(t, supervisor.Stopped, state.Status, "Lifecycle should be in Stopped state")

	// Remove the service via bus event
	h.sup.bus.Send(ctx, events.Event{
		System: registry.System,
		Kind:   registry.Begin,
	})

	h.sup.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Remove,
		Path:   events.Path(serviceID),
	})

	h.sup.bus.Send(ctx, events.Event{
		System: registry.System,
		Kind:   registry.Commit,
	})

	// wait for removal to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify service is removed
	_, err = h.sup.GetState(serviceID)
	require.Error(t, err, "Lifecycle should not exist")
	require.Contains(t, err.Error(), "not found", "Should get not found error")
}
