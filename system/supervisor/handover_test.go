// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
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

// TestSupervisor_ReplacementStartFailureKeepsReplacementOwned pins the point of
// no return in a handover. Once retirement succeeds, the replacement remains
// supervisor-owned and follows the ordinary failed/retry lifecycle; restoring
// the old controller would diverge from managers and resource registries that
// have already committed the replacement instance.
func TestSupervisor_ReplacementStartFailureKeepsReplacementOwned(t *testing.T) {
	h := newTestHarness(t)
	h.start(context.Background())
	defer h.stop()

	const serviceID = "test:replacement-start-fails"
	original := newCountingService()
	registerInstance(h, serviceID, original)
	awaitCondition(t, "original to start", original.isRunning)

	replacement := newCountingService()
	replacement.setStartErr(errors.New("replacement cannot start"))
	registerInstanceWithDeps(h, serviceID, replacement, true, nil)

	awaitCondition(t, "replacement failure", func() bool {
		state, err := h.sup.GetState(serviceID)
		return err == nil && state.Status == supervisor.StatusFailed
	})
	_, originalStops := original.counts()
	require.Equal(t, 1, originalStops, "the superseded instance must remain retired")

	h.sup.mu.RLock()
	ctrl := h.sup.controllers[serviceID]
	h.sup.mu.RUnlock()
	require.NotNil(t, ctrl)
	require.Same(t, replacement, ctrl.Service(), "the replacement must retain lifecycle ownership")
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
	startErr      error
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
	if s.startErr != nil {
		return nil, s.startErr
	}
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

func (s *countingService) setStartErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startErr = err
}

// commitOutcomeContains reports whether the outcome of a commit carried the
// fragment. The control loop logs the error execute returned, so this reads the
// transaction outcome rather than any log line a helper happened to write on
// its own.
func (h *testSupervisorHarness) commitOutcomeContains(fragment string) bool {
	for _, entry := range h.logs.All() {
		if entry.Message != "failed to execute commit protocol" {
			continue
		}
		for _, v := range entry.ContextMap() {
			if strings.Contains(fmt.Sprintf("%v", v), fragment) {
				return true
			}
		}
	}
	return false
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
	// re-registration is recognized instead of being needlessly restarted.
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

// TestSupervisor_RejectedRetirementReportsUnrestoredServices covers the worst
// outcome this path can produce: a retirement is rejected and a service it
// stopped cannot be brought back. That service was running before the commit
// and is not running after it, so the condition has to reach the transaction
// outcome and the supervisor's own state, not just a log line.
func TestSupervisor_RejectedRetirementReportsUnrestoredServices(t *testing.T) {
	h := newTestHarness(t)
	h.start(context.Background())
	defer h.stop()

	noRetry := supervisor.LifecycleConfig{
		AutoStart:    true,
		StartTimeout: time.Second,
		StopTimeout:  time.Second,
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

	unrestorable := newCountingService()
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})
	register("test:unrestorable", unrestorable)
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})
	awaitCondition(t, "service to start", unrestorable.isRunning)

	stubborn := newCountingService()
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})
	register("test:stubborn", stubborn)
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})
	awaitCondition(t, "stubborn to start", stubborn.isRunning)

	// The retirement will fail on one service, and the service it does stop
	// cannot be started again.
	stubborn.setStopErr(errors.New("stop refused"))
	unrestorable.setStartErr(errors.New("start refused"))

	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})
	register("test:unrestorable", newCountingService())
	register("test:stubborn", newCountingService())
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})

	// The supervisor's own state shows the service is not running.
	awaitCondition(t, "supervisor to report the service is not running", func() bool {
		state, err := h.sup.GetState("test:unrestorable")
		return err == nil && state.Status != supervisor.StatusRunning
	})

	// And the outcome of the commit itself names it, rather than the failure
	// ending in a bare log line inside the restore.
	awaitCondition(t, "the rejected commit to report the unrestored service", func() bool {
		return h.commitOutcomeContains("services left stopped by a rejected retirement") &&
			h.commitOutcomeContains("test:unrestorable")
	})
}

// strictService fails a second Stop, so a test can pin exactly-once retirement
// rather than relying on implementations that happen to tolerate a repeat.
type strictService struct {
	statusUpdates chan any
	mu            sync.Mutex
	stops         int
	running       bool
}

func newStrictService() *strictService {
	return &strictService{}
}

func (s *strictService) Start(_ context.Context) (<-chan any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statusUpdates = make(chan any, 10)
	s.running = true
	return s.statusUpdates, nil
}

func (s *strictService) Stop(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stops++
	if s.stops > 1 {
		return errors.New("Stop called more than once")
	}
	if s.statusUpdates != nil {
		close(s.statusUpdates)
		s.statusUpdates = nil
	}
	s.running = false
	return nil
}

func (s *strictService) stopCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stops
}

func (s *strictService) isRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// TestSupervisor_DeleteThenRegisterStopsInstanceOnce covers the update shape pg
// and terminal use: a manager deletes its entry and registers a replacement in
// one transaction. The registration collapses the pending removal, so the
// retirement is what stops the superseded instance, and it must be the only
// thing that stops it.
func TestSupervisor_DeleteThenRegisterStopsInstanceOnce(t *testing.T) {
	h := newTestHarness(t)
	h.start(context.Background())
	defer h.stop()

	const serviceID = "test:scope"

	original := newStrictService()
	registerInstanceWithDeps(h, serviceID, original, true, nil)
	awaitCondition(t, "original to start", original.isRunning)

	// The manager's Delete followed by its Add, in one transaction.
	replacement := newStrictService()
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRemove,
		Path:   serviceID,
	})
	h.sup.handleEvent(event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   serviceID,
		Data: &supervisor.Entry{
			Service: replacement,
			Config: supervisor.LifecycleConfig{
				AutoStart:    true,
				StartTimeout: 5 * time.Second,
				StopTimeout:  5 * time.Second,
			},
		},
	})
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})

	awaitCondition(t, "replacement to start", replacement.isRunning)
	awaitCondition(t, "superseded instance to stop", func() bool { return !original.isRunning() })

	require.Equal(t, 1, original.stopCount(),
		"the superseded instance must be stopped exactly once")
	require.Equal(t, 0, replacement.stopCount(),
		"the adopted instance must not be stopped")
	h.assertServiceState(serviceID, supervisor.StatusRunning)
}
