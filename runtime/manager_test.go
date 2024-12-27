package runtime

import (
	"context"
	"errors"
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

func TestRuntimeService_LibraryOperations(t *testing.T) {
	t.Run("create library", func(t *testing.T) {
		manager, bus, mockRt := setupRuntimeTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newRuntimeEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Accept)

		// Send create library event
		libraryConfig := map[string]interface{}{
			"meta": map[string]interface{}{
				"runtime": "testRuntime",
			},
			"source": "library code",
		}

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-library",
			Data: registry.Entry{
				ID:   "test-library",
				Kind: runtime.KindLibrary,
				Data: payload.New(libraryConfig),
			},
		})

		collector.AssertEvent("test-library", registry.Accept)
		_, exists := mockRt.libraries["test-library"]
		assert.True(t, exists, "Library should exist in mock runtime")
	})

	t.Run("update library", func(t *testing.T) {
		manager, bus, mockRt := setupRuntimeTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newRuntimeEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Accept)

		// First create the library
		libraryConfig := map[string]interface{}{
			"meta": map[string]interface{}{
				"runtime": "testRuntime",
			},
			"source": "library code",
		}

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-library",
			Data: registry.Entry{
				ID:   "test-library",
				Kind: runtime.KindLibrary,
				Data: payload.New(libraryConfig),
			},
		})
		collector.AssertEvent("test-library", registry.Accept)

		// Then update it
		updatedConfig := map[string]interface{}{
			"meta": map[string]interface{}{
				"runtime": "testRuntime",
			},
			"source": "updated library code",
		}

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Update,
			Path:   "test-library",
			Data: registry.Entry{
				ID:   "test-library",
				Kind: runtime.KindLibrary,
				Data: payload.New(updatedConfig),
			},
		})

		collector.AssertEvent("test-library", registry.Accept)
		updatedLibrary, exists := mockRt.libraries["test-library"]
		assert.True(t, exists, "Library should exist in mock runtime")
		assert.Equal(t, "updated library code", updatedLibrary.Source)
	})

	t.Run("delete library", func(t *testing.T) {
		manager, bus, mockRt := setupRuntimeTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newRuntimeEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Accept)

		// First create the library
		libraryConfig := map[string]interface{}{
			"meta": map[string]interface{}{
				"runtime": "testRuntime",
			},
			"source": "library code",
		}

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-library",
			Data: registry.Entry{
				ID:   "test-library",
				Kind: runtime.KindLibrary,
				Data: payload.New(libraryConfig),
			},
		})
		collector.AssertEvent("test-library", registry.Accept)

		// Then delete it
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Delete,
			Path:   "test-library",
			Data: registry.Entry{
				ID:   "test-library",
				Kind: runtime.KindLibrary,
			},
		})

		collector.AssertEvent("test-library", registry.Accept)
		_, exists := mockRt.libraries["test-library"]
		assert.False(t, exists, "Library should not exist in mock runtime")
	})
	t.Run("delete function", func(t *testing.T) {
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

		// Then delete it
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Delete,
			Path:   "test-function",
			Data: registry.Entry{
				ID:   "test-function",
				Kind: runtime.KindFunction,
			},
		})

		collector.AssertEvent("test-function", registry.Accept)
		_, exists := mockRt.functions["test-function"]
		assert.False(t, exists, "Function should not exist in mock runtime")
	})

	t.Run("create function - duplicate", func(t *testing.T) {
		manager, bus, _ := setupRuntimeTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newRuntimeEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Accept, registry.Reject)

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

		// Then create it again
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
		collector.AssertEvent("test-function", registry.Reject)
	})
}

func TestRuntimeService_ErrorHandling(t *testing.T) {
	t.Run("update function - not found", func(t *testing.T) {
		manager, bus, _ := setupRuntimeTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newRuntimeEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Reject)

		// Update a non-existent function
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

		collector.AssertEvent("test-function", registry.Reject)
	})

	t.Run("delete function - not found", func(t *testing.T) {
		manager, bus, _ := setupRuntimeTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newRuntimeEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Reject)

		// Delete a non-existent function
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Delete,
			Path:   "test-function",
			Data: registry.Entry{
				ID:   "test-function",
				Kind: runtime.KindFunction,
			},
		})

		collector.AssertEvent("test-function", registry.Reject)
	})

	t.Run("create library - duplicate", func(t *testing.T) {
		manager, bus, _ := setupRuntimeTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newRuntimeEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Accept, registry.Reject)

		// First create the library
		libraryConfig := map[string]interface{}{
			"meta": map[string]interface{}{
				"runtime": "testRuntime",
			},
			"source": "library code",
		}

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-library",
			Data: registry.Entry{
				ID:   "test-library",
				Kind: runtime.KindLibrary,
				Data: payload.New(libraryConfig),
			},
		})
		collector.AssertEvent("test-library", registry.Accept)

		// Then create it again
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-library",
			Data: registry.Entry{
				ID:   "test-library",
				Kind: runtime.KindLibrary,
				Data: payload.New(libraryConfig),
			},
		})
		collector.AssertEvent("test-library", registry.Reject)
	})

	t.Run("update library - not found", func(t *testing.T) {
		manager, bus, _ := setupRuntimeTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newRuntimeEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Reject)

		// Update a non-existent library
		updatedConfig := map[string]interface{}{
			"meta": map[string]interface{}{
				"runtime": "testRuntime",
			},
			"source": "updated library code",
		}

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Update,
			Path:   "test-library",
			Data: registry.Entry{
				ID:   "test-library",
				Kind: runtime.KindLibrary,
				Data: payload.New(updatedConfig),
			},
		})

		collector.AssertEvent("test-library", registry.Reject)
	})

	t.Run("delete library - not found", func(t *testing.T) {
		manager, bus, _ := setupRuntimeTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newRuntimeEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Reject)

		// Delete a non-existent library
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Delete,
			Path:   "test-library",
			Data: registry.Entry{
				ID:   "test-library",
				Kind: runtime.KindLibrary,
			},
		})

		collector.AssertEvent("test-library", registry.Reject)
	})
}

func TestRuntimeService_InvalidData(t *testing.T) {
	t.Run("invalid registry event data", func(t *testing.T) {
		manager, bus, _ := setupRuntimeTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		// Send an event with invalid data (not a registry.Entry)
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "invalid-event",
			Data:   "not-a-registry-entry",
		})

		// No easy way to assert here without capturing logs,
		// but the code should log an error.
	})

	t.Run("create event without data", func(t *testing.T) {
		manager, bus, _ := setupRuntimeTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newRuntimeEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Reject)

		// Send a create event without data
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-function",
			Data: registry.Entry{
				ID:   "test-function",
				Kind: runtime.KindFunction,
				Data: nil,
			},
		})

		collector.AssertEvent("test-function", registry.Reject)
	})

	t.Run("update event without data", func(t *testing.T) {
		manager, bus, _ := setupRuntimeTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newRuntimeEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Reject)

		// Send an update event without data
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Update,
			Path:   "test-function",
			Data: registry.Entry{
				ID:   "test-function",
				Kind: runtime.KindFunction,
				Data: nil,
			},
		})

		collector.AssertEvent("test-function", registry.Reject)
	})
	t.Run("invalid config function", func(t *testing.T) {
		manager, bus, _ := setupRuntimeTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newRuntimeEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Reject)

		// Send a create event with invalid config
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-function",
			Data: registry.Entry{
				ID:   "test-function",
				Kind: runtime.KindFunction,
				Data: payload.New(map[string]interface{}{
					"invalid": "config",
				}),
			},
		})

		collector.AssertEvent("test-function", registry.Reject)
	})
	t.Run("invalid config library", func(t *testing.T) {
		manager, bus, _ := setupRuntimeTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newRuntimeEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Reject)

		// Send a create event with invalid config
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-library",
			Data: registry.Entry{
				ID:   "test-library",
				Kind: runtime.KindLibrary,
				Data: payload.New(map[string]interface{}{
					"invalid": "config",
				}),
			},
		})

		collector.AssertEvent("test-library", registry.Reject)
	})
}

func TestRuntimeService_UnmarshalAndValidate(t *testing.T) {
	t.Run("unmarshal error - function", func(t *testing.T) {
		manager, _, _ := setupRuntimeTest(t)

		// Invalid JSON data
		err := manager.unmarshalAndValidate(payload.New("invalid-json"), &runtime.FunctionConfig{})
		assert.Error(t, err)
	})

	t.Run("validation error - function", func(t *testing.T) {
		manager, _, _ := setupRuntimeTest(t)

		// Valid JSON but invalid config (missing required fields)
		err := manager.unmarshalAndValidate(payload.New(map[string]interface{}{}), &runtime.FunctionConfig{})
		assert.Error(t, err)
	})

	t.Run("unmarshal error - library", func(t *testing.T) {
		manager, _, _ := setupRuntimeTest(t)

		// Invalid JSON data
		err := manager.unmarshalAndValidate(payload.New("invalid-json"), &runtime.LibraryConfig{})
		assert.Error(t, err)
	})

	t.Run("validation error - library", func(t *testing.T) {
		manager, _, _ := setupRuntimeTest(t)

		// Valid JSON but invalid config (missing required fields)
		err := manager.unmarshalAndValidate(payload.New(map[string]interface{}{}), &runtime.LibraryConfig{})
		assert.Error(t, err)
	})
	t.Run("valid config - function", func(t *testing.T) {
		manager, _, _ := setupRuntimeTest(t)

		// Valid JSON and config
		validConfig := map[string]interface{}{
			"meta": map[string]interface{}{
				"runtime": "testRuntime",
			},
			"source": "function() {}",
			"method": "testMethod",
		}
		err := manager.unmarshalAndValidate(payload.New(validConfig), &runtime.FunctionConfig{})
		assert.NoError(t, err)
	})

	t.Run("valid config - library", func(t *testing.T) {
		manager, _, _ := setupRuntimeTest(t)

		// Valid JSON and config
		validConfig := map[string]interface{}{
			"meta": map[string]interface{}{
				"runtime": "testRuntime",
			},
			"source": "library code",
		}
		err := manager.unmarshalAndValidate(payload.New(validConfig), &runtime.LibraryConfig{})
		assert.NoError(t, err)
	})
}

// Error Mock Runtime
type errorMockRuntime struct {
	mockRuntime
	addFunctionError    error
	updateFunctionError error
	addLibraryError     error
	updateLibraryError  error
	deleteError         error
}

func (m *errorMockRuntime) AddFunction(id registry.ID, config runtime.FunctionConfig) error {
	if m.addFunctionError != nil {
		return m.addFunctionError
	}
	return m.mockRuntime.AddFunction(id, config)
}

func (m *errorMockRuntime) UpdateFunction(id registry.ID, config runtime.FunctionConfig) error {
	if m.updateFunctionError != nil {
		return m.updateFunctionError
	}
	return m.mockRuntime.UpdateFunction(id, config)
}

func (m *errorMockRuntime) AddLibrary(id registry.ID, config runtime.LibraryConfig) error {
	if m.addLibraryError != nil {
		return m.addLibraryError
	}
	return m.mockRuntime.AddLibrary(id, config)
}

func (m *errorMockRuntime) UpdateLibrary(id registry.ID, config runtime.LibraryConfig) error {
	if m.updateLibraryError != nil {
		return m.updateLibraryError
	}
	return m.mockRuntime.UpdateLibrary(id, config)
}

func (m *errorMockRuntime) Delete(id registry.ID) error {
	if m.deleteError != nil {
		return m.deleteError
	}
	return m.mockRuntime.Delete(id)
}

func TestRuntimeService_MockRuntimeErrors(t *testing.T) {
	t.Run("add function error", func(t *testing.T) {
		mockRt := &errorMockRuntime{
			mockRuntime: mockRuntime{
				functions: make(map[registry.ID]runtime.FunctionConfig),
				libraries: make(map[registry.ID]runtime.LibraryConfig),
			},
			addFunctionError: errors.New("mock AddFunction error"),
		}
		manager, bus, _ := setupRuntimeTest(t)
		manager.run = mockRt // Use the errorMockRuntime
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newRuntimeEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Reject)

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

		collector.AssertEvent("test-function", registry.Reject)
	})

	t.Run("update function error", func(t *testing.T) {
		mockRt := &errorMockRuntime{
			mockRuntime: mockRuntime{
				functions: make(map[registry.ID]runtime.FunctionConfig),
				libraries: make(map[registry.ID]runtime.LibraryConfig),
			},
			updateFunctionError: errors.New("mock UpdateFunction error"),
		}
		manager, bus, _ := setupRuntimeTest(t)
		manager.run = mockRt
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newRuntimeEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Reject, registry.Accept)

		// Create function first
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

		// Send update function event that will fail in mockRuntime
		updatedFunctionConfig := map[string]interface{}{
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
				Data: payload.New(updatedFunctionConfig),
			},
		})
		collector.AssertEvent("test-function", registry.Reject)

	})
	t.Run("add library error", func(t *testing.T) {
		mockRt := &errorMockRuntime{
			mockRuntime: mockRuntime{
				functions: make(map[registry.ID]runtime.FunctionConfig),
				libraries: make(map[registry.ID]runtime.LibraryConfig),
			},
			addLibraryError: errors.New("mock AddLibrary error"),
		}
		manager, bus, _ := setupRuntimeTest(t)
		manager.run = mockRt
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newRuntimeEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Reject)

		// Send create library event
		libraryConfig := map[string]interface{}{
			"meta": map[string]interface{}{
				"runtime": "testRuntime",
			},
			"source": "library code",
		}

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-library",
			Data: registry.Entry{
				ID:   "test-library",
				Kind: runtime.KindLibrary,
				Data: payload.New(libraryConfig),
			},
		})

		collector.AssertEvent("test-library", registry.Reject)
	})

	t.Run("update library error", func(t *testing.T) {
		mockRt := &errorMockRuntime{
			mockRuntime: mockRuntime{
				functions: make(map[registry.ID]runtime.FunctionConfig),
				libraries: make(map[registry.ID]runtime.LibraryConfig),
			},
			updateLibraryError: errors.New("mock UpdateLibrary error"),
		}
		manager, bus, _ := setupRuntimeTest(t)
		manager.run = mockRt
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newRuntimeEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Reject, registry.Accept)

		// Create library first
		libraryConfig := map[string]interface{}{
			"meta": map[string]interface{}{
				"runtime": "testRuntime",
			},
			"source": "library code",
		}

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-library",
			Data: registry.Entry{
				ID:   "test-library",
				Kind: runtime.KindLibrary,
				Data: payload.New(libraryConfig),
			},
		})
		collector.AssertEvent("test-library", registry.Accept)

		// Send update library event that will fail
		updatedLibraryConfig := map[string]interface{}{
			"meta": map[string]interface{}{
				"runtime": "testRuntime",
			},
			"source": "updated library code",
		}

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Update,
			Path:   "test-library",
			Data: registry.Entry{
				ID:   "test-library",
				Kind: runtime.KindLibrary,
				Data: payload.New(updatedLibraryConfig),
			},
		})

		collector.AssertEvent("test-library", registry.Reject)
	})

	t.Run("delete error", func(t *testing.T) {
		mockRt := &errorMockRuntime{
			mockRuntime: mockRuntime{
				functions: make(map[registry.ID]runtime.FunctionConfig),
				libraries: make(map[registry.ID]runtime.LibraryConfig),
			},
			deleteError: errors.New("mock Delete error"),
		}
		manager, bus, _ := setupRuntimeTest(t)
		manager.run = mockRt
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newRuntimeEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Reject, registry.Accept)

		// Create function first (can be function or library)
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

		// Send delete event that will fail
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Delete,
			Path:   "test-function",
			Data: registry.Entry{
				ID:   "test-function",
				Kind: runtime.KindFunction,
			},
		})

		collector.AssertEvent("test-function", registry.Reject)
	})
}
