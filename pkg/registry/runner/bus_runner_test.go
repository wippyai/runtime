package runner

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	eventbus "github.com/ponyruntime/pony/pkg/eventbus"
)

// testComponent represents a component that can be configured via registry eventbus.
type testComponent struct {
	bus             events.Bus
	mu              sync.RWMutex
	config          map[registry.ID]string
	rejectedConfigs map[registry.ID]bool
}

// newTestComponent creates a new testComponent.
func newTestComponent(bus events.Bus) *testComponent {
	return &testComponent{
		bus:             bus,
		config:          make(map[registry.ID]string),
		rejectedConfigs: make(map[registry.ID]bool),
	}
}

// handleEvent handles registry eventbus and updates the component's configuration.
func (c *testComponent) handleEvent(evt events.Event) {
	if evt.System != registry.System {
		return // Ignore events from other systems.
	}

	entry, ok := evt.Data.(registry.Entry)
	if !ok {
		return // Ignore events with incorrect data type.
	}

	if entry.Kind != "listener" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	switch evt.Kind {
	case registry.Create, registry.Update:
		p, ok := entry.Data.(payload.Payload)
		if !ok {
			fmt.Printf("entry.Data is not of type payload.Payload, got %T\n", entry.Data)
			return
		}

		data, ok := p.Data().(string)
		if !ok {
			fmt.Printf("payload.Data is not of type string, got %T\n", entry.Data)
			return
		}

		// Reject configuration based on some criteria (e.g., value starts with "reject").
		if len(data) >= 6 && data[:6] == "reject" {
			c.rejectedConfigs[entry.ID] = true
			c.bus.Send(context.Background(), events.Event{
				System: registry.System,
				Kind:   registry.Reject,
				Path:   events.Path(entry.ID), // Add Path field
				Data:   entry,
			})
			return
		}

		c.config[entry.ID] = data
		c.bus.Send(context.Background(), events.Event{
			System: registry.System,
			Kind:   registry.Accept,
			Path:   events.Path(entry.ID), // Add Path field
			Data:   entry,
		})

	case registry.Delete:
		if _, exists := c.config[entry.ID]; exists {
			delete(c.config, entry.ID)
			c.bus.Send(context.Background(), events.Event{
				System: registry.System,
				Kind:   registry.Accept,
				Path:   events.Path(entry.ID), // Add Path field
				Data:   entry,
			})
		} else {
			// Mark as rejected even if it doesn't exist in the listener.
			c.rejectedConfigs[entry.ID] = true
			c.bus.Send(context.Background(), events.Event{
				System: registry.System,
				Kind:   registry.Reject,
				Path:   events.Path(entry.ID), // Add Path field
				Data:   entry,
			})
		}

	default:
		return
	}
}

// getConfig returns the current configuration value for a given path.
func (c *testComponent) getConfig(path registry.ID) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	val, ok := c.config[path]
	return val, ok
}

// wasRejected checks if a configuration was rejected.
func (c *testComponent) wasRejected(path registry.ID) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.rejectedConfigs[path]
	return ok
}

// attachComponent sets up an event listener for the testComponent.
func attachComponent(ctx context.Context, t *testing.T, bus events.Bus, component *testComponent) func() {
	// Listen for all kinds within the registry system.
	listener, err := eventbus.NewSubscriber(ctx, bus, registry.System, "", component.handleEvent)
	if err != nil {
		t.Fatalf("Failed to create event listener for component: %v", err)
	}

	return func() {
		listener.Close()
	}
}

// createEntry creates registry entries with string payloads for tests.
func createEntry(path registry.ID, kind registry.Kind, data string) registry.Entry {
	return registry.Entry{
		ID:   path,
		Kind: kind,
		Data: payload.NewString(data),
	}
}

func TestBusRunner_Operations(t *testing.T) {
	testCases := []struct {
		name        string
		changeSet   registry.ChangeSet
		expectError bool
		finalConfig map[registry.ID]string
		rejected    []registry.ID
		finalState  registry.State
	}{
		{
			name: "Create",
			changeSet: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: createEntry(
						"component/listener/key1",
						"listener",
						"value1",
					),
				},
			},
			expectError: false,
			finalConfig: map[registry.ID]string{
				"component/listener/key1": "value1",
			},
			rejected: []registry.ID{},
			finalState: registry.State{
				createEntry("component/listener/key1", "listener", "value1"),
			},
		},
		{
			name: "CreateAndReject",
			changeSet: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: createEntry(
						"component/listener/key2",
						"listener",
						"reject_this",
					),
				},
			},
			expectError: true,
			finalConfig: map[registry.ID]string{},
			rejected:    []registry.ID{"component/listener/key2"},
			finalState:  registry.State{},
		},
		{
			name: "Update",
			changeSet: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: createEntry(
						"component/listener/key3",
						"listener",
						"value3",
					),
				},
				{
					Kind: registry.Update,
					Entry: createEntry(
						"component/listener/key3",
						"listener",
						"updatedValue3",
					),
				},
			},
			expectError: false,
			finalConfig: map[registry.ID]string{
				"component/listener/key3": "updatedValue3",
			},
			rejected: []registry.ID{},
			finalState: registry.State{
				createEntry("component/listener/key3", "listener", "updatedValue3"),
			},
		},
		{
			name: "Delete",
			changeSet: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: createEntry(
						"component/listener/key4",
						"listener",
						"value4",
					),
				},
				{
					Kind:  registry.Delete,
					Entry: registry.Entry{ID: "component/listener/key4", Kind: "listener"},
				},
			},
			expectError: false,
			finalConfig: map[registry.ID]string{},
			rejected:    []registry.ID{},
			finalState:  registry.State{},
		},
		{
			name: "DeleteRejected",
			changeSet: registry.ChangeSet{
				{
					Kind:  registry.Delete,
					Entry: registry.Entry{ID: "component/listener/nonexistent", Kind: "listener"},
				},
			},
			expectError: true, // Expect an error because deletion is rejected.
			finalConfig: map[registry.ID]string{},
			rejected:    []registry.ID{"component/listener/nonexistent"},
			finalState:  registry.State{}, // State should remain unchanged.
		},
		{
			name: "MixedOperations",
			changeSet: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: createEntry(
						"component/listener/a",
						"listener",
						"valueA",
					),
				},
				{
					Kind: registry.Update,
					Entry: createEntry(
						"component/listener/a",
						"listener",
						"updatedA",
					),
				},
				{
					Kind: registry.Create,
					Entry: createEntry(
						"component/listener/b",
						"listener",
						"reject_B",
					),
				},
				{
					Kind:  registry.Delete,
					Entry: registry.Entry{ID: "component/listener/a", Kind: "listener"},
				},
			},
			expectError: true, // Expect an error because of the rejection
			finalConfig: map[registry.ID]string{},
			rejected:    []registry.ID{"component/listener/b"},
			finalState:  registry.State{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			bus := eventbus.NewBus()
			busRunner := NewBusRunner(bus, zap.NewNop())
			component := newTestComponent(bus)
			componentClose := attachComponent(ctx, t, bus, component)
			defer componentClose()

			initialState := registry.State{}

			finalState, err := busRunner.Transition(ctx, initialState, tc.changeSet)
			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			// Verify the component's listener.
			for path, expectedValue := range tc.finalConfig {
				actualValue, ok := component.getConfig(path)
				assert.True(t, ok, "Expected listener not found: %s", path)
				assert.Equal(t, expectedValue, actualValue, "Incorrect value for listener: %s", path)
			}

			// Verify rejected configs.
			for _, rejectedPath := range tc.rejected {
				assert.True(t, component.wasRejected(rejectedPath), "Expected listener to be rejected: %s", rejectedPath)
			}

			// Verify the number of configs.
			assert.Equal(t, len(tc.finalConfig), len(component.config), "Unexpected number of configs")

			// Verify the final state.
			assert.ElementsMatch(t, tc.finalState, finalState, "Final state does not match expected state")
		})
	}
}

func TestBusRunner_RollbackOnSecondOperationFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bus := eventbus.NewBus()
	busRunner := NewBusRunner(bus, zap.NewNop())
	component := newTestComponent(bus)
	componentClose := attachComponent(ctx, t, bus, component)
	defer componentClose()

	initialState := registry.State{} // StartComponent with an empty state
	changeSet := registry.ChangeSet{
		{
			Kind: registry.Create,
			Entry: createEntry(
				"component/listener/key1",
				"listener",
				"value1",
			),
		},
		{
			Kind: registry.Create,
			Entry: createEntry(
				"component/listener/key2",
				"listener",
				"reject_this", // This operation will be rejected
			),
		},
	}

	finalState, err := busRunner.Transition(ctx, initialState, changeSet)

	// 1. Expect an error because the second operation is rejected
	require.Error(t, err)

	// 2. Verify the component's listener is empty (rolled back)
	assert.Equal(t, 0, len(component.config), "Config should be empty after rollback")

	// 3. Verify that key2 was rejected
	assert.True(t, component.wasRejected("component/listener/key2"), "component/listener/key2 should be rejected")

	// 4. Verify the final state is empty (rolled back)
	assert.Empty(t, finalState, "Final state should be empty after rollback")
}

func TestBusRunner_BeginAndCommitEvents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bus := eventbus.NewBus()
	busRunner := NewBusRunner(bus, zap.NewNop())
	component := newTestComponent(bus)

	// Use a WaitGroup to wait for the listener to process events
	var wg sync.WaitGroup

	// opChan to receive events in the listener
	eventChan := make(chan events.Event, 10)

	// Attach the listener to the bus
	listener, err := eventbus.NewSubscriber(
		ctx, bus, registry.System, "registry.*",
		func(evt events.Event) {
			if evt.System == registry.System && (evt.Kind == registry.Begin || evt.Kind == registry.Commit) {
				eventChan <- evt
				wg.Done()

				if evt.Kind == registry.Commit || evt.Kind == registry.Discard {
					close(eventChan)
				}
			}
		},
	)
	require.NoError(t, err)
	defer listener.Close()

	componentClose := attachComponent(ctx, t, bus, component)
	defer componentClose()

	initialState := registry.State{}
	changeSet := registry.ChangeSet{
		{
			Kind: registry.Create,
			Entry: createEntry(
				"component/listener/key1",
				"listener",
				"value1",
			),
		},
	}

	// Expect 2 events: Begin and Commit
	wg.Add(2)

	_, err = busRunner.Transition(ctx, initialState, changeSet)
	require.NoError(t, err)

	// wait for the listener to process the events
	wg.Wait()

	// Collect the received events
	var receivedEvents []events.Event
	for evt := range eventChan {
		receivedEvents = append(receivedEvents, evt)
	}

	// Assert that we received exactly 2 events
	assert.Equal(t, 2, len(receivedEvents), "Expected 2 events (Begin and Commit)")

	// Verify that the first event is Begin
	assert.Equal(t, registry.Begin, receivedEvents[0].Kind, "First event should be Begin")

	// Verify that the second event is Commit
	assert.Equal(t, registry.Commit, receivedEvents[1].Kind, "Second event should be Commit")
}

func TestBusRunner_BeginAndDiscardEvents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bus := eventbus.NewBus()
	busRunner := NewBusRunner(bus, zap.NewNop())
	component := newTestComponent(bus)

	// Use a WaitGroup to wait for the listener to process events
	var wg sync.WaitGroup

	// opChan to receive events in the listener, buffered to prevent blocking
	eventChan := make(chan events.Event, 10)

	// Attach the listener to the bus to listen for Begin and Discard events
	listener, err := eventbus.NewSubscriber(
		ctx, bus, registry.System, "registry.*",
		func(evt events.Event) {
			if evt.System == registry.System && (evt.Kind == registry.Begin || evt.Kind == registry.Discard) {
				eventChan <- evt
				wg.Done()

				if evt.Kind == registry.Commit || evt.Kind == registry.Discard {
					close(eventChan)
				}
			}
		},
	)
	require.NoError(t, err)
	defer listener.Close()

	componentClose := attachComponent(ctx, t, bus, component)
	defer componentClose()

	initialState := registry.State{}
	changeSet := registry.ChangeSet{
		{
			Kind: registry.Create,
			Entry: createEntry(
				"component/listener/key1",
				"listener",
				"reject_this", // This will cause a rejection and thus a Discard event
			),
		},
	}

	// Expect 2 events: Begin and Discard
	wg.Add(2)

	_, err = busRunner.Transition(ctx, initialState, changeSet)
	require.Error(t, err) // We expect an error because the operation is rejected

	// wait for the listener to process the events
	wg.Wait()

	// Collect the received events
	var receivedEvents []events.Event
	for evt := range eventChan {
		receivedEvents = append(receivedEvents, evt)
	}

	// Assert that we received exactly 2 events
	assert.Equal(t, 2, len(receivedEvents), "Expected 2 events (Begin and Discard)")

	// Verify that the first event is Begin
	assert.Equal(t, registry.Begin, receivedEvents[0].Kind, "First event should be Begin")

	// Verify that the second event is Discard
	assert.Equal(t, registry.Discard, receivedEvents[1].Kind, "Second event should be Discard")
}

func TestBusRunner_ErrorPropagation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bus := eventbus.NewBus()
	busRunner := NewBusRunner(bus, zap.NewNop())

	expectedError := errors.New("component configuration not allowed")

	// Create a test component with modified behavior
	component := &testComponent{
		bus:             bus,
		config:          make(map[registry.ID]string),
		rejectedConfigs: make(map[registry.ID]bool),
	}

	// Set up event listener with modified behavior for this test
	listener, err := eventbus.NewSubscriber(ctx, bus, registry.System, "", func(evt events.Event) {
		if evt.System == registry.System && evt.Kind == registry.Create {
			entry, ok := evt.Data.(registry.Entry)
			if !ok {
				return
			}

			// Reject with custom error message
			component.rejectedConfigs[entry.ID] = true
			bus.Send(context.Background(), events.Event{
				System: registry.System,
				Kind:   registry.Reject,
				Path:   events.Path(entry.ID),
				Data:   expectedError, // send error instead of entry
			})
			return
		}
	})
	require.NoError(t, err)
	defer listener.Close()

	// Create a changeset that should trigger the rejection
	initialState := registry.State{}
	changeSet := registry.ChangeSet{
		{
			Kind: registry.Create,
			Entry: createEntry(
				"component/listener/error-test",
				"listener",
				"test-value",
			),
		},
	}

	// Run the transition
	_, err = busRunner.Transition(ctx, initialState, changeSet)

	// Verify that an error occurred and contains our message
	require.Error(t, err)
	assert.Contains(t, err.Error(), expectedError.Error())

	// Verify the error was propagated
	assert.True(t, component.wasRejected("component/listener/error-test"),
		"Expected component/listener/error-test to be rejected")

	// Verify no config was stored
	assert.Equal(t, 0, len(component.config),
		"No config should be stored after rejection")
}
