package supervisor

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type operationEvent struct {
	id      string
	isStart bool
}

type executableService struct {
	*testService
	id        string
	eventChan chan<- operationEvent
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

// Collect events for exact number of expected events
func collectEvents(t *testing.T, events chan operationEvent, count int, expectStart bool) []string {
	var result []string
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
	sp := NewSequencer(logger)

	events := make(chan operationEvent, 10)

	// Create test services with dependencies:
	// A -> B -> C (A depends on B, B depends on C)
	services := map[string]*executableService{
		"service-a": newTestController("service-a", events),
		"service-b": newTestController("service-b", events),
		"service-c": newTestController("service-c", events),
	}

	ops := []Operation{
		{
			Type:         OperationStart,
			ID:           "service-a",
			Controller:   services["service-a"],
			Dependencies: []string{"service-b"},
		},
		{
			Type:         OperationStart,
			ID:           "service-b",
			Controller:   services["service-b"],
			Dependencies: []string{"service-c"},
		},
		{
			Type:         OperationStart,
			ID:           "service-c",
			Controller:   services["service-c"],
			Dependencies: []string{},
		},
	}

	// execute operations
	ctx := context.Background()
	err := sp.Transition(ctx, ops...)
	require.NoError(t, err)

	// Read start events in order
	var startOrder []string
	for i := 0; i < len(services); i++ {
		event := <-events
		require.True(t, event.isStart, "expected start event")
		startOrder = append(startOrder, event.id)
	}

	// Verify services started in correct order
	expectedStartOrder := []string{"service-c", "service-b", "service-a"}
	require.Equal(t, expectedStartOrder, startOrder, "Services started in wrong order")

	// Now stop all services
	stopOps := []Operation{
		{
			Type:         OperationStop,
			ID:           "service-a",
			Controller:   services["service-a"],
			Dependencies: []string{"service-b"},
		},
		{
			Type:         OperationStop,
			ID:           "service-b",
			Controller:   services["service-b"],
			Dependencies: []string{"service-c"},
		},
		{
			Type:         OperationStop,
			ID:           "service-c",
			Controller:   services["service-c"],
			Dependencies: []string{},
		},
	}

	err = sp.Transition(ctx, stopOps...)
	require.NoError(t, err)

	// Read stop events in order
	var stopOrder []string
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
	sp := NewSequencer(logger)

	events := make(chan operationEvent, 10)

	// Create test services:
	// A -> C
	// B -> C
	// (A and B can start in parallel, C must start last)
	services := map[string]*executableService{
		"service-a": newTestController("service-a", events),
		"service-b": newTestController("service-b", events),
		"service-c": newTestController("service-c", events),
	}

	ops := []Operation{
		{
			Type:         OperationStart,
			ID:           "service-c",
			Controller:   services["service-c"],
			Dependencies: []string{"service-a", "service-b"},
		},
		{
			Type:         OperationStart,
			ID:           "service-a",
			Controller:   services["service-a"],
			Dependencies: []string{},
		},
		{
			Type:         OperationStart,
			ID:           "service-b",
			Controller:   services["service-b"],
			Dependencies: []string{},
		},
	}

	// execute operations
	ctx := context.Background()
	err := sp.Transition(ctx, ops...)
	require.NoError(t, err)

	// Read first two events (should be A and B in any order)
	var firstTwo []string
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

func TestSequencer_MixedOperations(t *testing.T) {
	logger := zap.NewNop()
	sp := NewSequencer(logger)

	events := make(chan operationEvent, 10)

	// Create services for mixed start/stop operations
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

	ops := []Operation{
		{
			Type:         OperationStart,
			ID:           "start-1",
			Controller:   services["start-1"],
			Dependencies: []string{},
		},
		{
			Type:         OperationStart,
			ID:           "start-2",
			Controller:   services["start-2"],
			Dependencies: []string{"start-1"},
		},
		{
			Type:         OperationStop,
			ID:           "stop-1",
			Controller:   services["stop-1"],
			Dependencies: []string{"stop-2"},
		},
		{
			Type:         OperationStop,
			ID:           "stop-2",
			Controller:   services["stop-2"],
			Dependencies: []string{},
		},
	}

	err := sp.Transition(ctx, ops...)
	require.NoError(t, err)

	// Read all events in order
	var stopOrder, startOrder []string
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
	sp := NewSequencer(logger)
	events := make(chan operationEvent, 10)

	// Create test services and register them in reverse dependency order
	// Actual dependency chain: A -> B -> C
	// Registration order: C, A, B
	services := map[string]*executableService{
		"service-c": newTestController("service-c", events),
		"service-a": newTestController("service-a", events),
		"service-b": newTestController("service-b", events),
	}

	ops := []Operation{
		{
			Type:         OperationStart,
			ID:           "service-c",
			Controller:   services["service-c"],
			Dependencies: []string{},
		},
		{
			Type:         OperationStart,
			ID:           "service-a",
			Controller:   services["service-a"],
			Dependencies: []string{"service-b"},
		},
		{
			Type:         OperationStart,
			ID:           "service-b",
			Controller:   services["service-b"],
			Dependencies: []string{"service-c"},
		},
	}

	// execute operations
	ctx := context.Background()
	err := sp.Transition(ctx, ops...)
	require.NoError(t, err)

	// Even though registered out of order, should start in correct dependency order
	startOrder := collectEvents(t, events, 3, true)
	require.Equal(t, []string{"service-c", "service-b", "service-a"}, startOrder,
		"Services should start in dependency order regardless of registration order")
}

func TestSequencer_ComplexDependencyChain(t *testing.T) {
	logger := zap.NewNop()
	sp := NewSequencer(logger)
	events := make(chan operationEvent, 20)

	// Create dependency chain with parallel groups:
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
	ops := []Operation{
		{
			Type:         OperationStart,
			ID:           "service-d",
			Controller:   services["service-d"],
			Dependencies: []string{"service-c1", "service-c2"},
		},
		{
			Type:         OperationStart,
			ID:           "service-c1",
			Controller:   services["service-c1"],
			Dependencies: []string{"service-b"},
		},
		{
			Type:         OperationStart,
			ID:           "service-b",
			Controller:   services["service-b"],
			Dependencies: []string{"service-a1", "service-a2"},
		},
		{
			Type:         OperationStart,
			ID:           "service-c2",
			Controller:   services["service-c2"],
			Dependencies: []string{"service-b"},
		},
		{
			Type:         OperationStart,
			ID:           "service-a1",
			Controller:   services["service-a1"],
			Dependencies: []string{},
		},
		{
			Type:         OperationStart,
			ID:           "service-a2",
			Controller:   services["service-a2"],
			Dependencies: []string{},
		},
	}

	ctx := context.Background()
	err := sp.Transition(ctx, ops...)
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
	stopOps := []Operation{
		{
			Type:         OperationStop,
			ID:           "service-d",
			Controller:   services["service-d"],
			Dependencies: []string{"service-c1", "service-c2"}, // Same deps as start
		},
		{
			Type:         OperationStop,
			ID:           "service-c1",
			Controller:   services["service-c1"],
			Dependencies: []string{"service-b"}, // Same as start
		},
		{
			Type:         OperationStop,
			ID:           "service-c2",
			Controller:   services["service-c2"],
			Dependencies: []string{"service-b"}, // Same as start
		},
		{
			Type:         OperationStop,
			ID:           "service-b",
			Controller:   services["service-b"],
			Dependencies: []string{"service-a1", "service-a2"}, // Same as start
		},
		{
			Type:         OperationStop,
			ID:           "service-a1",
			Controller:   services["service-a1"],
			Dependencies: []string{}, // Same as start
		},
		{
			Type:         OperationStop,
			ID:           "service-a2",
			Controller:   services["service-a2"],
			Dependencies: []string{}, // Same as start
		},
	}

	err = sp.Transition(ctx, stopOps...)
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
