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
	eventbus "github.com/ponyruntime/pony/pkg/events"
)

// testComponent represents a component that can be configured via registry events.
type testComponent struct {
	mu              sync.RWMutex
	config          map[registry.Path]string
	rejectedConfigs map[registry.Path]bool // Tracks rejected configurations.
}

// newTestComponent creates a new testComponent.
func newTestComponent() *testComponent {
	return &testComponent{
		config:          make(map[registry.Path]string),
		rejectedConfigs: make(map[registry.Path]bool),
	}
}

// handleEvent handles registry events and updates the component's configuration.
func (c *testComponent) handleEvent(bus events.Bus, evt events.Event) {
	if evt.System != registry.System {
		return // Ignore events from other systems.
	}

	entry, ok := evt.Data.(registry.Entry)
	if !ok {
		fmt.Printf("Received event with unexpected data type. Expected registry.Entry, got %T\n", evt.Data)
		return // Ignore events with incorrect data type.
	}

	if entry.Kind != "config" {
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
			c.rejectedConfigs[entry.Path] = true
			bus.Send(context.Background(), events.Event{
				System: registry.System,
				Kind:   registry.Reject,
				Data:   registry.Entry{Path: entry.Path},
			})
			return
		}

		c.config[entry.Path] = data
		bus.Send(context.Background(), events.Event{
			System: registry.System,
			Kind:   registry.Accept,
			Data:   registry.Entry{Path: entry.Path},
		})

	case registry.Delete:
		if _, exists := c.config[entry.Path]; exists {
			delete(c.config, entry.Path)
			bus.Send(context.Background(), events.Event{
				System: registry.System,
				Kind:   registry.Accept,
				Data:   registry.Entry{Path: entry.Path},
			})
		} else {
			// Mark as rejected even if it doesn't exist in the config.
			c.rejectedConfigs[entry.Path] = true
			bus.Send(context.Background(), events.Event{
				System: registry.System,
				Kind:   registry.Reject,
				Data:   registry.Entry{Path: entry.Path},
			})
		}

	default:
		return
	}
}

// getConfig returns the current configuration value for a given path.
func (c *testComponent) getConfig(path registry.Path) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	val, ok := c.config[path]
	return val, ok
}

// wasRejected checks if a configuration was rejected.
func (c *testComponent) wasRejected(path registry.Path) bool {
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
func createEntry(path registry.Path, kind registry.Kind, data string) registry.Entry {
	return registry.Entry{
		Path: path,
		Kind: kind,
		Data: payload.NewString(data),
	}
}

func TestBusRunner_Operations(t *testing.T) {
	testCases := []struct {
		name        string
		changeSet   registry.ChangeSet
		expectError bool
		finalConfig map[registry.Path]string
		rejected    []registry.Path
		finalState  registry.State
	}{
		{
			name: "Create",
			changeSet: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: createEntry(
						"component/config/key1",
						"config",
						"value1",
					),
				},
			},
			expectError: false,
			finalConfig: map[registry.Path]string{
				"component/config/key1": "value1",
			},
			rejected: []registry.Path{},
			finalState: registry.State{
				createEntry("component/config/key1", "config", "value1"),
			},
		},
		{
			name: "CreateAndReject",
			changeSet: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: createEntry(
						"component/config/key2",
						"config",
						"reject_this",
					),
				},
			},
			expectError: true,
			finalConfig: map[registry.Path]string{},
			rejected:    []registry.Path{"component/config/key2"},
			finalState:  registry.State{},
		},
		{
			name: "Update",
			changeSet: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: createEntry(
						"component/config/key3",
						"config",
						"value3",
					),
				},
				{
					Kind: registry.Update,
					Entry: createEntry(
						"component/config/key3",
						"config",
						"updatedValue3",
					),
				},
			},
			expectError: false,
			finalConfig: map[registry.Path]string{
				"component/config/key3": "updatedValue3",
			},
			rejected: []registry.Path{},
			finalState: registry.State{
				createEntry("component/config/key3", "config", "updatedValue3"),
			},
		},
		{
			name: "Delete",
			changeSet: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: createEntry(
						"component/config/key4",
						"config",
						"value4",
					),
				},
				{
					Kind:  registry.Delete,
					Entry: registry.Entry{Path: "component/config/key4", Kind: "config"},
				},
			},
			expectError: false,
			finalConfig: map[registry.Path]string{},
			rejected:    []registry.Path{},
			finalState:  registry.State{},
		},
		{
			name: "DeleteRejected",
			changeSet: registry.ChangeSet{
				{
					Kind:  registry.Delete,
					Entry: registry.Entry{Path: "component/config/nonexistent", Kind: "config"},
				},
			},
			expectError: true, // Expect an error because deletion is rejected.
			finalConfig: map[registry.Path]string{},
			rejected:    []registry.Path{"component/config/nonexistent"},
			finalState:  registry.State{}, // State should remain unchanged.
		},
		{
			name: "MixedOperations",
			changeSet: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: createEntry(
						"component/config/a",
						"config",
						"valueA",
					),
				},
				{
					Kind: registry.Update,
					Entry: createEntry(
						"component/config/a",
						"config",
						"updatedA",
					),
				},
				{
					Kind: registry.Create,
					Entry: createEntry(
						"component/config/b",
						"config",
						"reject_B",
					),
				},
				{
					Kind:  registry.Delete,
					Entry: registry.Entry{Path: "component/config/a", Kind: "config"},
				},
			},
			expectError: true, // Expect an error because of the rejection
			finalConfig: map[registry.Path]string{},
			rejected:    []registry.Path{"component/config/b"},
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

			// Verify the component's config.
			for path, expectedValue := range tc.finalConfig {
				actualValue, ok := component.getConfig(path)
				assert.True(t, ok, "Expected config not found: %s", path)
				assert.Equal(t, expectedValue, actualValue, "Incorrect value for config: %s", path)
			}

			// Verify rejected configs.
			for _, rejectedPath := range tc.rejected {
				assert.True(t, component.wasRejected(rejectedPath), "Expected config to be rejected: %s", rejectedPath)
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
				"component/config/key1",
				"config",
				"value1",
			),
		},
		{
			Kind: registry.Create,
			Entry: createEntry(
				"component/config/key2",
				"config",
				"reject_this", // This operation will be rejected
			),
		},
	}

	finalState, err := busRunner.Transition(ctx, initialState, changeSet)

	// 1. Expect an error because the second operation is rejected
	require.Error(t, err)

	// 2. Verify the component's config is empty (rolled back)
	assert.Equal(t, 0, len(component.config), "Config should be empty after rollback")

	// 3. Verify that key2 was rejected
	assert.True(t, component.wasRejected("component/config/key2"), "component/config/key2 should be rejected")

	// 4. Verify the final state is empty (rolled back)
	assert.Empty(t, finalState, "Final state should be empty after rollback")
}
