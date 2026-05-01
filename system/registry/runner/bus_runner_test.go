// SPDX-License-Identifier: MPL-2.0

package runner

import (
	"context"
	errors2 "errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/eventbus"
)

func internalDispatchPolicy() *KindDispatchPolicy {
	return NewKindDispatchPolicy([]registry.Kind{
		registry.EntryKind,
		registry.NamespaceDependency,
		registry.NamespaceRequirement,
		registry.NamespaceDefinition,
	})
}

// testComponent represents a component that can be configured via registry events.
type testComponent struct {
	bus             event.Bus
	config          map[registry.ID]string
	rejectedConfigs map[registry.ID]bool
	mu              sync.RWMutex
}

// newTestComponent creates a new testComponent.
func newTestComponent(bus event.Bus) *testComponent {
	return &testComponent{
		bus:             bus,
		config:          make(map[registry.ID]string),
		rejectedConfigs: make(map[registry.ID]bool),
	}
}

// handleEvent handles registry events and updates the component's configuration.
func (c *testComponent) handleEvent(evt event.Event) {
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
	case registry.EntryCreate, registry.EntryUpdate:
		data, ok := entry.Data.Data().(string)
		if !ok {
			fmt.Printf("payload.Data is not of type string, got %T\n", entry.Data)
			return
		}

		// Reject configuration based on some criteria (e.g., value starts with "reject").
		if len(data) >= 6 && data[:6] == "reject" {
			c.rejectedConfigs[entry.ID] = true
			c.bus.Send(context.Background(), event.Event{
				System: registry.System,
				Kind:   registry.EntryReject,
				Path:   entry.ID.String(),
				Data:   entry,
			})
			return
		}

		c.config[entry.ID] = data
		c.bus.Send(context.Background(), event.Event{
			System: registry.System,
			Kind:   registry.EntryAccept,
			Path:   entry.ID.String(),
			Data:   entry,
		})

	case registry.EntryDelete:
		id := registry.ParseID("component/listener/lib1")
		if entry.ID == id {
			// Reject deletion of lib1 if app1 still exists
			c.rejectedConfigs[entry.ID] = true
			c.bus.Send(context.Background(), event.Event{
				System: registry.System,
				Kind:   registry.EntryReject,
				Path:   entry.ID.String(),
				Data:   fmt.Errorf("listener %s is used by: [app1]", entry.ID),
			})
			return
		}

		if _, exists := c.config[entry.ID]; exists {
			delete(c.config, entry.ID)
			c.bus.Send(context.Background(), event.Event{
				System: registry.System,
				Kind:   registry.EntryAccept,
				Path:   entry.ID.String(),
				Data:   entry,
			})
		} else {
			// Mark as rejected even if it doesn't exist in the listener.
			c.rejectedConfigs[entry.ID] = true
			c.bus.Send(context.Background(), event.Event{
				System: registry.System,
				Kind:   registry.EntryReject,
				Path:   entry.ID.String(),
				Data:   entry,
			})
		}
	}
}

// getConfig returns the current configuration value for a given Process.
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
func attachComponent(ctx context.Context, t *testing.T, bus event.Bus, component *testComponent) func() {
	// Listen for all kinds within the registry system.
	listener, err := eventbus.NewSubscriber(ctx, bus, registry.System, "", component.handleEvent)
	require.NoError(t, err, "Failed to create event listener for component")

	return func() {
		listener.Close()
	}
}

// createEntry creates registry entries with string payloads for tests.
//

func createEntry(id registry.ID, kind registry.Kind, data string) registry.Entry {
	return registry.Entry{
		ID:   id,
		Kind: kind,
		Data: payload.NewString(data),
	}
}

// setupTestEnvironment prepares a test environment with necessary components.
func setupTestEnvironment(t *testing.T) (context.Context, event.Bus, *BusRunner, *testComponent, func()) {
	ctx, cancel := context.WithCancel(ctxapi.NewRootContext())

	bus := eventbus.NewBus()
	awaitSvc := eventbus.NewAwaitService(bus)
	require.NoError(t, awaitSvc.Start(ctx))
	ctx = event.WithAwaitService(ctx, awaitSvc)

	busRunner := NewBusRunner(bus, zap.NewNop(), newTestBuilder(nil), WithDispatchPolicy(internalDispatchPolicy()))
	component := newTestComponent(bus)

	componentCleanup := attachComponent(ctx, t, bus, component)

	cleanup := func() {
		componentCleanup()
		_ = awaitSvc.Stop()
		cancel()
	}

	return ctx, bus, busRunner, component, cleanup
}

// setupEventListener creates an event listener for specific event kinds.
func setupEventListener(
	ctx context.Context,
	t *testing.T,
	bus event.Bus,
	kinds []event.Kind,
	wg *sync.WaitGroup,
	eventChan chan<- event.Event,
) func() {
	listener, err := eventbus.NewSubscriber(
		ctx, bus, registry.System, "registry.*",
		func(evt event.Event) {
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
func waitForEvents(wg *sync.WaitGroup, eventChan chan event.Event) []event.Event {
	wg.Wait()
	close(eventChan)

	events := make([]event.Event, 0)
	for evt := range eventChan {
		events = append(events, evt)
	}
	return events
}

func TestBusRunner_Operations(t *testing.T) {
	testCases := []struct {
		finalConfig map[registry.ID]string
		name        string
		changeSet   registry.ChangeSet
		rejected    []registry.ID
		finalState  registry.State
		expectError bool
	}{
		{
			name: "Spawn",
			changeSet: registry.ChangeSet{
				{
					Kind: registry.EntryCreate,
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
					Kind: registry.EntryCreate,
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
					Kind: registry.EntryCreate,
					Entry: createEntry(
						registry.ParseID("component/listener/key3"),
						"listener",
						"value3",
					),
				},
				{
					Kind: registry.EntryUpdate,
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
					Kind: registry.EntryCreate,
					Entry: createEntry(
						registry.ParseID("component/listener/key4"),
						"listener",
						"value4",
					),
				},
				{
					Kind:  registry.EntryDelete,
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
					Kind: registry.EntryCreate,
					Entry: createEntry(
						registry.ParseID("component/listener/a"),
						"listener",
						"valueA",
					),
				},
				{
					Kind: registry.EntryUpdate,
					Entry: createEntry(
						registry.ParseID("component/listener/a"),
						"listener",
						"updatedA",
					),
				},
				{
					Kind: registry.EntryCreate,
					Entry: createEntry(
						registry.ParseID("component/listener/b"),
						"listener",
						"reject_B",
					),
				},
				{
					Kind:  registry.EntryDelete,
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
					Kind: registry.EntryCreate,
					Entry: createEntry(
						registry.ParseID("component/listener/dup"),
						"listener",
						"value1",
					),
				},
				{
					Kind: registry.EntryCreate,
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
					Kind: registry.EntryCreate,
					Entry: createEntry(
						registry.ParseID("component/listener/key1"),
						"listener",
						"value1",
					),
				},
				{
					Kind: registry.EntryUpdate,
					Entry: registry.Entry{
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
					Kind: registry.EntryDelete,
					Entry: registry.Entry{
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

	initialState := registry.State{} // Launch with an empty state
	changeSet := registry.ChangeSet{
		{
			Kind: registry.EntryCreate,
			Entry: createEntry(
				registry.ParseID("component/listener/key1"),
				"listener",
				"value1",
			),
		},
		{
			Kind: registry.EntryCreate,
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
	eventChan := make(chan event.Event, 10)

	// Listen for Begin and Commit events
	listenerCleanup := setupEventListener(
		ctx,
		t,
		bus,
		[]event.Kind{registry.TxBegin, registry.TxCommit},
		&wg,
		eventChan,
	)
	defer listenerCleanup()

	initialState := registry.State{}
	changeSet := registry.ChangeSet{
		{
			Kind: registry.EntryCreate,
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
	assert.Equal(t, registry.TxBegin, receivedEvents[0].Kind, "First event should be Begin")
	assert.Equal(t, registry.TxCommit, receivedEvents[1].Kind, "Second event should be Commit")
}

func TestBusRunner_WaitsForTransactionAcknowledgements(t *testing.T) {
	ctx, cancel := context.WithCancel(ctxapi.NewRootContext())
	defer cancel()

	bus := eventbus.NewBus()
	awaitSvc := eventbus.NewAwaitService(bus)
	require.NoError(t, awaitSvc.Start(ctx))
	defer func() { _ = awaitSvc.Stop() }()
	ctx = event.WithAwaitService(ctx, awaitSvc)

	busRunner := NewBusRunner(
		bus,
		zap.NewNop(),
		newTestBuilder(nil),
		WithTransactionParticipants(func() []string { return []string{"tx-test-listener"} }),
		WithEventWaitTimeout(500*time.Millisecond),
	)

	entryID := registry.NewID("", "tx-wait")
	entry := registry.Entry{ID: entryID, Kind: "listener", Data: payload.NewString("ok")}

	commitSeen := make(chan event.Path, 1)
	releaseCommit := make(chan struct{})
	txSub, err := eventbus.NewSubscriber(ctx, bus, registry.System, "registry.(begin|commit|discard)", func(evt event.Event) {
		if evt.Kind == registry.TxCommit {
			commitSeen <- evt.Path
			<-releaseCommit
		}
		bus.Send(ctx, event.Event{
			System: registry.System,
			Kind:   registry.TxAccept,
			Path:   evt.Path + "/tx-test-listener",
		})
	})
	require.NoError(t, err)
	defer txSub.Close()

	entrySub, err := eventbus.NewSubscriber(ctx, bus, registry.System, registry.EntryCreate, func(evt event.Event) {
		bus.Send(ctx, event.Event{
			System: registry.System,
			Kind:   registry.EntryAccept,
			Path:   evt.Path,
		})
	})
	require.NoError(t, err)
	defer entrySub.Close()

	done := make(chan error, 1)
	go func() {
		_, err := busRunner.Transition(ctx, nil, registry.ChangeSet{
			{Kind: registry.EntryCreate, Entry: entry},
		})
		done <- err
	}()

	select {
	case <-commitSeen:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for commit event")
	}

	select {
	case err := <-done:
		t.Fatalf("transition completed before commit acknowledgement: %v", err)
	default:
	}

	close(releaseCommit)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for transition to finish")
	}
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
			Kind: registry.EntryCreate,
			Entry: registry.Entry{
				ID:   lib1ID,
				Kind: "listener",
				Data: payload.NewString("lib-data"),
				Meta: attrs.Bag{},
			},
		},
		{
			Kind: registry.EntryCreate,
			Entry: registry.Entry{
				ID:   app1ID,
				Kind: "listener",
				Data: payload.NewString("app-data"),
				Meta: attrs.Bag{
					registry.TagDependsOn: []string{lib1ID.String()},
				},
			},
		},
		{
			Kind: registry.EntryCreate,
			Entry: registry.Entry{
				ID:   endpoint1ID,
				Kind: "listener",
				Data: payload.NewString("reject_this"), // This will trigger rejection
				Meta: attrs.Bag{
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
	ctx, cancel := context.WithTimeout(ctxapi.NewRootContext(), time.Second*5)
	defer cancel()

	bus := eventbus.NewBus()
	awaitSvc := eventbus.NewAwaitService(bus)
	require.NoError(t, awaitSvc.Start(ctx))
	defer func() { _ = awaitSvc.Stop() }()
	ctx = event.WithAwaitService(ctx, awaitSvc)

	busRunner := NewBusRunner(bus, zap.NewNop(), newTestBuilder(nil), WithDispatchPolicy(internalDispatchPolicy()))
	expectedError := errors2.New("component configuration not allowed")

	// Spawn a test component specifically for error testing
	component := &testComponent{
		bus:             bus,
		config:          make(map[registry.ID]string),
		rejectedConfigs: make(map[registry.ID]bool),
	}

	// Set up dedicated error-testing listener
	listener, err := eventbus.NewSubscriber(ctx, bus, registry.System, "", func(evt event.Event) {
		if evt.System != registry.System || evt.Kind != registry.EntryCreate {
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

		bus.Send(context.Background(), event.Event{
			System: registry.System,
			Kind:   registry.EntryReject,
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
			Kind: registry.EntryCreate,
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
	eventChan := make(chan event.Event, 10)

	// Listen for Begin and Discard events
	listenerCleanup := setupEventListener(
		ctx,
		t,
		bus,
		[]event.Kind{registry.TxBegin, registry.TxDiscard},
		&wg,
		eventChan,
	)
	defer listenerCleanup()

	initialState := registry.State{}
	changeSet := registry.ChangeSet{
		{
			Kind: registry.EntryCreate,
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
	assert.Equal(t, registry.TxBegin, receivedEvents[0].Kind, "First event should be Begin")
	assert.Equal(t, registry.TxDiscard, receivedEvents[1].Kind, "Second event should be Discard")
}

func TestBusRunner_CustomEventWaitTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(ctxapi.NewRootContext())
	defer cancel()

	bus := eventbus.NewBus()
	awaitSvc := eventbus.NewAwaitService(bus)
	require.NoError(t, awaitSvc.Start(ctx))
	defer func() { _ = awaitSvc.Stop() }()
	ctx = event.WithAwaitService(ctx, awaitSvc)

	busRunner := NewBusRunner(
		bus,
		zap.NewNop(),
		newTestBuilder(nil),
		WithEventWaitTimeout(20*time.Millisecond),
	)

	initialState := registry.State{}
	changeSet := registry.ChangeSet{
		{
			Kind: registry.EntryCreate,
			Entry: createEntry(
				registry.ParseID("component/listener/timeout"),
				"listener",
				"value",
			),
		},
	}

	start := time.Now()
	_, err := busRunner.Transition(ctx, initialState, changeSet)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "event handler timeout")
	assert.Less(t, elapsed, 500*time.Millisecond)
}

// TestBusRunner_RollbackOrderWithResolver verifies that rollback deletes dependents before dependencies
// when using a resolver that can extract dependencies from metadata.
func TestBusRunner_RollbackOrderWithResolver(t *testing.T) {
	ctx, cancel := context.WithCancel(ctxapi.NewRootContext())
	defer cancel()

	bus := eventbus.NewBus()
	awaitSvc := eventbus.NewAwaitService(bus)
	require.NoError(t, awaitSvc.Start(ctx))
	defer func() { _ = awaitSvc.Stop() }()
	ctx = event.WithAwaitService(ctx, awaitSvc)

	// Create resolver with meta.server pattern (like HTTP components use)
	resolver := &testResolver{
		deps: map[string][]string{
			"app:router":  {"app:server"},
			"app:static":  {"app:server"},
			"app:handler": {"app:router"},
		},
	}

	busRunner := NewBusRunner(bus, zap.NewNop(), newTestBuilder(resolver), WithDispatchPolicy(internalDispatchPolicy()))

	// Track deletion order
	var mu sync.Mutex
	var deleteOrder []string

	// Component that tracks operation order and rejects the last one
	listener, err := eventbus.NewSubscriber(ctx, bus, registry.System, "", func(evt event.Event) {
		if evt.System != registry.System {
			return
		}

		entry, ok := evt.Data.(registry.Entry)
		if !ok {
			return
		}

		if entry.Kind != "http" {
			return
		}

		switch evt.Kind {
		case registry.EntryCreate:
			// Accept creates, but reject "app:handler" to trigger rollback
			if entry.ID.String() == "app:handler" {
				bus.Send(context.Background(), event.Event{
					System: registry.System,
					Kind:   registry.EntryReject,
					Path:   entry.ID.String(),
					Data:   errors2.New("handler rejected"),
				})
				return
			}
			bus.Send(context.Background(), event.Event{
				System: registry.System,
				Kind:   registry.EntryAccept,
				Path:   entry.ID.String(),
			})

		case registry.EntryDelete:
			mu.Lock()
			deleteOrder = append(deleteOrder, entry.ID.String())
			mu.Unlock()
			bus.Send(context.Background(), event.Event{
				System: registry.System,
				Kind:   registry.EntryAccept,
				Path:   entry.ID.String(),
			})
		}
	})
	require.NoError(t, err)
	defer listener.Close()

	// Create entries in dependency order: server -> router -> static -> handler
	serverID := registry.ParseID("app:server")
	routerID := registry.ParseID("app:router")
	staticID := registry.ParseID("app:static")
	handlerID := registry.ParseID("app:handler")

	changeSet := registry.ChangeSet{
		{
			Kind: registry.EntryCreate,
			Entry: registry.Entry{
				ID:   serverID,
				Kind: "http",
				Data: payload.NewString("server"),
			},
		},
		{
			Kind: registry.EntryCreate,
			Entry: registry.Entry{
				ID:   routerID,
				Kind: "http",
				Data: payload.NewString("router"),
				Meta: attrs.Bag{"server": serverID.String()},
			},
		},
		{
			Kind: registry.EntryCreate,
			Entry: registry.Entry{
				ID:   staticID,
				Kind: "http",
				Data: payload.NewString("static"),
				Meta: attrs.Bag{"server": serverID.String()},
			},
		},
		{
			Kind: registry.EntryCreate,
			Entry: registry.Entry{
				ID:   handlerID,
				Kind: "http",
				Data: payload.NewString("handler"),
				Meta: attrs.Bag{"router": routerID.String()},
			},
		},
	}

	_, err = busRunner.Transition(ctx, registry.State{}, changeSet)
	require.Error(t, err, "Should fail because handler is rejected")

	// Verify deletion order: dependents must be deleted before dependencies
	// Expected: router, static deleted before server (handler was never created)
	mu.Lock()
	defer mu.Unlock()

	require.Len(t, deleteOrder, 3, "Should delete server, router, and static")

	// Find positions
	serverPos := -1
	routerPos := -1
	staticPos := -1
	for i, id := range deleteOrder {
		switch id {
		case "app:server":
			serverPos = i
		case "app:router":
			routerPos = i
		case "app:static":
			staticPos = i
		}
	}

	// Router and static must be deleted before server
	assert.Greater(t, serverPos, routerPos, "router must be deleted before server, got order: %v", deleteOrder)
	assert.Greater(t, serverPos, staticPos, "static must be deleted before server, got order: %v", deleteOrder)
}

// testResolver implements registry.DependencyResolver for testing
type testResolver struct {
	deps map[string][]string
}

func (r *testResolver) Extract(entry registry.Entry) []string {
	if deps, ok := r.deps[entry.ID.String()]; ok {
		return deps
	}
	return nil
}

func (r *testResolver) RegisterPattern(_ registry.DependencyPattern) error {
	return nil
}

// testBuilder implements runnerBuilder for tests
type testBuilder struct {
	resolver *testResolver
}

func newTestBuilder(resolver *testResolver) *testBuilder {
	return &testBuilder{resolver: resolver}
}

func (b *testBuilder) ValidateOperation(state registry.StateMap, op registry.Operation) error {
	switch op.Kind {
	case registry.EntryCreate:
		if _, exists := state[op.Entry.ID]; exists {
			return fmt.Errorf("entry already exists: %s", op.Entry.ID)
		}
	case registry.EntryUpdate:
		if _, exists := state[op.Entry.ID]; !exists {
			return fmt.Errorf("entry not found: %s", op.Entry.ID)
		}
	case registry.EntryDelete:
		if _, exists := state[op.Entry.ID]; !exists {
			return fmt.Errorf("entry not found: %s", op.Entry.ID)
		}
	}
	return nil
}

func (b *testBuilder) ApplyOperation(state registry.StateMap, op registry.Operation) (registry.StateMap, error) {
	if err := b.ValidateOperation(state, op); err != nil {
		return state, err
	}

	newState := make(registry.StateMap, len(state))
	for k, v := range state {
		newState[k] = v
	}

	switch op.Kind {
	case registry.EntryCreate, registry.EntryUpdate:
		newState[op.Entry.ID] = op.Entry
	case registry.EntryDelete:
		delete(newState, op.Entry.ID)
	}

	return newState, nil
}

func (b *testBuilder) BuildDelta(from, to registry.State) (registry.ChangeSet, error) {
	fromState := make(registry.StateMap)
	for _, entry := range from {
		fromState[entry.ID] = entry
	}
	toState := make(registry.StateMap)
	for _, entry := range to {
		toState[entry.ID] = entry
	}

	var deleteOps, otherOps []registry.Operation

	// Find deletes
	for _, fromEntry := range from {
		if _, exists := toState[fromEntry.ID]; !exists {
			deleteOps = append(deleteOps, registry.Operation{
				Kind:  registry.EntryDelete,
				Entry: fromEntry,
			})
		}
	}

	// Find creates and updates
	for _, toEntry := range to {
		fromEntry, exists := fromState[toEntry.ID]
		if !exists {
			otherOps = append(otherOps, registry.Operation{
				Kind:  registry.EntryCreate,
				Entry: toEntry,
			})
		} else if fromEntry.Kind != toEntry.Kind || fromEntry.Data != toEntry.Data {
			otherOps = append(otherOps, registry.Operation{
				Kind:  registry.EntryUpdate,
				Entry: toEntry,
			})
		}
	}

	// Sort deletes by reverse dependency order (dependents first)
	if b.resolver != nil && len(deleteOps) > 0 {
		deleteOps = b.sortDeletesByDependency(deleteOps)
	}

	result := make(registry.ChangeSet, 0, len(deleteOps)+len(otherOps))
	result = append(result, deleteOps...)
	result = append(result, otherOps...)

	return result, nil
}

// sortDeletesByDependency sorts delete operations so dependents are deleted before dependencies
func (b *testBuilder) sortDeletesByDependency(ops []registry.Operation) []registry.Operation {
	if b.resolver == nil {
		return ops
	}

	// Build dependency graph
	depCount := make(map[string]int)
	dependents := make(map[string][]string)

	for _, op := range ops {
		id := op.Entry.ID.String()
		if _, ok := depCount[id]; !ok {
			depCount[id] = 0
		}

		deps := b.resolver.Extract(op.Entry)
		for _, dep := range deps {
			depCount[id]++
			dependents[dep] = append(dependents[dep], id)
		}
	}

	// Topological sort (Kahn's algorithm) - items with no dependencies first
	var queue []string
	for _, op := range ops {
		id := op.Entry.ID.String()
		if depCount[id] == 0 {
			queue = append(queue, id)
		}
	}

	var sorted []string
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		sorted = append(sorted, id)

		for _, dependent := range dependents[id] {
			depCount[dependent]--
			if depCount[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	// Reverse for delete order (dependents before dependencies)
	for i, j := 0, len(sorted)-1; i < j; i, j = i+1, j-1 {
		sorted[i], sorted[j] = sorted[j], sorted[i]
	}

	// Build result
	opMap := make(map[string]registry.Operation)
	for _, op := range ops {
		opMap[op.Entry.ID.String()] = op
	}

	result := make([]registry.Operation, 0, len(ops))
	for _, id := range sorted {
		if op, ok := opMap[id]; ok {
			result = append(result, op)
		}
	}

	return result
}
