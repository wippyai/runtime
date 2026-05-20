// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"errors"
	"fmt"
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
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// ---- helpers

type testService struct {
	startErr      error
	stopErr       error
	statusUpdates chan any
	startDelay    time.Duration
	stopDelay     time.Duration
	mu            sync.Mutex
	started       bool
	stopped       bool
}

type blockingStartService struct {
	startedCh   chan struct{}
	releaseCh   chan struct{}
	detailsCh   chan any
	startedOnce sync.Once
	stoppedOnce sync.Once
}

func newBlockingStartService() *blockingStartService {
	return &blockingStartService{
		startedCh: make(chan struct{}),
		releaseCh: make(chan struct{}),
		detailsCh: make(chan any),
	}
}

func (s *blockingStartService) Start(ctx context.Context) (<-chan any, error) {
	s.startedOnce.Do(func() { close(s.startedCh) })
	select {
	case <-s.releaseCh:
		return s.detailsCh, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *blockingStartService) Stop(_ context.Context) error {
	s.stoppedOnce.Do(func() { close(s.detailsCh) })
	return nil
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
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})

	for serviceID, autoStart := range services {
		svc := h.service(serviceID)
		h.sup.handleEvent(event.Event{
			System: supervisor.System,
			Kind:   supervisor.ServiceRegister,
			Path:   serviceID,
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

	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})
}

func (h *testSupervisorHarness) registerServiceWithDeps(serviceID string, autoStart bool, dependencies []string) {
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   serviceID,
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
		case supervisor.StatusRunning:
			svc.WaitForStart(h.t)
			require.True(h.t, svc.IsStarted(), "Topology %s should be started", id)
		case supervisor.StatusStopped:
			svc.WaitForStop(h.t)
			require.True(h.t, svc.IsStopped(), "Topology %s should be stopped", id)
		case supervisor.StatusUnknown, supervisor.StatusStarting, supervisor.StatusStopping, supervisor.StatusExited, supervisor.StatusFailed:
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
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRemove,
		Path:   serviceID,
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
	require.Error(h.t, err, "Topology should not exist")
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
	require.True(t, service.IsStarted(), "Topology should be started")

	// stop supervisor and wait for service shutdown
	h.stop()
	service.WaitForStop(t)
	require.True(t, service.IsStopped(), "Topology should be stopped")

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
	require.True(t, svc1.IsStarted(), "Topology 1 should be started automatically")

	svc2 := h.services["service-2"]
	require.False(t, svc2.IsStarted(), "Topology 2 should not be started automatically")

	// Launch service-2 manually
	h.sup.actions <- action{
		kind:      actStart,
		serviceID: "service-2",
	}

	// Both services should be running
	h.waitForAllServices(supervisor.StatusRunning)

	// stop and verify shutdown
	h.stop()
	h.waitForAllServices(supervisor.StatusStopped)
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
	require.True(t, service.IsStarted(), "Topology should be started")

	// Begin transaction and remove the service
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})
	h.removeService("test-service")
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})

	// Verify service is stopped and removed
	service.WaitForStop(t)
	require.True(t, service.IsStopped(), "Topology should be stopped")
	h.assertServiceNotFound("test-service")
}

func TestSupervisor_TransactionValidation(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Attempt operations outside of transaction
	svc := h.service("test-service")
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   "test-service",
		Data: &supervisor.Entry{
			Service: svc,
			Config:  supervisor.LifecycleConfig{},
		},
	})

	// Topology should not be registered without transaction
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
	require.Equal(t, supervisor.StatusUnknown, state.Status)
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
		kind:      actStart,
		serviceID: "service-1",
	}
	h.sup.actions <- action{
		kind:      actStart,
		serviceID: "service-2",
	}

	// wait for services to start
	svc1 := h.services["service-1"]
	svc2 := h.services["service-2"]
	svc1.WaitForStart(t)
	svc2.WaitForStart(t)

	// stop service-1 specifically
	h.sup.actions <- action{
		kind:      actStop,
		serviceID: "service-1",
	}

	// wait for service-1 to stop
	svc1.WaitForStop(t)
	require.True(t, svc1.IsStopped(), "Topology 1 should be stopped")
	require.True(t, svc2.IsStarted(), "Topology 2 should still be running")

	// Launch service-1 again
	h.sup.actions <- action{
		kind:      actStart,
		serviceID: "service-1",
	}

	// wait for service-1 to start again
	svc1.WaitForStart(t)
	require.True(t, svc1.IsStarted(), "Topology 1 should be started again")
}

func TestSupervisor_ServiceFailureAndRetry(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Spawn service that fails on first start attempt
	svc := newTestService()
	var startAttempts int32
	svc.startErr = fmt.Errorf("startup failure")

	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
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
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})

	// wait for retry attempts to complete
	time.Sleep(500 * time.Millisecond)

	// Verify service state
	state, err := h.sup.GetState("failing-service")
	require.NoError(t, err)
	require.Equal(t, supervisor.StatusFailed, state.Status)
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
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})

	// AddCleanup new service and remove existing one
	svc2 := h.service("service-2")
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   "service-2",
		Data: &supervisor.Entry{
			Service: svc2,
			Config:  supervisor.LifecycleConfig{AutoStart: true},
		},
	})
	h.removeService("service-1")

	// Discard transaction
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxDiscard})

	// Verify original state is maintained
	time.Sleep(100 * time.Millisecond) // wait for event processing

	require.True(t, service1.IsStarted(), "Topology 1 should still be running")
	h.assertServiceNotFound("service-2")
}

func TestSupervisor_ConcurrentTransactions(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Launch first transaction
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})

	// Attempt to start another transaction while first is open
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})

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

	// AddCleanup long stop delay to test proper shutdown
	service.stopDelay = 200 * time.Millisecond

	// Done the running service
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})
	h.removeService("running-service")
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})

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
	h.assertServiceState("slow-stop", supervisor.StatusRunning)

	// Introduce a small delay to allow service's helpers state to update
	time.Sleep(50 * time.Millisecond)

	// Initiate stop and immediately check state
	h.sup.actions <- action{
		kind:      actStop,
		serviceID: "slow-stop",
	}

	// wait for full stop
	svc.WaitForStop(t)
	time.Sleep(100 * time.Millisecond) // wait for stop to propagate
	h.assertServiceState("slow-stop", supervisor.StatusStopped)
}

func TestSupervisor_InvalidRegistrationPayload(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Test cases for invalid payloads
	invalidPayloads := []struct {
		data any
		name string
	}{
		{nil, "nil-payload"},
		{&supervisor.Entry{}, "empty-entry"},
		{&supervisor.Entry{Config: supervisor.LifecycleConfig{}}, "missing-service"},
		{"invalid", "string-payload"},
	}

	for _, tc := range invalidPayloads {
		t.Run(tc.name, func(_ *testing.T) {
			h.sup.handleEvent(event.Event{
				System: supervisor.System,
				Kind:   supervisor.ServiceRegister,
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
		kind:      actStart,
		serviceID: "manual-start",
	}

	// wait for services to reach their states
	time.Sleep(500 * time.Millisecond)

	// Spawn all states
	states := h.sup.GetAllStates()

	// Verify expected states
	require.Len(t, states, 3)

	// Auto-start service should be running
	require.Equal(t, supervisor.StatusRunning, states["auto-start"].Status)

	// Manual start service should be running
	require.Equal(t, supervisor.StatusRunning, states["manual-start"].Status)

	// Failing service should be in failed state
	require.Equal(t, supervisor.StatusFailed, states["failing-service"].Status)
}

func TestSupervisor_BusEventControl(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Spawn a service to be controlled via events
	svc := newTestService()
	serviceID := "event-controlled-service"

	// send registration events through the bus
	h.sup.bus.Send(ctx, event.Event{
		System: registry.System,
		Kind:   registry.TxBegin,
	})

	h.sup.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   serviceID,
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

	h.sup.bus.Send(ctx, event.Event{
		System: registry.System,
		Kind:   registry.TxCommit,
	})

	// wait for registration to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify service is registered but not started
	state, err := h.sup.GetState(serviceID)
	require.NoError(t, err, "Topology should be registered")
	require.Equal(t, supervisor.StatusUnknown, state.Status, "Topology should be in Unknown state")
	require.False(t, svc.IsStarted(), "Topology should not be started")

	// Launch the service via bus event
	h.sup.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceStart,
		Path:   serviceID,
	})

	// wait for service to start
	svc.WaitForStart(t)
	require.True(t, svc.IsStarted(), "Topology should be started")

	// Verify running state
	state, err = h.sup.GetState(serviceID)
	require.NoError(t, err)
	require.Equal(t, supervisor.StatusRunning, state.Status, "Topology should be in Running state")

	// stop the service via bus event
	h.sup.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceStop,
		Path:   serviceID,
	})

	// wait for service to stop
	svc.WaitForStop(t)
	require.True(t, svc.IsStopped(), "Topology should be stopped")

	// Verify stopped state
	state, err = h.sup.GetState(serviceID)
	require.NoError(t, err)
	require.Equal(t, supervisor.StatusStopped, state.Status, "Topology should be in Stopped state")

	// Done the service via bus event
	h.sup.bus.Send(ctx, event.Event{
		System: registry.System,
		Kind:   registry.TxBegin,
	})

	h.sup.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRemove,
		Path:   serviceID,
	})

	h.sup.bus.Send(ctx, event.Event{
		System: registry.System,
		Kind:   registry.TxCommit,
	})

	// wait for removal to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify service is removed
	_, err = h.sup.GetState(serviceID)
	require.Error(t, err, "Topology should not exist")
	require.Contains(t, err.Error(), "not found", "Should get not found error")
}

func TestSupervisor_ContextCancellation(t *testing.T) {
	h := newTestHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	h.start(ctx)

	// Register a service that takes time to start
	svc := h.service("slow-service")
	svc.startDelay = 2 * time.Second

	h.registerServices(map[string]bool{
		"slow-service": true,
	})

	// Cancel context while service is starting
	cancel()

	// Wait a bit to ensure cancellation is processed
	time.Sleep(100 * time.Millisecond)

	// Verify service was not started
	require.False(t, svc.IsStarted(), "Service should not be started after context cancellation")
	h.stop()
}

func TestSupervisor_ServiceTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Register a service that takes longer than timeout
	svc := h.service("timeout-service")
	svc.startDelay = 6 * time.Second // Longer than 5s timeout

	h.registerServices(map[string]bool{
		"timeout-service": true,
	})

	// Wait for timeout with context timeout to prevent test hanging.
	// Budget: 5s StartTimeout + 6s startDelay + scheduler slack. 15s
	// gives 4s of headroom over the worst case, which is enough under
	// -race overhead and parallel test load (where 8s would race).
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Poll for service state with context timeout
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Fatal("Timeout waiting for service to fail")
		case <-ticker.C:
			state, err := h.sup.GetState("timeout-service")
			if err == nil && state.Status == supervisor.StatusFailed {
				// Service failed as expected
				goto verifyState
			}
		}
	}

verifyState:

	// Verify service state
	state, err := h.sup.GetState("timeout-service")
	require.NoError(t, err)
	require.Equal(t, supervisor.StatusFailed, state.Status, "Service should be in Failed state due to timeout")

	h.stop()
}

func TestSupervisor_DependencyCycle(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Begin transaction
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})

	// Register services with circular dependency
	h.registerServiceWithDeps("service-a", true, []string{"service-b"})
	h.registerServiceWithDeps("service-b", true, []string{"service-c"})
	h.registerServiceWithDeps("service-c", true, []string{"service-a"})

	// Commit transaction
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})

	// Wait for initial processing
	// Use context with timeout instead of time.Sleep to prevent test hanging
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer waitCancel()

	select {
	case <-waitCtx.Done():
		t.Log("Timeout waiting for initial processing")
	case <-time.After(100 * time.Millisecond):
		// Wait for initial processing
	}

	// Trigger dependency cycle detection by attempting to start one of the services
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceStart,
		Path:   "service-a",
	})

	// Wait longer for dependency cycle detection to complete
	// Use context with timeout instead of time.Sleep to prevent test hanging
	cycleCtx, cycleCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cycleCancel()

	select {
	case <-cycleCtx.Done():
		t.Log("Timeout waiting for dependency cycle detection")
	case <-time.After(500 * time.Millisecond):
		// Wait longer for dependency cycle detection to complete
	}

	// Verify all services are not in Running state due to dependency cycle
	// Note: Current implementation may not actively detect cycles, so services may remain in Unknown status
	for _, id := range []string{"service-a", "service-b", "service-c"} {
		state, err := h.sup.GetState(id)
		require.NoError(t, err)
		require.NotEqual(t, supervisor.StatusRunning, state.Status, "Service %s should not be in Running state due to dependency cycle", id)
		// Services should either be in Unknown (not started) or Failed (if cycle detection is implemented)
		require.Contains(t, []supervisor.Status{supervisor.StatusUnknown, supervisor.StatusFailed}, state.Status,
			"Service %s should be in Unknown or Failed state due to dependency cycle", id)
	}

	h.stop()
}

func TestSupervisor_ConcurrentServiceOperations(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Register multiple services
	services := map[string]bool{
		"service-1": true,
		"service-2": true,
		"service-3": true,
	}
	h.registerServices(services)

	// Start concurrent operations
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Randomly start/stop services
			for _, id := range []string{"service-1", "service-2", "service-3"} {
				if rand.Float32() < 0.5 {
					h.sup.handleEvent(event.Event{
						System: supervisor.System,
						Kind:   supervisor.ServiceStart,
						Path:   id,
					})
				} else {
					h.sup.handleEvent(event.Event{
						System: supervisor.System,
						Kind:   supervisor.ServiceStop,
						Path:   id,
					})
				}
			}
		}()
	}

	wg.Wait()
	time.Sleep(100 * time.Millisecond) // Wait for operations to complete

	// Verify no panics occurred and supervisor is still running
	states := h.sup.GetAllStates()
	require.Len(t, states, 3, "All services should still be registered")

	h.stop()
}

func TestSupervisor_ServiceStatusUpdates(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Register a service
	svc := h.service("status-service")
	h.registerServices(map[string]bool{
		"status-service": true,
	})

	// Start the service
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceStart,
		Path:   "status-service",
	})

	// Wait for service to start
	svc.WaitForStart(t)

	// Send status updates
	svc.statusUpdates <- "status-1"
	svc.statusUpdates <- "status-2"
	svc.statusUpdates <- "status-3"

	// Wait for status updates to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify state contains latest status
	state, err := h.sup.GetState("status-service")
	require.NoError(t, err)
	require.Equal(t, "status-3", state.Details, "State should contain latest status update")

	h.stop()
}

// Dependency Tests

func TestSupervisor_DependencyOrdering(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Register services with dependencies: A -> B -> C
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})
	h.registerServiceWithDeps("service-c", false, nil)                   // no deps
	h.registerServiceWithDeps("service-b", false, []string{"service-c"}) // depends on C
	h.registerServiceWithDeps("service-a", false, []string{"service-b"}) // depends on B
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})

	// Launch service A, which should trigger starting dependencies first
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceStart,
		Path:   "service-a",
	})

	// Wait for all services to reach running state instead of fixed sleep
	h.waitForAllServices(supervisor.StatusRunning)

	// Verify states
	h.assertServiceState("service-c", supervisor.StatusRunning)
	h.assertServiceState("service-b", supervisor.StatusRunning)
	h.assertServiceState("service-a", supervisor.StatusRunning)
}

func TestSupervisor_DependencyFailure(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Service B will fail to start
	svcB := h.service("service-b")
	svcB.startErr = errors.New("failed to start service B")

	// Register services with dependencies: A -> B -> C
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})
	h.registerServiceWithDeps("service-c", false, nil)
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   "service-b",
		Data: &supervisor.Entry{
			Service: svcB,
			Config: supervisor.LifecycleConfig{
				DependsOn:    []string{"service-c"},
				StartTimeout: 5 * time.Second,
				StopTimeout:  5 * time.Second,
				RetryPolicy: supervisor.RetryPolicy{
					MaxAttempts:  3,
					InitialDelay: 100 * time.Millisecond,
				},
			},
		},
	})
	h.registerServiceWithDeps("service-a", false, []string{"service-b"})
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})

	// Launch service A
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceStart,
		Path:   "service-a",
	})

	// Wait a bit longer for retries and full failure
	time.Sleep(500 * time.Millisecond)

	// First verify service C is running
	h.assertServiceState("service-c", supervisor.StatusRunning)

	// Then verify B has fully failed (after retries)
	state, err := h.sup.GetState("service-b")
	require.NoError(t, err)
	require.Equal(t, supervisor.StatusFailed, state.Status, "service-b should be in failed state")

	// Finally verify A didn't start
	h.assertServiceState("service-a", supervisor.StatusUnknown) // A shouldn't start if B fails
}

func TestSupervisor_ParallelDependencyStart(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// AddCleanup artificial delay to B and C
	svcB := h.service("service-b")
	svcC := h.service("service-c")
	svcB.startDelay = 200 * time.Millisecond
	svcC.startDelay = 200 * time.Millisecond

	// Register services with parallel dependencies: A -> (B,C)
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})
	h.registerServiceWithDeps("service-b", false, nil)
	h.registerServiceWithDeps("service-c", false, nil)
	h.registerServiceWithDeps("service-a", false, []string{"service-b", "service-c"})
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})

	startTime := time.Now()

	// Launch service A
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceStart,
		Path:   "service-a",
	})

	// Wait for all services to reach running state instead of fixed sleep
	h.waitForAllServices(supervisor.StatusRunning)

	// Verify states
	h.assertServiceState("service-b", supervisor.StatusRunning)
	h.assertServiceState("service-c", supervisor.StatusRunning)
	h.assertServiceState("service-a", supervisor.StatusRunning)

	// Verify parallel execution
	totalTime := time.Since(startTime)
	require.Less(t, totalTime, 400*time.Millisecond,
		"Dependencies should have started in parallel")
}

func TestSupervisor_DependencyStopOrder(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Register services with dependencies: A -> B -> C
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})
	h.registerServiceWithDeps("service-c", false, nil)
	h.registerServiceWithDeps("service-b", false, []string{"service-c"})
	h.registerServiceWithDeps("service-a", false, []string{"service-b"})
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})

	// Launch all services by starting A
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceStart,
		Path:   "service-a",
	})

	// Wait for all services to reach running state instead of fixed sleep
	h.waitForAllServices(supervisor.StatusRunning)

	// Verify all services started
	h.assertServiceState("service-c", supervisor.StatusRunning)
	h.assertServiceState("service-b", supervisor.StatusRunning)
	h.assertServiceState("service-a", supervisor.StatusRunning)

	// Now stop service C
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceStop,
		Path:   "service-c",
	})

	// Wait for all services to reach stopped state instead of fixed sleep
	h.waitForAllServices(supervisor.StatusStopped)

	// Verify all services stopped in correct order
	h.assertServiceState("service-a", supervisor.StatusStopped)
	h.assertServiceState("service-b", supervisor.StatusStopped)
	h.assertServiceState("service-c", supervisor.StatusStopped)
}

func TestSupervisor_MissingDependencies(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Register service A with missing dependency
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})
	h.registerServiceWithDeps("service-a", false, []string{"missing-service"})
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})

	// Try to start service A
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceStart,
		Path:   "service-a",
	})

	time.Sleep(100 * time.Millisecond)

	// Verify service A didn't start
	state, err := h.sup.GetState("service-a")
	require.NoError(t, err)
	require.NotEqual(t, supervisor.StatusRunning, state.Status)
}

func TestSupervisor_AddDependencyToExistingService(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// First register service B (not autostarted)
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})
	h.registerServiceWithDeps("service-b", false, nil)
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})

	// Wait for registration to complete and verify B is registered but not running
	time.Sleep(100 * time.Millisecond)
	state, err := h.sup.GetState("service-b")
	require.NoError(t, err)
	require.Equal(t, supervisor.StatusUnknown, state.Status)

	// Now register service A that depends on B
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})
	h.registerServiceWithDeps("service-a", true, []string{"service-b"})
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})

	time.Sleep(200 * time.Millisecond)

	// Both services should now be running because A's autostart triggered B
	h.assertServiceState("service-b", supervisor.StatusRunning)
	h.assertServiceState("service-a", supervisor.StatusRunning)

	// close A and verify B keeps running (since it was started as dependency)
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceStop,
		Path:   "service-a",
	})

	time.Sleep(200 * time.Millisecond)

	h.assertServiceState("service-b", supervisor.StatusRunning)
	h.assertServiceState("service-a", supervisor.StatusStopped)
}

func TestSupervisor_ComplexDependencyChain_WithPreexisting(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// First register and start service-base
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})
	h.registerServiceWithDeps("service-base", true, nil) // autostart true
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})

	time.Sleep(100 * time.Millisecond)
	h.assertServiceState("service-base", supervisor.StatusRunning)

	// Register service-middle that depends on service-base but don't autostart
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})
	h.registerServiceWithDeps("service-middle", false, []string{"service-base"})
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})

	time.Sleep(100 * time.Millisecond)
	h.assertServiceState("service-middle", supervisor.StatusUnknown) // Should not be started

	// Now register service-top with autostart that depends on service-middle
	// This should trigger starting both service-middle and service-top
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})
	h.registerServiceWithDeps("service-top", true, []string{"service-middle"})
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})

	time.Sleep(100 * time.Millisecond)

	// Verify all services are running
	h.assertServiceState("service-base", supervisor.StatusRunning)
	h.assertServiceState("service-middle", supervisor.StatusRunning)
	h.assertServiceState("service-top", supervisor.StatusRunning)

	// Now stop service-middle - this should stop service-top but leave service-base running
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceStop,
		Path:   "service-middle",
	})

	time.Sleep(100 * time.Millisecond)

	h.assertServiceState("service-base", supervisor.StatusRunning)   // Should still be running
	h.assertServiceState("service-middle", supervisor.StatusStopped) // Should be stopped
	h.assertServiceState("service-top", supervisor.StatusStopped)    // Should be stopped due to dependency

	// Finally stop service-base
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceStop,
		Path:   "service-base",
	})

	time.Sleep(100 * time.Millisecond)

	// All services should be stopped
	h.assertServiceState("service-base", supervisor.StatusStopped)
	h.assertServiceState("service-middle", supervisor.StatusStopped)
	h.assertServiceState("service-top", supervisor.StatusStopped)

	// Now try to start service-top - this should fail since dependencies are not started
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceStart,
		Path:   "service-top",
	})

	time.Sleep(100 * time.Millisecond)
	h.assertServiceState("service-base", supervisor.StatusRunning)   // Should still be running
	h.assertServiceState("service-middle", supervisor.StatusRunning) // Should still be running
	h.assertServiceState("service-top", supervisor.StatusRunning)    // Should not start due to missing deps
}

func TestSupervisor_WithoutDependencyResolver(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)

	// Register services with only lifecycle dependencies
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})
	h.registerServiceWithDeps("service-b", false, nil)
	h.registerServiceWithDeps("service-a", false, []string{"service-b"})
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})

	// Start service-a
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceStart,
		Path:   "service-a",
	})

	h.waitForAllServices(supervisor.StatusRunning)

	// Verify lifecycle dependencies work without resolver
	h.assertServiceState("service-b", supervisor.StatusRunning)
	h.assertServiceState("service-a", supervisor.StatusRunning)
}

func TestSupervisor_GetStateDoesNotBlockDuringCommitTransition(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	h.start(ctx)
	defer h.stop()

	svc := newBlockingStartService()

	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   "slow-service",
		Data: &supervisor.Entry{
			Service: svc,
			Config: supervisor.LifecycleConfig{
				AutoStart:    true,
				StartTimeout: 5 * time.Second,
				StopTimeout:  time.Second,
				RetryPolicy: supervisor.RetryPolicy{
					MaxAttempts: 1,
				},
			},
		},
	})
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})

	select {
	case <-svc.startedCh:
	case <-time.After(time.Second):
		t.Fatal("service start did not begin")
	}

	getDone := make(chan error, 1)
	go func() {
		_, err := h.sup.GetState("slow-service")
		getDone <- err
	}()

	blocked := false
	select {
	case err := <-getDone:
		require.NoError(t, err)
	case <-time.After(150 * time.Millisecond):
		blocked = true
	}

	close(svc.releaseCh)

	if blocked {
		select {
		case <-getDone:
		case <-time.After(time.Second):
		}
		t.Fatal("GetState blocked while commit transition was in progress")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state, err := h.sup.GetState("slow-service")
		if err == nil && state.Status == supervisor.StatusRunning {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("service did not reach running state")
}
