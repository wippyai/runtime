package runner

import (
	"context"
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
	eventbus "github.com/ponyruntime/pony/pkg/eventbus"
)

// testComponent represents a component that can be configured via registry eventbus.
type testComponent struct {
	mu              sync.RWMutex
	config          map[registry.ID]string
	rejectedConfigs map[registry.ID]bool // Tracks rejected configurations.
}

// newTestComponent creates a new testComponent.
func newTestComponent() *testComponent {
	return &testComponent{
		config:          make(map[registry.ID]string),
		rejectedConfigs: make(map[registry.ID]bool),
	}
}

// handleEvent handles registry eventbus and updates the component's configuration.
func (c *testComponent) handleEvent(bus events.Bus, evt events.Event) {
	if evt.System != registry.System {
		return // Ignore eventbus from other systems.
	}

	entry, ok := evt.Data.(registry.Entry)
	if !ok {
		fmt.Printf("Received event with unexpected data type. Expected registry.Entry, got %T\n", evt.Data)
		return // Ignore eventbus with incorrect data type.
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
			bus.Send(context.Background(), events.Event{
				System: registry.System,
				Kind:   registry.Reject,
				Data:   registry.Entry{ID: entry.ID},
			})
			return
		}

		c.config[entry.ID] = data
		bus.Send(context.Background(), events.Event{
			System: registry.System,
			Kind:   registry.Accept,
			Data:   registry.Entry{ID: entry.ID},
		})

	case registry.Delete:
		if _, exists := c.config[entry.ID]; exists {
			delete(c.config, entry.ID)
			bus.Send(context.Background(), events.Event{
				System: registry.System,
				Kind:   registry.Accept,
				Data:   registry.Entry{ID: entry.ID},
			})
		} else {
			// Mark as rejected even if it doesn't exist in the listener.
			c.rejectedConfigs[entry.ID] = true
			bus.Send(context.Background(), events.Event{
				System: registry.System,
				Kind:   registry.Reject,
				Data:   registry.Entry{ID: entry.ID},
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
	listener, err := eventbus.NewEventListener(ctx, bus, registry.System, "", component.handleEvent)
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

			bus := eventbus.NewBus(zap.NewNop())
			busRunner := NewBusRunner(bus, zap.NewNop())
			component := newTestComponent()
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

	bus := eventbus.NewBus(zap.NewNop())
	busRunner := NewBusRunner(bus, zap.NewNop())
	component := newTestComponent()
	componentClose := attachComponent(ctx, t, bus, component)
	defer componentClose()

	initialState := registry.State{} // Start with an empty state
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
