package runtime

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/runtime"
	"sync"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/pkg/eventbus"
	payload2 "github.com/ponyruntime/pony/pkg/payload"
	"github.com/ponyruntime/pony/pkg/payload/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Mock Runtime
type mockRuntime struct {
	functions map[registry.ID]runtime.FunctionConfig
	libraries map[registry.ID]runtime.LibraryConfig
	mu        sync.RWMutex
}

func (m *mockRuntime) AddFunction(id registry.ID, config runtime.FunctionConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.functions[id] = config
	return nil
}

func (m *mockRuntime) UpdateFunction(id registry.ID, config runtime.FunctionConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.functions[id] = config
	return nil
}

func (m *mockRuntime) AddLibrary(id registry.ID, config runtime.LibraryConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.libraries[id] = config
	return nil
}

func (m *mockRuntime) UpdateLibrary(id registry.ID, config runtime.LibraryConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.libraries[id] = config
	return nil
}

func (m *mockRuntime) Delete(id registry.ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.functions, id)
	delete(m.libraries, id)
	return nil
}

// Helper function to setup test environment
func setupRuntimeTest(t *testing.T) (*Service, *eventbus.Bus, *mockRuntime) {
	logger := zap.NewNop()
	bus := eventbus.NewBus(logger)

	tr := payload2.NewTranscoder()
	json.Register(tr)

	mockRt := &mockRuntime{
		functions: make(map[registry.ID]runtime.FunctionConfig),
		libraries: make(map[registry.ID]runtime.LibraryConfig),
	}
	manager := Init(bus, mockRt, tr, logger)
	return manager, bus, mockRt
}

// eventCollector helps manage event expectations in tests
type runtimeEventCollector struct {
	t        *testing.T
	bus      events.Bus
	ctx      context.Context
	cancel   context.CancelFunc
	listener *eventbus.Subscriber
	eventCh  chan events.Event
}

// newEventCollector creates a new eventCollector instance
func newRuntimeEventCollector(t *testing.T, bus events.Bus) *runtimeEventCollector {
	ctx, cancel := context.WithCancel(context.Background())
	return &runtimeEventCollector{
		t:       t,
		bus:     bus,
		ctx:     ctx,
		cancel:  cancel,
		eventCh: make(chan events.Event, 100), // Buffered channel to prevent blocking
	}
}

// Listen starts listening for events matching the given system and kinds
func (e *runtimeEventCollector) Listen(system events.System, kinds ...events.Kind) {
	// Create handler function that will send events to our channel
	handlerFunc := func(evt events.Event) {
		for _, k := range kinds {
			if evt.Kind == k {
				select {
				case e.eventCh <- evt:
				case <-e.ctx.Done():
				}
				break
			}
		}
	}

	// Create new subscriber with the handler
	listener, err := eventbus.NewSubscriber(e.ctx, e.bus, system, "*", handlerFunc)
	require.NoError(e.t, err, "Failed to create event subscriber")

	e.listener = listener
}

// AssertEventCount asserts that the expected number of events have been collected
func (e *runtimeEventCollector) AssertEventCount(expectedCount int) {
	events := make([]events.Event, 0, expectedCount)
	timeoutCh := time.After(5 * time.Second)

	for len(events) < expectedCount {
		select {
		case evt := <-e.eventCh:
			events = append(events, evt)
		case <-timeoutCh:
			require.Fail(e.t, fmt.Sprintf("Timeout waiting for events. Expected %d events, got %d",
				expectedCount, len(events)))
			return
		case <-e.ctx.Done():
			require.Fail(e.t, "Context cancelled while waiting for events")
			return
		}
	}
}

// AssertEvent asserts that an event with the expected path and kind exists
func (e *runtimeEventCollector) AssertEvent(expectedPath events.Path, expectedKind events.Kind) {
	timeoutCh := time.After(5 * time.Second)

	for {
		select {
		case evt := <-e.eventCh:
			if evt.Path == expectedPath && evt.Kind == expectedKind {
				assert.Equal(e.t, expectedPath, evt.Path)
				assert.Equal(e.t, expectedKind, evt.Kind)
				return
			}
		case <-timeoutCh:
			require.Fail(e.t, fmt.Sprintf("Timeout waiting for event with path: %s, kind: %s",
				string(expectedPath), string(expectedKind)))
			return
		case <-e.ctx.Done():
			require.Fail(e.t, "Context cancelled while waiting for event")
			return
		}
	}
}

// Close cleans up the eventCollector
func (e *runtimeEventCollector) Close() {
	if e.listener != nil {
		e.listener.Close()
	}
	e.cancel()
	close(e.eventCh)
}

func TestRuntimeService_Lifecycle(t *testing.T) {
	t.Run("start and stop", func(t *testing.T) {
		manager, _, _ := setupRuntimeTest(t)

		// Test Start
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)

		// Test Stop
		err = manager.Stop()
		require.NoError(t, err)
	})

	t.Run("double start", func(t *testing.T) {
		manager, _, _ := setupRuntimeTest(t)

		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)

		// Second start should fail
		err = manager.Start(ctx)
		assert.Error(t, err)

		manager.Stop()
	})
}
func TestRuntimeService_FunctionOperations(t *testing.T) {
	t.Run("create function", func(t *testing.T) {
		manager, bus, mockRt := setupRuntimeTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newRuntimeEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Accept)

		// Send create function event
		functionConfig := map[string]interface{}{
			"meta": map[string]interface{}{
				"runtime": "testRuntime",
			},
			"source": "function() {}",
			"method": "testMethod",
		}

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-function",
			Data: registry.Entry{
				ID:   "test-function",
				Kind: runtime.KindFunction,
				Data: payload.New(functionConfig),
			},
		})

		collector.AssertEvent("test-function", registry.Accept)
		_, exists := mockRt.functions["test-function"]
		assert.True(t, exists, "Function should exist in mock runtime")
	})

	t.Run("update function", func(t *testing.T) {
		manager, bus, mockRt := setupRuntimeTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newRuntimeEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Accept)

		// First create the function
		functionConfig := map[string]interface{}{
			"meta": map[string]interface{}{
				"runtime": "testRuntime",
			},
			"source": "function() {}",
			"method": "testMethod",
		}

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-function",
			Data: registry.Entry{
				ID:   "test-function",
				Kind: runtime.KindFunction,
				Data: payload.New(functionConfig),
			},
		})
		collector.AssertEvent("test-function", registry.Accept)

		// Then update it
		updatedConfig := map[string]interface{}{
			"meta": map[string]interface{}{
				"runtime": "testRuntime",
			},
			"source": "updatedFunction() {}",
			"method": "updatedMethod",
		}

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Update,
			Path:   "test-function",
			Data: registry.Entry{
				ID:   "test-function",
				Kind: runtime.KindFunction,
				Data: payload.New(updatedConfig),
			},
		})

		collector.AssertEvent("test-function", registry.Accept)
		updatedFunction, exists := mockRt.functions["test-function"]
		assert.True(t, exists, "Function should exist in mock runtime")
		assert.Equal(t, "updatedFunction() {}", updatedFunction.Source)
		assert.Equal(t, "updatedMethod", updatedFunction.Method)
	})
}
