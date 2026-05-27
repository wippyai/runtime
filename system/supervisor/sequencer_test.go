// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type operationEvent struct {
	id      string
	isStart bool
}

type executableService struct {
	*testService
	eventChan chan<- operationEvent
	id        string
}

func newTestController(id string, eventChan chan<- operationEvent) *executableService {
	return &executableService{
		testService: newTestService(),
		id:          id,
		eventChan:   eventChan,
	}
}

func (s *executableService) Start() error {
	s.eventChan <- operationEvent{id: s.id, isStart: true}
	_, err := s.testService.Start(context.Background())
	if err != nil {
		return err
	}

	return nil
}

func (s *executableService) Stop() error {
	s.eventChan <- operationEvent{id: s.id, isStart: false}
	return s.testService.Stop(context.Background())
}

type blockingControllable struct {
	releaseCh <-chan struct{}
	eventCh   chan<- operationEvent
	id        string
	once      sync.Once
}

func (c *blockingControllable) Start() error {
	c.once.Do(func() {
		c.eventCh <- operationEvent{id: c.id, isStart: true}
	})
	<-c.releaseCh
	return nil
}

func (c *blockingControllable) Stop() error { return nil }

func (c *blockingControllable) startMayCompleteInBackground() bool { return true }

type delayedBackgroundControllable struct {
	releaseCh    <-chan struct{}
	stateChanged chan struct{}
	eventCh      chan<- operationEvent
	id           string
	delay        time.Duration
	background   atomic.Bool
	once         sync.Once
}

func (c *delayedBackgroundControllable) Start() error {
	c.once.Do(func() {
		c.eventCh <- operationEvent{id: c.id, isStart: true}
		go func() {
			time.Sleep(c.delay)
			c.background.Store(true)
			select {
			case c.stateChanged <- struct{}{}:
			default:
			}
		}()
	})
	<-c.releaseCh
	return nil
}

func (c *delayedBackgroundControllable) Stop() error { return nil }

func (c *delayedBackgroundControllable) startMayCompleteInBackground() bool {
	return c.background.Load()
}

func (c *delayedBackgroundControllable) startStateChanged() <-chan struct{} {
	return c.stateChanged
}

type startFailingControllable struct {
	err     error
	eventCh chan<- operationEvent
	id      string
}

func (c *startFailingControllable) Start() error {
	c.eventCh <- operationEvent{id: c.id, isStart: true}
	return c.err
}

func (c *startFailingControllable) Stop() error { return nil }

// Collect events for exact number of expected events
func collectEvents(t *testing.T, events chan operationEvent, count int, expectStart bool) []string {
	result := make([]string, 0)
	for i := 0; i < count; i++ {
		event := <-events
		require.Equal(t, expectStart, event.isStart,
			"expected %v operation, got %v for %s",
			expectStart, event.isStart, event.id)
		result = append(result, event.id)
	}
	return result
}

// verifyOrderedGroups verifies that groups of operations occur in the specified order,
// allowing any order within each group
func verifyOrderedGroups(t *testing.T, events chan operationEvent, groups [][]string, isStart bool) {
	seen := make(map[string]bool)

	for _, group := range groups {
		// Collect all events for current group
		received := make([]string, len(group))
		for i := 0; i < len(group); i++ {
			event := <-events
			require.Equal(t, isStart, event.isStart,
				"expected %v operation, got %v for %s",
				isStart, event.isStart, event.id)

			// Verify we haven't seen this service before
			require.False(t, seen[event.id],
				"service %s appeared out of order", event.id)

			received[i] = event.id
			seen[event.id] = true
		}

		// Sort both slices for comparison
		sort.Strings(received)
		expected := make([]string, len(group))
		copy(expected, group)
		sort.Strings(expected)

		require.Equal(t, expected, received,
			"incorrect services in operation group")
	}
}

func TestSequencer_BasicDependencyOrder(t *testing.T) {
	logger := zap.NewNop()
	sp := newSequencer(logger)

	events := make(chan operationEvent, 10)

	// Spawn test services with dependencies:
	// A -> B -> C (A depends on B, B depends on C)
	services := map[string]*executableService{
		"service-a": newTestController("service-a", events),
		"service-b": newTestController("service-b", events),
		"service-c": newTestController("service-c", events),
	}

	ops := []operation{
		{
			kind:         opStart,
			id:           "service-a",
			controller:   services["service-a"],
			dependencies: []string{"service-b"},
		},
		{
			kind:         opStart,
			id:           "service-b",
			controller:   services["service-b"],
			dependencies: []string{"service-c"},
		},
		{
			kind:         opStart,
			id:           "service-c",
			controller:   services["service-c"],
			dependencies: []string{},
		},
	}

	// execute operations
	ctx := context.Background()
	err := sp.transition(ctx, ops...)
	require.NoError(t, err)

	// Read start events in order
	startOrder := make([]string, 0)
	for i := 0; i < len(services); i++ {
		event := <-events
		require.True(t, event.isStart, "expected start event")
		startOrder = append(startOrder, event.id)
	}

	// Verify services started in correct order
	expectedStartOrder := []string{"service-c", "service-b", "service-a"}
	require.Equal(t, expectedStartOrder, startOrder, "Services started in wrong order")

	// Now stop all services
	stopOps := []operation{
		{
			kind:         opStop,
			id:           "service-a",
			controller:   services["service-a"],
			dependencies: []string{"service-b"},
		},
		{
			kind:         opStop,
			id:           "service-b",
			controller:   services["service-b"],
			dependencies: []string{"service-c"},
		},
		{
			kind:         opStop,
			id:           "service-c",
			controller:   services["service-c"],
			dependencies: []string{},
		},
	}

	err = sp.transition(ctx, stopOps...)
	require.NoError(t, err)

	// Read stop events in order
	stopOrder := make([]string, 0)
	for i := 0; i < len(services); i++ {
		event := <-events
		require.False(t, event.isStart, "expected stop event")
		stopOrder = append(stopOrder, event.id)
	}

	// Verify services stopped in correct order (reverse dependency order)
	expectedStopOrder := []string{"service-a", "service-b", "service-c"}
	require.Equal(t, expectedStopOrder, stopOrder, "Services stopped in wrong order")
}

func TestSequencer_ParallelExecution(t *testing.T) {
	logger := zap.NewNop()
	sp := newSequencer(logger)

	events := make(chan operationEvent, 10)

	// Spawn test services:
	// A -> C
	// B -> C
	// (A and B can start in parallel, C must start last)
	services := map[string]*executableService{
		"service-a": newTestController("service-a", events),
		"service-b": newTestController("service-b", events),
		"service-c": newTestController("service-c", events),
	}

	ops := []operation{
		{
			kind:         opStart,
			id:           "service-c",
			controller:   services["service-c"],
			dependencies: []string{"service-a", "service-b"},
		},
		{
			kind:         opStart,
			id:           "service-a",
			controller:   services["service-a"],
			dependencies: []string{},
		},
		{
			kind:         opStart,
			id:           "service-b",
			controller:   services["service-b"],
			dependencies: []string{},
		},
	}

	// execute operations
	ctx := context.Background()
	err := sp.transition(ctx, ops...)
	require.NoError(t, err)

	// Read first two events (should be A and B in any order)
	firstTwo := make([]string, 0)
	for i := 0; i < 2; i++ {
		event := <-events
		require.True(t, event.isStart, "expected start event")
		firstTwo = append(firstTwo, event.id)
	}

	// Sort first two for consistent comparison
	sort.Strings(firstTwo)
	require.Equal(t, []string{"service-a", "service-b"}, firstTwo,
		"First two operations should be service-a and service-b in any order")

	// Third event must be C
	event := <-events
	require.True(t, event.isStart, "expected start event")
	require.Equal(t, "service-c", event.id, "service-c must start last")
}

func TestSequencer_StartIgnoresDependenciesOutsideCurrentBatch(t *testing.T) {
	logger := zap.NewNop()
	sp := newSequencer(logger)

	events := make(chan operationEvent, 10)

	// This mirrors a registry commit where the current batch starts a consumer
	// and a queue, while both also depend on a service that was already started
	// by a previous commit. The external dependency must not become a synthetic
	// node in the transition graph; otherwise the topo-sort sees a node it can
	// never start and may report a false cycle/stall.
	ops := []operation{
		{
			kind:         opStart,
			id:           "worker",
			controller:   newTestController("worker", events),
			dependencies: []string{"queue", "external-host"},
		},
		{
			kind:         opStart,
			id:           "queue",
			controller:   newTestController("queue", events),
			dependencies: []string{"external-host"},
		},
	}

	err := sp.transition(context.Background(), ops...)
	require.NoError(t, err)

	first := <-events
	second := <-events
	require.Equal(t, "queue", first.id)
	require.Equal(t, "worker", second.id)
}

func TestSequencer_StartDoesNotBarrierIndependentBranches(t *testing.T) {
	logger := zap.NewNop()
	sp := newSequencer(logger)

	events := make(chan operationEvent, 10)
	releaseBlocked := make(chan struct{})
	defer close(releaseBlocked)

	// Mirrors an optional integration that is still retrying while an
	// independent required branch has all of its own prerequisites.
	ops := []operation{
		{
			kind:       opStart,
			id:         "optional-integration",
			controller: &blockingControllable{id: "optional-integration", eventCh: events, releaseCh: releaseBlocked},
			optional:   true,
		},
		{
			kind:       opStart,
			id:         "required-host",
			controller: newTestController("required-host", events),
		},
		{
			kind:         opStart,
			id:           "required-worker",
			controller:   newTestController("required-worker", events),
			dependencies: []string{"required-host"},
		},
	}

	done := make(chan error, 1)
	go func() {
		done <- sp.transition(context.Background(), ops...)
	}()

	started := map[string]bool{}
	require.Eventually(t, func() bool {
		for {
			select {
			case event := <-events:
				started[event.id] = true
			default:
				return started["required-worker"]
			}
		}
	}, 250*time.Millisecond, 10*time.Millisecond, "independent required branch should not wait for unrelated retrying resource")

	select {
	case err := <-done:
		require.NoError(t, err)
	default:
	}
}

func TestSequencer_StartRechecksBackgroundEligibilityOnStateChange(t *testing.T) {
	logger := zap.NewNop()
	sp := newSequencer(logger)

	events := make(chan operationEvent, 10)
	releaseBlocked := make(chan struct{})
	defer close(releaseBlocked)

	ops := []operation{
		{
			kind:     opStart,
			id:       "optional-integration",
			optional: true,
			controller: &delayedBackgroundControllable{
				id:           "optional-integration",
				eventCh:      events,
				releaseCh:    releaseBlocked,
				stateChanged: make(chan struct{}, 1),
				delay:        50 * time.Millisecond,
			},
		},
		{
			kind:       opStart,
			id:         "required-worker",
			controller: newTestController("required-worker", events),
		},
	}

	done := make(chan error, 1)
	go func() {
		done <- sp.transition(context.Background(), ops...)
	}()

	require.Eventually(t, func() bool {
		for {
			select {
			case event := <-events:
				if event.id == "required-worker" {
					return true
				}
			default:
				return false
			}
		}
	}, 250*time.Millisecond, 10*time.Millisecond)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("start sequence did not wake after background eligibility changed")
	}
}

func TestSequencer_StartFailureBlocksOnlyDependents(t *testing.T) {
	logger := zap.NewNop()
	sp := newSequencer(logger)

	events := make(chan operationEvent, 10)
	ops := []operation{
		{
			kind:       opStart,
			id:         "bad-dependency",
			controller: &startFailingControllable{id: "bad-dependency", eventCh: events, err: errors.New("dependency unavailable")},
		},
		{
			kind:         opStart,
			id:           "blocked-dependent",
			controller:   newTestController("blocked-dependent", events),
			dependencies: []string{"bad-dependency"},
		},
		{
			kind:       opStart,
			id:         "independent-host",
			controller: newTestController("independent-host", events),
		},
		{
			kind:         opStart,
			id:           "independent-worker",
			controller:   newTestController("independent-worker", events),
			dependencies: []string{"independent-host"},
		},
	}

	err := sp.transition(context.Background(), ops...)
	require.Error(t, err)

	started := map[string]bool{}
	for {
		select {
		case event := <-events:
			started[event.id] = true
		default:
			require.True(t, started["independent-worker"], "independent branch should start despite dependency failure")
			require.False(t, started["blocked-dependent"], "dependent branch must remain blocked by dependency failure")
			return
		}
	}
}

func TestSequencer_OptionalStartFailureDoesNotFailIndependentRequiredBranch(t *testing.T) {
	logger := zap.NewNop()
	sp := newSequencer(logger)

	events := make(chan operationEvent, 10)
	ops := []operation{
		{
			kind:       opStart,
			id:         "optional-integration",
			controller: &startFailingControllable{id: "optional-integration", eventCh: events, err: errors.New("integration unavailable")},
			optional:   true,
		},
		{
			kind:       opStart,
			id:         "required-worker",
			controller: newTestController("required-worker", events),
		},
	}

	err := sp.transition(context.Background(), ops...)
	require.NoError(t, err)

	started := map[string]bool{}
	for {
		select {
		case event := <-events:
			started[event.id] = true
		default:
			require.True(t, started["optional-integration"])
			require.True(t, started["required-worker"])
			return
		}
	}
}

func TestSequencer_RequiredBlockerFailsTransitionWithoutBlockingIndependentBranch(t *testing.T) {
	logger := zap.NewNop()
	sp := newSequencer(logger)

	events := make(chan operationEvent, 10)
	ops := []operation{
		{
			kind:       opStart,
			id:         "blocked-required",
			controller: newTestController("blocked-required", events),
			blockers:   []string{"missing-required-service"},
		},
		{
			kind:       opStart,
			id:         "independent-required",
			controller: newTestController("independent-required", events),
		},
	}

	err := sp.transition(context.Background(), ops...)
	require.Error(t, err)
	require.Contains(t, err.Error(), "service startup blocked by missing dependencies")

	event := <-events
	require.Equal(t, "independent-required", event.id)
	require.True(t, event.isStart)
}

func TestSequencer_OptionalBlockerDoesNotFailTransition(t *testing.T) {
	logger := zap.NewNop()
	sp := newSequencer(logger)

	events := make(chan operationEvent, 10)
	ops := []operation{
		{
			kind:       opStart,
			id:         "blocked-optional",
			controller: newTestController("blocked-optional", events),
			blockers:   []string{"missing-optional-service"},
			optional:   true,
		},
		{
			kind:       opStart,
			id:         "independent-required",
			controller: newTestController("independent-required", events),
		},
	}

	err := sp.transition(context.Background(), ops...)
	require.NoError(t, err)

	select {
	case event := <-events:
		require.Equal(t, "independent-required", event.id)
	case <-time.After(time.Second):
		t.Fatal("independent required operation did not start")
	}

	select {
	case event := <-events:
		t.Fatalf("blocked optional operation should not start, got %s", event.id)
	default:
	}
}

func TestSequencer_MixedOperations(t *testing.T) {
	logger := zap.NewNop()
	sp := newSequencer(logger)

	events := make(chan operationEvent, 10)

	// Spawn services for mixed start/stop operations
	services := map[string]*executableService{
		"start-1": newTestController("start-1", events),
		"start-2": newTestController("start-2", events),
		"stop-1":  newTestController("stop-1", events),
		"stop-2":  newTestController("stop-2", events),
	}

	// Pre-start services that need to be stopped
	ctx := context.Background()
	for _, id := range []string{"stop-1", "stop-2"} {
		_, err := services[id].testService.Start(ctx) // Use testService directly to avoid event
		require.NoError(t, err)
	}

	ops := []operation{
		{
			kind:         opStart,
			id:           "start-1",
			controller:   services["start-1"],
			dependencies: []string{},
		},
		{
			kind:         opStart,
			id:           "start-2",
			controller:   services["start-2"],
			dependencies: []string{"start-1"},
		},
		{
			kind:         opStop,
			id:           "stop-1",
			controller:   services["stop-1"],
			dependencies: []string{"stop-2"},
		},
		{
			kind:         opStop,
			id:           "stop-2",
			controller:   services["stop-2"],
			dependencies: []string{},
		},
	}

	err := sp.transition(ctx, ops...)
	require.NoError(t, err)

	// Read all events in order
	stopOrder := make([]string, 0)
	startOrder := make([]string, 0)
	// First two should be stops
	for i := 0; i < 2; i++ {
		event := <-events
		require.False(t, event.isStart, "expected stop event")
		stopOrder = append(stopOrder, event.id)
	}

	// Next two should be starts
	for i := 0; i < 2; i++ {
		event := <-events
		require.True(t, event.isStart, "expected start event")
		startOrder = append(startOrder, event.id)
	}

	// Verify stop operations happen first
	expectedStopOrder := []string{"stop-1", "stop-2"}
	require.Equal(t, expectedStopOrder, stopOrder, "Incorrect stop order")

	// Verify start operations happen after stops
	expectedStartOrder := []string{"start-1", "start-2"}
	require.Equal(t, expectedStartOrder, startOrder, "Incorrect start order")
}

func TestSequencer_OutOfOrderDependencies(t *testing.T) {
	logger := zap.NewNop()
	sp := newSequencer(logger)
	events := make(chan operationEvent, 10)

	// Spawn test services and register them in reverse dependency order
	// Actual dependency chain: A -> B -> C
	// Registration order: C, A, B
	services := map[string]*executableService{
		"service-c": newTestController("service-c", events),
		"service-a": newTestController("service-a", events),
		"service-b": newTestController("service-b", events),
	}

	ops := []operation{
		{
			kind:         opStart,
			id:           "service-c",
			controller:   services["service-c"],
			dependencies: []string{},
		},
		{
			kind:         opStart,
			id:           "service-a",
			controller:   services["service-a"],
			dependencies: []string{"service-b"},
		},
		{
			kind:         opStart,
			id:           "service-b",
			controller:   services["service-b"],
			dependencies: []string{"service-c"},
		},
	}

	// execute operations
	ctx := context.Background()
	err := sp.transition(ctx, ops...)
	require.NoError(t, err)

	// Even though registered out of order, should start in correct dependency order
	startOrder := collectEvents(t, events, 3, true)
	require.Equal(t, []string{"service-c", "service-b", "service-a"}, startOrder,
		"Services should start in dependency order regardless of registration order")
}

func TestSequencer_ComplexDependencyChain(t *testing.T) {
	logger := zap.NewNop()
	sp := newSequencer(logger)
	events := make(chan operationEvent, 20)

	// Spawn dependency chain with parallel groups:
	// Level 1 (parallel): A1, A2
	// Level 2: B (depends on A1, A2)
	// Level 3 (parallel): C1, C2 (both depend on B)
	// Level 4: D (depends on C1, C2)
	services := map[string]*executableService{
		"service-a1": newTestController("service-a1", events),
		"service-a2": newTestController("service-a2", events),
		"service-b":  newTestController("service-b", events),
		"service-c1": newTestController("service-c1", events),
		"service-c2": newTestController("service-c2", events),
		"service-d":  newTestController("service-d", events),
	}

	// Register in mixed order
	ops := []operation{
		{
			kind:         opStart,
			id:           "service-d",
			controller:   services["service-d"],
			dependencies: []string{"service-c1", "service-c2"},
		},
		{
			kind:         opStart,
			id:           "service-c1",
			controller:   services["service-c1"],
			dependencies: []string{"service-b"},
		},
		{
			kind:         opStart,
			id:           "service-b",
			controller:   services["service-b"],
			dependencies: []string{"service-a1", "service-a2"},
		},
		{
			kind:         opStart,
			id:           "service-c2",
			controller:   services["service-c2"],
			dependencies: []string{"service-b"},
		},
		{
			kind:         opStart,
			id:           "service-a1",
			controller:   services["service-a1"],
			dependencies: []string{},
		},
		{
			kind:         opStart,
			id:           "service-a2",
			controller:   services["service-a2"],
			dependencies: []string{},
		},
	}

	ctx := context.Background()
	err := sp.transition(ctx, ops...)
	require.NoError(t, err)

	// Verify start sequence
	startGroups := [][]string{
		{"service-a1", "service-a2"}, // Level 1: parallel
		{"service-b"},                // Level 2: single
		{"service-c1", "service-c2"}, // Level 3: parallel
		{"service-d"},                // Level 4: single
	}
	verifyOrderedGroups(t, events, startGroups, true)

	// Now test stopping - should be exact reverse of start order
	// For stopping, we invert the dependency relationships:
	// - If A depends on B for starting, then B depends on A for stopping
	stopOps := []operation{
		{
			kind:         opStop,
			id:           "service-d",
			controller:   services["service-d"],
			dependencies: []string{"service-c1", "service-c2"}, // Same deps as start
		},
		{
			kind:         opStop,
			id:           "service-c1",
			controller:   services["service-c1"],
			dependencies: []string{"service-b"}, // Same as start
		},
		{
			kind:         opStop,
			id:           "service-c2",
			controller:   services["service-c2"],
			dependencies: []string{"service-b"}, // Same as start
		},
		{
			kind:         opStop,
			id:           "service-b",
			controller:   services["service-b"],
			dependencies: []string{"service-a1", "service-a2"}, // Same as start
		},
		{
			kind:         opStop,
			id:           "service-a1",
			controller:   services["service-a1"],
			dependencies: []string{}, // Same as start
		},
		{
			kind:         opStop,
			id:           "service-a2",
			controller:   services["service-a2"],
			dependencies: []string{}, // Same as start
		},
	}

	err = sp.transition(ctx, stopOps...)
	require.NoError(t, err)

	// Verify stop sequence (reverse of start groups)
	stopGroups := [][]string{
		{"service-d"},                // Level 1: single
		{"service-c1", "service-c2"}, // Level 2: parallel
		{"service-b"},                // Level 3: single
		{"service-a1", "service-a2"}, // Level 4: parallel
	}
	verifyOrderedGroups(t, events, stopGroups, false)
}

// failingController is a mock controller that can simulate failures
type failingController struct {
	stopError  error
	id         string
	stopCalled bool
}

func newFailingController(id string, stopError error) *failingController {
	return &failingController{
		id:        id,
		stopError: stopError,
	}
}

func (f *failingController) Start() error {
	return nil
}

func (f *failingController) Stop() error {
	f.stopCalled = true
	return f.stopError
}

func TestSequencer_StopErrorAbortsRemainingLevels(t *testing.T) {
	logger := zap.NewNop()
	sp := newSequencer(logger)

	serviceA := newFailingController("service-a", errors.New("service-a failed to stop"))
	database := newFailingController("database", nil)

	ops := []operation{
		{
			kind:         opStop,
			id:           "service-a",
			controller:   serviceA,
			dependencies: []string{"database"},
		},
		{
			kind:         opStop,
			id:           "database",
			controller:   database,
			dependencies: []string{},
		},
	}

	ctx := context.Background()
	err := sp.transition(ctx, ops...)

	require.Error(t, err, "Expected error from service-a failure")
	require.Contains(t, err.Error(), "service-a", "Error should mention service-a")

	require.True(t, serviceA.stopCalled, "ServiceA Stop() should have been called")

	// BUG: Database Stop() is never called because service-a error aborts sequencer
	require.True(t, database.stopCalled, "Database Stop() should have been called despite service-a failure")
}

func TestSequencer_StopCollectsMultipleLevelErrors(t *testing.T) {
	logger := zap.NewNop()
	sp := newSequencer(logger)

	serviceA := newFailingController("service-a", errors.New("service-a failed"))
	serviceB := newFailingController("service-b", nil)
	database := newFailingController("database", errors.New("database failed"))

	ops := []operation{
		{
			kind:         opStop,
			id:           "service-a",
			controller:   serviceA,
			dependencies: []string{"database"},
		},
		{
			kind:         opStop,
			id:           "service-b",
			controller:   serviceB,
			dependencies: []string{"database"},
		},
		{
			kind:         opStop,
			id:           "database",
			controller:   database,
			dependencies: []string{},
		},
	}

	ctx := context.Background()
	err := sp.transition(ctx, ops...)

	require.Error(t, err, "Expected errors from service-a and database")

	require.True(t, serviceA.stopCalled, "ServiceA should have been stopped")
	require.True(t, serviceB.stopCalled, "ServiceB should have been stopped")

	// BUG: Database is never called because service-a error aborts level processing
	require.True(t, database.stopCalled, "Database should have been stopped despite earlier failures")

	// After fix: error should mention both failures
	require.Contains(t, err.Error(), "service-a", "Error should mention service-a")
	// This will fail with current implementation but should pass after fix
}

func TestSequencer_StopPartialLevelFailure(t *testing.T) {
	logger := zap.NewNop()
	sp := newSequencer(logger)

	serviceA := newFailingController("service-a", nil)
	serviceB := newFailingController("service-b", errors.New("service-b failed"))
	serviceC := newFailingController("service-c", nil)

	ops := []operation{
		{
			kind:         opStop,
			id:           "service-a",
			controller:   serviceA,
			dependencies: []string{},
		},
		{
			kind:         opStop,
			id:           "service-b",
			controller:   serviceB,
			dependencies: []string{},
		},
		{
			kind:         opStop,
			id:           "service-c",
			controller:   serviceC,
			dependencies: []string{},
		},
	}

	ctx := context.Background()
	err := sp.transition(ctx, ops...)

	require.Error(t, err, "Expected error from service-b")
	require.Contains(t, err.Error(), "service-b", "Error should mention service-b")

	require.True(t, serviceA.stopCalled, "ServiceA should have been stopped")
	require.True(t, serviceB.stopCalled, "ServiceB should have been stopped")
	require.True(t, serviceC.stopCalled, "ServiceC should have been stopped")
}
