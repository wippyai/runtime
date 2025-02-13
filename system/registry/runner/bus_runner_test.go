package runner

import (
	"context"
	errors2 "errors"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"sync"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/system/eventbus"
)

// testComponent represents a component that can be configured via registry events.
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

// handleEvent handles registry events and updates the component's configuration.
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
		data, ok := entry.Data.Data().(string)
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
				Path:   entry.ID.String(),
				Data:   entry,
			})
			return
		}

		c.config[entry.ID] = data
		c.bus.Send(context.Background(), events.Event{
			System: registry.System,
			Kind:   registry.Accept,
			Path:   entry.ID.String(),
			Data:   entry,
		})

	case registry.Delete:
		id := registry.ParseID("component/listener/lib1")
		if entry.ID == id {
			// Reject deletion of lib1 if app1 still exists
			c.rejectedConfigs[entry.ID] = true
			c.bus.Send(context.Background(), events.Event{
				System: registry.System,
				Kind:   registry.Reject,
				Path:   entry.ID.String(),
				Data:   fmt.Errorf("listener %s is used by: [app1]", entry.ID),
			})
			return
		}

		if _, exists := c.config[entry.ID]; exists {
			delete(c.config, entry.ID)
			c.bus.Send(context.Background(), events.Event{
				System: registry.System,
				Kind:   registry.Accept,
				Path:   entry.ID.String(),
				Data:   entry,
			})
		} else {
			// Mark as rejected even if it doesn't exist in the listener.
			c.rejectedConfigs[entry.ID] = true
			c.bus.Send(context.Background(), events.Event{
				System: registry.System,
				Kind:   registry.Reject,
				Path:   entry.ID.String(),
				Data:   entry,
			})
		}
	}
}

// getConfig returns the current configuration value for a given ID.
func (c *testComponent) getConfig(id registry.ID) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	val, ok := c.config[id]
	return val, ok
}

// wasRejected checks if a configuration was rejected.
func (c *testComponent) wasRejected(id registry.ID) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.rejectedConfigs[id]
	return ok
}

// attachComponent sets up an event listener for the testComponent.
func attachComponent(ctx context.Context, t *testing.T, bus events.Bus, component *testComponent) func() {
	// Listen for all kinds within the registry system.
	listener, err := eventbus.NewSubscriber(ctx, bus, registry.System, "", component.handleEvent)
	require.NoError(t, err, "Failed to create event listener for component")

	return func() {
		listener.Close()
	}
}

// createEntry creates registry entries with string payloads for tests.
func createEntry(id registry.ID, kind registry.Kind, data string) registry.Entry {
	return registry.Entry{
		ID:   id,
		Kind: kind,
		Data: payload.NewString(data),
	}
}

// setupTestEnvironment prepares a test environment with necessary components.
func setupTestEnvironment(t *testing.T) (context.Context, events.Bus, *BusRunner, *testComponent, func()) {
	ctx, cancel := context.WithCancel(context.Background())

	bus := eventbus.NewBus()
	busRunner := NewBusRunner(bus, zap.NewNop())
	component := newTestComponent(bus)

	componentCleanup := attachComponent(ctx, t, bus, component)

	cleanup := func() {
		componentCleanup()
		cancel()
	}

	return ctx, bus, busRunner, component, cleanup
}

// setupEventListener creates an event listener for specific event kinds.
func setupEventListener(
	ctx context.Context,
	t *testing.T,
	bus events.Bus,
	kinds []events.Kind,
	wg *sync.WaitGroup,
	eventChan chan<- events.Event,
) func() {
	listener, err := eventbus.NewSubscriber(
		ctx, bus, registry.System, "registry.*",
		func(evt events.Event) {
			if evt.System == registry.System {
				for _, kind := range kinds {
					if evt.Kind == kind {
						eventChan <- evt
						wg.Done()
						return
					}
				}
			}
		},
	)
	require.NoError(t, err)

	return listener.Close
}

// waitForEvents waits for a specific number of events and returns them.
func waitForEvents(wg *sync.WaitGroup, eventChan chan events.Event) []events.Event {
	wg.Wait()
	close(eventChan)

	var events []events.Event
	for evt := range eventChan {
		events = append(events, evt)
	}
	return events
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
			name: "Spawn",
			changeSet: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: createEntry(
						registry.ParseID("component/listener/key1"),
						"listener",
						"value1",
					),
				},
			},
			expectError: false,
			finalConfig: map[registry.ID]string{
				registry.ParseID("component/listener/key1"): "value1",
			},
			rejected: []registry.ID{},
			finalState: registry.State{
				createEntry(registry.ParseID("component/listener/key1"), "listener", "value1"),
			},
		},
		{
			name: "CreateAndReject",
			changeSet: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: createEntry(
						registry.ParseID("component/listener/key2"),
						"listener",
						"reject_this",
					),
				},
			},
			expectError: true,
			finalConfig: map[registry.ID]string{},
			rejected:    []registry.ID{registry.ParseID("component/listener/key2")},
			finalState:  registry.State{},
		},
		{
			name: "Update",
			changeSet: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: createEntry(
						registry.ParseID("component/listener/key3"),
						"listener",
						"value3",
					),
				},
				{
					Kind: registry.Update,
					Entry: createEntry(
						registry.ParseID("component/listener/key3"),
						"listener",
						"updatedValue3",
					),
				},
			},
			expectError: false,
			finalConfig: map[registry.ID]string{
				registry.ParseID("component/listener/key3"): "updatedValue3",
			},
			rejected: []registry.ID{},
			finalState: registry.State{
				createEntry(registry.ParseID("component/listener/key3"), "listener", "updatedValue3"),
			},
		},
		{
			name: "Delete",
			changeSet: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: createEntry(
						registry.ParseID("component/listener/key4"),
						"listener",
						"value4",
					),
				},
				{
					Kind:  registry.Delete,
					Entry: registry.Entry{ID: registry.ParseID("component/listener/key4"), Kind: "listener"},
				},
			},
			expectError: false,
			finalConfig: map[registry.ID]string{},
			rejected:    []registry.ID{},
			finalState:  registry.State{},
		},
		{
			name: "MixedOperations",
			changeSet: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: createEntry(
						registry.ParseID("component/listener/a"),
						"listener",
						"valueA",
					),
				},
				{
					Kind: registry.Update,
					Entry: createEntry(
						registry.ParseID("component/listener/a"),
						"listener",
						"updatedA",
					),
				},
				{
					Kind: registry.Create,
					Entry: createEntry(
						registry.ParseID("component/listener/b"),
						"listener",
						"reject_B",
					),
				},
				{
					Kind:  registry.Delete,
					Entry: registry.Entry{ID: registry.ParseID("component/listener/a"), Kind: "listener"},
				},
			},
			expectError: true, // Expect an error because of the rejection
			finalConfig: map[registry.ID]string{},
			rejected:    []registry.ID{registry.ParseID("component/listener/b")},
			finalState:  registry.State{},
		},
		{
			name: "DuplicateCreate",
			changeSet: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: createEntry(
						registry.ParseID("component/listener/dup"),
						"listener",
						"value1",
					),
				},
				{
					Kind: registry.Create,
					Entry: createEntry(
						registry.ParseID("component/listener/dup"),
						"listener",
						"value2",
					),
				},
			},
			expectError: true,
			finalConfig: map[registry.ID]string{},
			rejected:    []registry.ID{}, // does not reach component
			finalState:  registry.State{},
		},
		{
			name: "UpdateWithKindChange",
			changeSet: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: createEntry(
						registry.ParseID("component/listener/key1"),
						"listener",
						"value1",
					),
				},
				{
					Kind: registry.Update,
					Entry: registry.Entry{
						ID:   registry.ParseID("component/listener/key1"),
						Kind: "different_kind",
						Data: payload.NewString("value2"),
					},
				},
			},
			expectError: true,
			finalConfig: map[registry.ID]string{},
			rejected:    []registry.ID{}, // does not reach component
			finalState:  registry.State{},
		},
		{
			name: "DeleteNonExistent",
			changeSet: registry.ChangeSet{
				{
					Kind: registry.Delete,
					Entry: registry.Entry{
						ID:   registry.ParseID("component/listener/nonexistent"),
						Kind: "listener",
					},
				},
			},
			expectError: true,
			finalConfig: map[registry.ID]string{},
			rejected:    []registry.ID{}, // does not reach component
			finalState:  registry.State{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _, busRunner, component, cleanup := setupTestEnvironment(t)
			defer cleanup()

			initialState := registry.State{}
			finalState, err := busRunner.Transition(ctx, initialState, tc.changeSet)

			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			// Verify the component's configuration
			for id, expectedValue := range tc.finalConfig {
				actualValue, ok := component.getConfig(id)
				assert.True(t, ok, "Expected configuration not found: %s", id)
				assert.Equal(t, expectedValue, actualValue, "Incorrect value for configuration: %s", id)
			}

			// Verify rejected configs
			for _, rejectedID := range tc.rejected {
				assert.True(t, component.wasRejected(rejectedID), "Expected configuration to be rejected: %s", rejectedID)
			}

			// Verify the number of configs
			assert.Equal(t, len(tc.finalConfig), len(component.config), "Unexpected number of configurations")

			// Verify the final state
			assert.ElementsMatch(t, tc.finalState, finalState, "Final state does not match expected state")
		})
	}
}

func TestBusRunner_RollbackOnSecondOperationFailure(t *testing.T) {
	ctx, _, busRunner, component, cleanup := setupTestEnvironment(t)
	defer cleanup()

	initialState := registry.State{} // Start with an empty state
	changeSet := registry.ChangeSet{
		{
			Kind: registry.Create,
			Entry: createEntry(
				registry.ParseID("component/listener/key1"),
				"listener",
				"value1",
			),
		},
		{
			Kind: registry.Create,
			Entry: createEntry(
				registry.ParseID("component/listener/key2"),
				"listener",
				"reject_this", // This operation will be rejected
			),
		},
	}

	finalState, err := busRunner.Transition(ctx, initialState, changeSet)

	// 1. Expect an error because the second operation is rejected
	require.Error(t, err)

	// 2. Verify the component's configuration is empty (rolled back)
	assert.Equal(t, 0, len(component.config), "Config should be empty after rollback")

	// 3. Verify that key2 was rejected
	assert.True(t, component.wasRejected(registry.ParseID("component/listener/key2")),
		"component/listener/key2 should be rejected")

	// 4. Verify the final state is empty (rolled back)
	assert.Empty(t, finalState, "Final state should be empty after rollback")
}

func TestBusRunner_BeginAndCommitEvents(t *testing.T) {
	ctx, bus, busRunner, _, cleanup := setupTestEnvironment(t)
	defer cleanup()

	var wg sync.WaitGroup
	eventChan := make(chan events.Event, 10)

	// Listen for Begin and Commit events
	listenerCleanup := setupEventListener(
		ctx,
		t,
		bus,
		[]events.Kind{registry.Begin, registry.Commit},
		&wg,
		eventChan,
	)
	defer listenerCleanup()

	initialState := registry.State{}
	changeSet := registry.ChangeSet{
		{
			Kind: registry.Create,
			Entry: createEntry(
				registry.ParseID("component/listener/key1"),
				"listener",
				"value1",
			),
		},
	}

	// Expect 2 events: Begin and Commit
	wg.Add(2)

	_, err := busRunner.Transition(ctx, initialState, changeSet)
	require.NoError(t, err)

	receivedEvents := waitForEvents(&wg, eventChan)

	assert.Equal(t, 2, len(receivedEvents), "Expected 2 events (Begin and Commit)")
	assert.Equal(t, registry.Begin, receivedEvents[0].Kind, "First event should be Begin")
	assert.Equal(t, registry.Commit, receivedEvents[1].Kind, "Second event should be Commit")
}

func TestBusRunner_RollbackOrder(t *testing.T) {
	ctx, _, busRunner, _, cleanup := setupTestEnvironment(t)
	defer cleanup()

	lib1ID := registry.ParseID("component/listener/lib1")
	app1ID := registry.ParseID("component/listener/app1")
	endpoint1ID := registry.ParseID("component/listener/endpoint1")

	initialState := registry.State{}
	changeSet := registry.ChangeSet{
		{
			Kind: registry.Create,
			Entry: registry.Entry{
				ID:   lib1ID,
				Kind: "listener",
				Data: payload.NewString("lib-data"),
				Meta: registry.Metadata{},
			},
		},
		{
			Kind: registry.Create,
			Entry: registry.Entry{
				ID:   app1ID,
				Kind: "listener",
				Data: payload.NewString("app-data"),
				Meta: registry.Metadata{
					registry.TagDependsOn: []string{lib1ID.String()},
				},
			},
		},
		{
			Kind: registry.Create,
			Entry: registry.Entry{
				ID:   endpoint1ID,
				Kind: "listener",
				Data: payload.NewString("reject_this"), // This will trigger rejection
				Meta: registry.Metadata{
					registry.TagDependsOn: []string{app1ID.String()},
				},
			},
		},
	}

	finalState, err := busRunner.Transition(ctx, initialState, changeSet)
	require.Error(t, err)

	// Check that lib1 remains but app1 is gone after rollback
	var hasLib, hasApp bool
	for _, entry := range finalState {
		if entry.ID == lib1ID {
			hasLib = true
		}
		if entry.ID == app1ID {
			hasApp = true
		}
	}

	assert.True(t, hasLib, "lib1 should remain since deletion would be rejected")
	assert.False(t, hasApp, "app1 should be gone after rollback")
}

func TestBusRunner_ErrorPropagation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	bus := eventbus.NewBus()
	busRunner := NewBusRunner(bus, zap.NewNop())
	expectedError := errors2.New("component configuration not allowed")

	// Spawn a test component specifically for error testing
	component := &testComponent{
		bus:             bus,
		config:          make(map[registry.ID]string),
		rejectedConfigs: make(map[registry.ID]bool),
	}

	// Set up dedicated error-testing listener
	listener, err := eventbus.NewSubscriber(ctx, bus, registry.System, "", func(evt events.Event) {
		if evt.System != registry.System || evt.Kind != registry.Create {
			return
		}

		entry, ok := evt.Data.(registry.Entry)
		if !ok {
			return
		}

		// Reject with custom error message
		component.mu.Lock()
		component.rejectedConfigs[entry.ID] = true
		component.mu.Unlock()

		bus.Send(context.Background(), events.Event{
			System: registry.System,
			Kind:   registry.Reject,
			Path:   entry.ID.String(),
			Data:   expectedError,
		})
	})
	require.NoError(t, err)
	defer listener.Close()

	// Spawn a changeset that should trigger the rejection
	initialState := registry.State{}
	changeSet := registry.ChangeSet{
		{
			Kind: registry.Create,
			Entry: createEntry(
				registry.ParseID("component/listener/error-test"),
				"listener",
				"test-value",
			),
		},
	}

	// Run the transition
	_, err = busRunner.Transition(ctx, initialState, changeSet)

	// Verify error propagation
	require.Error(t, err)
	assert.Contains(t, err.Error(), expectedError.Error())

	// Verify the error was recorded
	assert.True(t, component.wasRejected(registry.ParseID("component/listener/error-test")),
		"Expected component/listener/error-test to be rejected")

	// Verify no config was stored
	assert.Equal(t, 0, len(component.config),
		"No config should be stored after rejection")
}

func TestBusRunner_BeginAndDiscardEvents(t *testing.T) {
	ctx, bus, busRunner, _, cleanup := setupTestEnvironment(t)
	defer cleanup()

	var wg sync.WaitGroup
	eventChan := make(chan events.Event, 10)

	// Listen for Begin and Discard events
	listenerCleanup := setupEventListener(
		ctx,
		t,
		bus,
		[]events.Kind{registry.Begin, registry.Discard},
		&wg,
		eventChan,
	)
	defer listenerCleanup()

	initialState := registry.State{}
	changeSet := registry.ChangeSet{
		{
			Kind: registry.Create,
			Entry: createEntry(
				registry.ParseID("component/listener/key1"),
				"listener",
				"reject_this", // This will cause a rejection and thus a Discard event
			),
		},
	}

	// Expect 2 events: Begin and Discard
	wg.Add(2)

	_, err := busRunner.Transition(ctx, initialState, changeSet)
	require.Error(t, err) // We expect an error because the operation is rejected

	receivedEvents := waitForEvents(&wg, eventChan)

	assert.Equal(t, 2, len(receivedEvents), "Expected 2 events (Begin and Discard)")
	assert.Equal(t, registry.Begin, receivedEvents[0].Kind, "First event should be Begin")
	assert.Equal(t, registry.Discard, receivedEvents[1].Kind, "Second event should be Discard")
}
