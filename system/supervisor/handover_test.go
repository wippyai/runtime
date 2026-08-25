// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

// registerInstance commits a registration for serviceID carrying svc, the way a
// registry transaction delivers one.
func registerInstance(h *testSupervisorHarness, serviceID string, svc supervisor.Service) {
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   serviceID,
		Data: &supervisor.Entry{
			Service: svc,
			Config: supervisor.LifecycleConfig{
				AutoStart:    true,
				StartTimeout: 5 * time.Second,
				StopTimeout:  5 * time.Second,
			},
		},
	})
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})
}

// TestSupervisor_RegisterAdoptsReplacementInstance covers the handover a config
// change drives: the entry's manager rebuilds its service, and the supervisor
// must retire the instance it is supervising and adopt the replacement.
func TestSupervisor_RegisterAdoptsReplacementInstance(t *testing.T) {
	h := newTestHarness(t)
	h.start(context.Background())
	defer h.stop()

	const serviceID = "test:replaced"

	original := newTestService()
	registerInstance(h, serviceID, original)
	original.WaitForStart(t)
	require.True(t, original.IsStarted(), "original instance must be started")

	replacement := newTestService()
	registerInstance(h, serviceID, replacement)

	replacement.WaitForStart(t)
	require.True(t, replacement.IsStarted(), "replacement instance must be started by the supervisor")

	original.WaitForStop(t)
	require.True(t, original.IsStopped(), "superseded instance must be stopped")

	h.assertServiceState(serviceID, supervisor.StatusRunning)

	h.sup.mu.RLock()
	ctrl := h.sup.controllers[serviceID]
	h.sup.mu.RUnlock()
	require.NotNil(t, ctrl)
	require.Same(t, replacement, ctrl.Service(), "controller must supervise the replacement")
}

// TestSupervisor_RegisterKeepsIdenticalInstanceRunning guards the handover
// against needless churn: re-registering the instance already supervised must
// not stop and restart it.
func TestSupervisor_RegisterKeepsIdenticalInstanceRunning(t *testing.T) {
	h := newTestHarness(t)
	h.start(context.Background())
	defer h.stop()

	const serviceID = "test:stable"

	svc := newTestService()
	registerInstance(h, serviceID, svc)
	svc.WaitForStart(t)

	registerInstance(h, serviceID, svc)

	require.True(t, svc.IsStarted(), "re-registered instance must stay started")
	require.False(t, svc.IsStopped(), "re-registered instance must not be stopped")
	h.assertServiceState(serviceID, supervisor.StatusRunning)
}

// registerInstanceWithDeps commits a registration for serviceID carrying svc and
// declaring the given dependencies.
func registerInstanceWithDeps(
	h *testSupervisorHarness,
	serviceID string,
	svc supervisor.Service,
	autoStart bool,
	dependencies []string,
) {
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   serviceID,
		Data: &supervisor.Entry{
			Service: svc,
			Config: supervisor.LifecycleConfig{
				AutoStart:    autoStart,
				Requires:     dependencies,
				StartTimeout: 5 * time.Second,
				StopTimeout:  5 * time.Second,
			},
		},
	})
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})
}

// countingService records how many times it is started and stopped so tests can
// assert exactly-once retirement.
type countingService struct {
	statusUpdates chan any
	stopErr       error
	mu            sync.Mutex
	starts        int
	stops         int
	running       bool
}

func newCountingService() *countingService {
	return &countingService{}
}

func (s *countingService) Start(_ context.Context) (<-chan any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statusUpdates = make(chan any, 10)
	s.starts++
	s.running = true
	return s.statusUpdates, nil
}

func (s *countingService) Stop(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stops++
	if s.stopErr != nil {
		return s.stopErr
	}
	if s.statusUpdates != nil {
		close(s.statusUpdates)
		s.statusUpdates = nil
	}
	s.running = false
	return nil
}

func (s *countingService) counts() (starts, stops int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.starts, s.stops
}

func (s *countingService) isRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *countingService) setStopErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopErr = err
}

func awaitCondition(t *testing.T, reason string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", reason)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestSupervisor_ReplacementRetiresRunningDependents covers a replacement whose
// dependent was left untouched by the commit: the dependent captured the
// superseded instance, so it must come down before the replacement is adopted
// and come back up against the adopted one.
func TestSupervisor_ReplacementRetiresRunningDependents(t *testing.T) {
	h := newTestHarness(t)
	h.start(context.Background())
	defer h.stop()

	dependency := newCountingService()
	registerInstanceWithDeps(h, "test:dependency", dependency, true, nil)
	awaitCondition(t, "dependency to start", dependency.isRunning)

	dependent := newCountingService()
	registerInstanceWithDeps(h, "test:dependent", dependent, true, []string{"test:dependency"})
	awaitCondition(t, "dependent to start", dependent.isRunning)

	_, dependentStopsBefore := dependent.counts()
	dependentStartsBefore, _ := dependent.counts()

	// Replace only the dependency.
	replacement := newCountingService()
	registerInstanceWithDeps(h, "test:dependency", replacement, true, nil)

	awaitCondition(t, "replacement to start", replacement.isRunning)
	require.False(t, dependency.isRunning(), "superseded dependency must be stopped")

	awaitCondition(t, "dependent to be restarted", func() bool {
		starts, stops := dependent.counts()
		return stops > dependentStopsBefore && starts > dependentStartsBefore
	})
	require.True(t, dependent.isRunning(), "dependent must be running again after the handover")

	h.assertServiceState("test:dependency", supervisor.StatusRunning)
	h.assertServiceState("test:dependent", supervisor.StatusRunning)
}

// TestSupervisor_RejectedRetirementRestoresRunningSet covers a retirement whose
// stop fails: the supervisor must not be left owning a half-retired set with
// services permanently down.
func TestSupervisor_RejectedRetirementRestoresRunningSet(t *testing.T) {
	h := newTestHarness(t)
	h.start(context.Background())
	defer h.stop()

	// Retries are disabled so the controller cannot mask a lost service: a
	// service left down by a rejected retirement stays down.
	noRetry := supervisor.LifecycleConfig{
		AutoStart:    true,
		StartTimeout: 5 * time.Second,
		StopTimeout:  5 * time.Second,
		RetryPolicy:  supervisor.RetryPolicy{MaxAttempts: 1},
	}
	register := func(id string, svc supervisor.Service) {
		h.sup.handleEvent(event.Event{
			System: supervisor.System,
			Kind:   supervisor.ServiceRegister,
			Path:   id,
			Data:   &supervisor.Entry{Service: svc, Config: noRetry},
		})
	}

	healthy := newCountingService()
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})
	register("test:healthy", healthy)
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})
	awaitCondition(t, "healthy to start", healthy.isRunning)

	stubborn := newCountingService()
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})
	register("test:stubborn", stubborn)
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})
	awaitCondition(t, "stubborn to start", stubborn.isRunning)

	stubborn.setStopErr(errors.New("stop refused"))

	// Replace both in one commit; the stubborn one cannot be stopped.
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})
	register("test:healthy", newCountingService())
	register("test:stubborn", newCountingService())
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})

	// The commit is rejected, so the supervisor keeps the set it had and the
	// service it managed to stop along the way is brought back: a rejected
	// retirement leaves nothing permanently unusable.
	awaitCondition(t, "healthy to be restored", healthy.isRunning)

	h.assertServiceState("test:healthy", supervisor.StatusRunning)
	state, err := h.sup.GetState("test:stubborn")
	require.NoError(t, err, "a rejected retirement must not drop the controller")
	require.NotEqual(t, supervisor.StatusUnknown, state.Status)
}

// TestSupervisor_RetirementCancelsController covers the controller lifetime:
// dropping the map entry is not enough, the retired controller's supervise
// goroutine has to end too.
func TestSupervisor_RetirementCancelsController(t *testing.T) {
	h := newTestHarness(t)
	h.start(context.Background())
	defer h.stop()

	const serviceID = "test:churn"

	before := runtime.NumGoroutine()

	original := newTestService()
	registerInstance(h, serviceID, original)
	original.WaitForStart(t)

	for i := 0; i < 10; i++ {
		replacement := newTestService()
		registerInstance(h, serviceID, replacement)
		replacement.WaitForStart(t)
	}

	// Every retired controller must have released its goroutine; allow a small
	// margin for runtime bookkeeping unrelated to the controllers.
	awaitCondition(t, "retired controllers to release their goroutines", func() bool {
		return runtime.NumGoroutine() <= before+5
	})

	h.sup.mu.RLock()
	controllerCount := len(h.sup.controllers)
	h.sup.mu.RUnlock()
	require.Equal(t, 1, controllerCount, "only the adopted controller may remain")
}

// TestSupervisor_RetirementSkipsAlreadyStoppedService covers managers that stop
// their own instance before re-registering: the supervisor must not call Stop a
// second time.
func TestSupervisor_RetirementSkipsAlreadyStoppedService(t *testing.T) {
	h := newTestHarness(t)
	h.start(context.Background())
	defer h.stop()

	const serviceID = "test:selfstopping"

	original := newCountingService()
	registerInstanceWithDeps(h, serviceID, original, true, nil)
	awaitCondition(t, "original to start", original.isRunning)

	// The manager stops its own instance, the way pg and terminal do during
	// their delete-then-add update.
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceStop,
		Path:   serviceID,
	})
	awaitCondition(t, "original to stop", func() bool { return !original.isRunning() })

	_, stopsAfterSelfStop := original.counts()

	replacement := newCountingService()
	registerInstanceWithDeps(h, serviceID, replacement, true, nil)
	awaitCondition(t, "replacement to start", replacement.isRunning)

	_, stopsAfterHandover := original.counts()
	require.Equal(t, stopsAfterSelfStop, stopsAfterHandover,
		"an already stopped service must not be stopped again by the retirement")
}

func TestSameServiceInstance(t *testing.T) {
	pointer := newTestService()
	other := newTestService()

	require.True(t, sameServiceInstance(pointer, pointer), "the same pointer is the same instance")
	require.False(t, sameServiceInstance(pointer, other), "a different pointer is a replacement")
	require.True(t, sameServiceInstance(nil, nil), "two absent services match")
	require.False(t, sameServiceInstance(pointer, nil), "an absent service is not a match")

	// A comparable value implementation compares by value, so an identical
	// re-registration is recognised instead of being needlessly restarted.
	require.True(t, sameServiceInstance(valueService{name: "a"}, valueService{name: "a"}))
	require.False(t, sameServiceInstance(valueService{name: "a"}, valueService{name: "b"}))
	require.False(t, sameServiceInstance(valueService{name: "a"}, pointer),
		"different implementations never match")
}

// valueService is a comparable value implementation of the public Service
// interface.
type valueService struct {
	name string
}

func (valueService) Start(_ context.Context) (<-chan any, error) { return nil, nil }
func (valueService) Stop(_ context.Context) error                { return nil }
