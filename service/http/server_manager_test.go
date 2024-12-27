package http

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/service/http"
	httpbase "net/http"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/pkg/eventbus"
	payload2 "github.com/ponyruntime/pony/pkg/payload"
	"github.com/ponyruntime/pony/pkg/payload/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Helper function to setup test environment
func setupTest(t *testing.T) (*ServerManager, *eventbus.Bus) {
	logger := zap.NewNop()
	bus := eventbus.NewBus(logger)

	tr := payload2.NewTranscoder()
	json.Register(tr)
	manager := Init(bus, tr, func(writer httpbase.ResponseWriter, request *httpbase.Request) {
		_, _ = writer.Write([]byte("Hello, World!"))
	}, logger)
	return manager, bus
}

// eventCollector helps manage event expectations in tests
type eventCollector struct {
	t        *testing.T
	bus      events.Bus
	ctx      context.Context
	cancel   context.CancelFunc
	listener *eventbus.Subscriber
	eventCh  chan events.Event
}

// newEventCollector creates a new eventCollector instance
func newEventCollector(t *testing.T, bus events.Bus) *eventCollector {
	ctx, cancel := context.WithCancel(context.Background())
	return &eventCollector{
		t:       t,
		bus:     bus,
		ctx:     ctx,
		cancel:  cancel,
		eventCh: make(chan events.Event, 100), // Buffered channel to prevent blocking
	}
}

// Listen starts listening for events matching the given system and kinds
func (e *eventCollector) Listen(system events.System, kinds ...events.Kind) {
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
func (e *eventCollector) AssertEventCount(expectedCount int) {
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
func (e *eventCollector) AssertEvent(expectedPath events.Path, expectedKind events.Kind) {
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
func (e *eventCollector) Close() {
	if e.listener != nil {
		e.listener.Close()
	}
	e.cancel()
	close(e.eventCh)
}
func TestServerManager_Lifecycle(t *testing.T) {
	t.Run("start and stop", func(t *testing.T) {
		manager, _ := setupTest(t)

		// Test Start
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)

		// Test Stop
		err = manager.Stop()
		require.NoError(t, err)
	})

	t.Run("double start", func(t *testing.T) {
		manager, _ := setupTest(t)

		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)

		// Second start should fail
		err = manager.Start(ctx)
		assert.Error(t, err)

		manager.Stop()
	})
}

func TestServerManager_ServerOperations(t *testing.T) {
	t.Run("create server", func(t *testing.T) {
		manager, bus := setupTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Accept)

		// Send create server event
		serverConfig := map[string]interface{}{
			"addr": ":8080",
			"timeouts": map[string]interface{}{
				"read":  "5s",
				"write": "5s",
				"idle":  "60s",
			},
		}

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-server",
			Data: registry.Entry{
				ID:   "test-server",
				Kind: http.KindServer,
				Data: payload.New(serverConfig),
			},
		})

		collector.AssertEvent("test-server", registry.Accept)
	})

	t.Run("update server", func(t *testing.T) {
		manager, bus := setupTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Accept)

		// First create the server
		serverConfig := map[string]interface{}{
			"addr": ":8080",
		}

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-server",
			Data: registry.Entry{
				ID:   "test-server",
				Kind: http.KindServer,
				Data: payload.New(serverConfig),
			},
		})
		collector.AssertEvent("test-server", registry.Accept)

		// Then update it
		updatedConfig := map[string]interface{}{
			"addr": ":8081",
		}

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Update,
			Path:   "test-server",
			Data: registry.Entry{
				ID:   "test-server",
				Kind: http.KindServer,
				Data: payload.New(updatedConfig),
			},
		})

		collector.AssertEvent("test-server", registry.Accept)
	})

	t.Run("delete server", func(t *testing.T) {
		manager, bus := setupTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Accept)

		// First create the server
		serverConfig := map[string]interface{}{
			"addr": ":8080",
		}

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-server",
			Data: registry.Entry{
				ID:   "test-server",
				Kind: http.KindServer,
				Data: payload.New(serverConfig),
			},
		})
		collector.AssertEvent("test-server", registry.Accept)

		// Then delete it
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Delete,
			Path:   "test-server",
			Data: registry.Entry{
				ID:   "test-server",
				Kind: http.KindServer,
			},
		})

		collector.AssertEvent("test-server", registry.Accept)
	})

	t.Run("create server - duplicate", func(t *testing.T) {
		manager, bus := setupTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Accept, registry.Reject)

		// First create the server
		serverConfig := map[string]interface{}{
			"addr": ":8080",
		}

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-server",
			Data: registry.Entry{
				ID:   "test-server",
				Kind: http.KindServer,
				Data: payload.New(serverConfig),
			},
		})
		collector.AssertEvent("test-server", registry.Accept)

		// Then create it again
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-server",
			Data: registry.Entry{
				ID:   "test-server",
				Kind: http.KindServer,
				Data: payload.New(serverConfig),
			},
		})
		collector.AssertEvent("test-server", registry.Reject)
	})

	t.Run("update server - not found", func(t *testing.T) {
		manager, bus := setupTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Reject)

		// Update a non-existent server
		updatedConfig := map[string]interface{}{
			"addr": ":8081",
		}

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Update,
			Path:   "test-server",
			Data: registry.Entry{
				ID:   "test-server",
				Kind: http.KindServer,
				Data: payload.New(updatedConfig),
			},
		})

		collector.AssertEvent("test-server", registry.Reject)
	})

	t.Run("delete server - not found", func(t *testing.T) {
		manager, bus := setupTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Reject)

		// Delete a non-existent server
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Delete,
			Path:   "test-server",
			Data: registry.Entry{
				ID:   "test-server",
				Kind: http.KindServer,
			},
		})

		collector.AssertEvent("test-server", registry.Reject)
	})
}

func TestServerManager_RouterOperations(t *testing.T) {
	t.Run("create router", func(t *testing.T) {
		manager, bus := setupTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Accept)

		// First create a server
		serverConfig := map[string]interface{}{
			"addr": ":8080",
		}

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-server",
			Data: registry.Entry{
				ID:   "test-server",
				Kind: http.KindServer,
				Data: payload.New(serverConfig),
			},
		})
		collector.AssertEvent("test-server", registry.Accept)

		// Then create a router
		routerConfig := map[string]interface{}{
			"prefix": "/api",
			"meta": map[string]interface{}{
				"server": "test-server",
			},
		}

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-router",
			Data: registry.Entry{
				ID:   "test-router",
				Kind: http.KindRouter,
				Data: payload.New(routerConfig),
			},
		})

		collector.AssertEvent("test-router", registry.Accept)
	})

	t.Run("update router", func(t *testing.T) {
		manager, bus := setupTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Accept)

		// Create server and router first
		serverConfig := map[string]interface{}{
			"addr": ":8080",
		}
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-server",
			Data: registry.Entry{
				ID:   "test-server",
				Kind: http.KindServer,
				Data: payload.New(serverConfig),
			},
		})
		collector.AssertEvent("test-server", registry.Accept)

		routerConfig := map[string]interface{}{
			"prefix": "/api",
			"meta": map[string]interface{}{
				"server": "test-server",
			},
		}
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-router",
			Data: registry.Entry{
				ID:   "test-router",
				Kind: http.KindRouter,
				Data: payload.New(routerConfig),
			},
		})
		collector.AssertEvent("test-router", registry.Accept)

		// Update router
		updatedRouterConfig := map[string]interface{}{
			"prefix": "/api/v2",
			"meta": map[string]interface{}{
				"server": "test-server",
			},
		}
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Update,
			Path:   "test-router",
			Data: registry.Entry{
				ID:   "test-router",
				Kind: http.KindRouter,
				Data: payload.New(updatedRouterConfig),
			},
		})

		collector.AssertEvent("test-router", registry.Accept)
	})

	t.Run("delete router", func(t *testing.T) {
		manager, bus := setupTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)

		require.NoError(t, err)
		defer manager.Stop()

		collector := newEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Accept)

		// Setup server and router
		serverConfig := map[string]interface{}{
			"addr": ":8080",
		}
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-server",
			Data: registry.Entry{
				ID:   "test-server",
				Kind: http.KindServer,
				Data: payload.New(serverConfig),
			},
		})

		collector.AssertEvent("test-server", registry.Accept)

		routerConfig := map[string]interface{}{
			"prefix": "/api",
			"meta": map[string]interface{}{
				"server": "test-server",
			},
		}
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-router",
			Data: registry.Entry{
				ID:   "test-router",
				Kind: http.KindRouter,
				Data: payload.New(routerConfig),
			},
		})
		collector.AssertEvent("test-router", registry.Accept)

		// Delete router
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Delete,
			Path:   "test-router",
			Data: registry.Entry{
				ID:   "test-router",
				Kind: http.KindRouter,
			},
		})

		collector.AssertEvent("test-router", registry.Accept)
	})

	t.Run("create router - server not found", func(t *testing.T) {
		manager, bus := setupTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Reject)

		// Try to create a router for a non-existent server
		routerConfig := map[string]interface{}{
			"prefix": "/api",
			"meta": map[string]interface{}{
				"server": "test-server",
			},
		}

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-router",
			Data: registry.Entry{
				ID:   "test-router",
				Kind: http.KindRouter,
				Data: payload.New(routerConfig),
			},
		})

		collector.AssertEvent("test-router", registry.Reject)
	})

	t.Run("delete router - not found", func(t *testing.T) {
		manager, bus := setupTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Reject)

		// Try to delete a non-existent router
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Delete,
			Path:   "test-router",
			Data: registry.Entry{
				ID:   "test-router",
				Kind: http.KindRouter,
			},
		})

		collector.AssertEvent("test-router", registry.Reject)
	})
}

func TestServerManager_EndpointOperations(t *testing.T) {
	t.Run("create endpoint", func(t *testing.T) {
		manager, bus := setupTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Accept)

		// Setup server first
		serverConfig := map[string]interface{}{
			"addr": ":8080",
		}

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-server",
			Data: registry.Entry{
				ID:   "test-server",
				Kind: http.KindServer,
				Data: payload.New(serverConfig),
			},
		})
		collector.AssertEvent("test-server", registry.Accept)

		// Create endpoint
		endpointConfig := map[string]interface{}{
			"path":   "/test",
			"method": "GET",
			"meta": map[string]interface{}{
				"server": "test-server",
			},
		}

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-endpoint",
			Data: registry.Entry{
				ID:   "test-endpoint",
				Kind: http.KindEndpoint,
				Data: payload.New(endpointConfig),
			},
		})

		collector.AssertEvent("test-endpoint", registry.Accept)
	})

	t.Run("update endpoint", func(t *testing.T) {
		manager, bus := setupTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)

		require.NoError(t, err)
		defer manager.Stop()

		collector := newEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Accept)

		// Setup server and initial endpoint
		serverConfig := map[string]interface{}{
			"addr": ":8080",
		}

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-server",
			Data: registry.Entry{
				ID:   "test-server",
				Kind: http.KindServer,
				Data: payload.New(serverConfig),
			},
		})
		collector.AssertEvent("test-server", registry.Accept)

		endpointConfig := map[string]interface{}{
			"path":   "/test",
			"method": "GET",
			"meta": map[string]interface{}{
				"server": "test-server",
			},
		}

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-endpoint",
			Data: registry.Entry{
				ID:   "test-endpoint",
				Kind: http.KindEndpoint,
				Data: payload.New(endpointConfig),
			},
		})
		collector.AssertEvent("test-endpoint", registry.Accept)

		// Update endpoint
		updatedConfig := map[string]interface{}{
			"path":   "/test/updated",
			"method": "POST",
			"meta": map[string]interface{}{
				"server": "test-server",
			},
		}

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Update,
			Path:   "test-endpoint",
			Data: registry.Entry{
				ID:   "test-endpoint",
				Kind: http.KindEndpoint,
				Data: payload.New(updatedConfig),
			},
		})

		collector.AssertEvent("test-endpoint", registry.Accept)
	})

	t.Run("delete endpoint", func(t *testing.T) {
		manager, bus := setupTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Accept)

		// Setup server and endpoint
		serverConfig := map[string]interface{}{
			"addr": ":8080",
		}

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-server",
			Data: registry.Entry{
				ID:   "test-server",
				Kind: http.KindServer,
				Data: payload.New(serverConfig),
			},
		})
		collector.AssertEvent("test-server", registry.Accept)

		endpointConfig := map[string]interface{}{
			"path":   "/test",
			"method": "GET",
			"meta": map[string]interface{}{
				"server": "test-server",
			},
		}

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-endpoint",
			Data: registry.Entry{
				ID:   "test-endpoint",
				Kind: http.KindEndpoint,
				Data: payload.New(endpointConfig),
			},
		})
		collector.AssertEvent("test-endpoint", registry.Accept)

		// Delete endpoint
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Delete,
			Path:   "test-endpoint",
			Data: registry.Entry{
				ID:   "test-endpoint",
				Kind: http.KindEndpoint,
			},
		})

		collector.AssertEvent("test-endpoint", registry.Accept)
	})

	t.Run("create endpoint - server not found", func(t *testing.T) {
		manager, bus := setupTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Reject)

		// Try to create an endpoint for a non-existent server
		endpointConfig := map[string]interface{}{
			"path":   "/test",
			"method": "GET",
			"meta": map[string]interface{}{
				"server": "nonexistent-server",
			},
		}

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-endpoint",
			Data: registry.Entry{
				ID:   "test-endpoint",
				Kind: http.KindEndpoint,
				Data: payload.New(endpointConfig),
			},
		})

		collector.AssertEvent("test-endpoint", registry.Reject)
	})

	t.Run("delete endpoint - not found", func(t *testing.T) {
		manager, bus := setupTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Reject)

		// Try to delete a non-existent endpoint
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Delete,
			Path:   "nonexistent-endpoint",
			Data: registry.Entry{
				ID:   "nonexistent-endpoint",
				Kind: http.KindEndpoint,
			},
		})

		collector.AssertEvent("nonexistent-endpoint", registry.Reject)
	})
}

func TestServerManager_handleEvent(t *testing.T) {
	t.Run("invalid registry event data", func(t *testing.T) {
		manager, bus := setupTest(t)
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

		// You can add assertions here to verify the behavior,
		// e.g., check logs or side effects
	})

	t.Run("create event without data", func(t *testing.T) {
		manager, bus := setupTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Reject)

		// Send a create event without data
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-server",
			Data: registry.Entry{
				ID:   "test-server",
				Kind: http.KindServer,
				Data: nil,
			},
		})

		collector.AssertEvent("test-server", registry.Reject)
	})

	t.Run("update event without data", func(t *testing.T) {
		manager, bus := setupTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Reject)

		// Send an update event without data
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Update,
			Path:   "test-server",
			Data: registry.Entry{
				ID:   "test-server",
				Kind: http.KindServer,
				Data: nil,
			},
		})

		collector.AssertEvent("test-server", registry.Reject)
	})

	t.Run("invalid config", func(t *testing.T) {
		manager, bus := setupTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Reject)

		// Send a create event with invalid config
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-server",
			Data: registry.Entry{
				ID:   "test-server",
				Kind: http.KindServer,
				Data: payload.New(map[string]interface{}{
					"invalid": "config",
				}),
			},
		})

		collector.AssertEvent("test-server", registry.Reject)
	})

	t.Run("invalid config router", func(t *testing.T) {
		manager, bus := setupTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Reject)

		// Send a create event with invalid config
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-router",
			Data: registry.Entry{
				ID:   "test-router",
				Kind: http.KindRouter,
				Data: payload.New(map[string]interface{}{
					"invalid": "config",
				}),
			},
		})

		collector.AssertEvent("test-router", registry.Reject)
	})

	t.Run("invalid config endpoint", func(t *testing.T) {
		manager, bus := setupTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Reject)

		// Send a create event with invalid config
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-endpoint",
			Data: registry.Entry{
				ID:   "test-endpoint",
				Kind: http.KindEndpoint,
				Data: payload.New(map[string]interface{}{
					"invalid": "config",
				}),
			},
		})

		collector.AssertEvent("test-endpoint", registry.Reject)
	})
}

func TestServerManager_unmarshalAndValidate(t *testing.T) {
	t.Run("unmarshal error", func(t *testing.T) {
		manager, _ := setupTest(t)

		// Invalid JSON data
		err := manager.unmarshalAndValidate(payload.New("invalid-json"), &http.ServerConfig{})
		assert.Error(t, err)
	})

	t.Run("validation error", func(t *testing.T) {
		manager, _ := setupTest(t)

		// Valid JSON but invalid config (missing required field)
		err := manager.unmarshalAndValidate(payload.New(map[string]interface{}{}), &http.ServerConfig{})
		assert.Error(t, err)
	})
}

func TestServerManager_Migration(t *testing.T) {
	t.Run("migrate router to new server", func(t *testing.T) {
		manager, bus := setupTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Accept)

		// Create two servers
		server1Config := map[string]interface{}{"addr": ":8080"}
		server2Config := map[string]interface{}{"addr": ":8081"}
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-server-1",
			Data:   registry.Entry{ID: "test-server-1", Kind: http.KindServer, Data: payload.New(server1Config)},
		})
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-server-2",
			Data:   registry.Entry{ID: "test-server-2", Kind: http.KindServer, Data: payload.New(server2Config)},
		})
		collector.AssertEvent("test-server-1", registry.Accept)
		collector.AssertEvent("test-server-2", registry.Accept)

		// Create router on server 1
		routerConfig := map[string]interface{}{
			"prefix": "/api",
			"meta":   map[string]interface{}{"server": "test-server-1"},
		}
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-router",
			Data:   registry.Entry{ID: "test-router", Kind: http.KindRouter, Data: payload.New(routerConfig)},
		})
		collector.AssertEvent("test-router", registry.Accept)

		// Assert router is on server 1
		assert.Equal(t, registry.ID("test-server-1"), manager.routerServers[registry.ID("test-router")])

		// Update router to move to server 2
		updatedRouterConfig := map[string]interface{}{
			"prefix": "/api/v2",
			"meta":   map[string]interface{}{"server": "test-server-2"},
		}
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Update,
			Path:   "test-router",
			Data:   registry.Entry{ID: "test-router", Kind: http.KindRouter, Data: payload.New(updatedRouterConfig)},
		})
		collector.AssertEvent("test-router", registry.Accept)

		// Assert router is now on server 2
		assert.Equal(t, registry.ID("test-server-2"), manager.routerServers[registry.ID("test-router")])
	})

	t.Run("migrate endpoint to new server", func(t *testing.T) {
		manager, bus := setupTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Accept)

		// Create two servers
		server1Config := map[string]interface{}{"addr": ":8080"}
		server2Config := map[string]interface{}{"addr": ":8081"}
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-server-1",
			Data:   registry.Entry{ID: "test-server-1", Kind: http.KindServer, Data: payload.New(server1Config)},
		})
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-server-2",
			Data:   registry.Entry{ID: "test-server-2", Kind: http.KindServer, Data: payload.New(server2Config)},
		})
		collector.AssertEvent("test-server-1", registry.Accept)
		collector.AssertEvent("test-server-2", registry.Accept)

		// Create endpoint on server 1
		endpointConfig := map[string]interface{}{
			"path":   "/test",
			"method": "GET",
			"meta":   map[string]interface{}{"server": "test-server-1"},
		}
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-endpoint",
			Data:   registry.Entry{ID: "test-endpoint", Kind: http.KindEndpoint, Data: payload.New(endpointConfig)},
		})
		collector.AssertEvent("test-endpoint", registry.Accept)

		// Assert endpoint is on server 1
		assert.Equal(t, registry.ID("test-server-1"), manager.endpointServers[registry.ID("test-endpoint")])

		// Update endpoint to move to server 2
		updatedEndpointConfig := map[string]interface{}{
			"path":   "/test/v2",
			"method": "POST",
			"meta":   map[string]interface{}{"server": "test-server-2"},
		}
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Update,
			Path:   "test-endpoint",
			Data:   registry.Entry{ID: "test-endpoint", Kind: http.KindEndpoint, Data: payload.New(updatedEndpointConfig)},
		})
		collector.AssertEvent("test-endpoint", registry.Accept)

		// Assert endpoint is now on server 2
		assert.Equal(t, registry.ID("test-server-2"), manager.endpointServers[registry.ID("test-endpoint")])
	})
}

func TestServerManager_Start_EdgeCases(t *testing.T) {
	t.Run("Start with cancelled context", func(t *testing.T) {
		manager, _ := setupTest(t)
		defer manager.Stop()

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel the context immediately

		err := manager.Start(ctx)
		assert.Error(t, err) // Expecting an error when context is already cancelled
	})
}

func TestServerManager_handleServer_EdgeCases(t *testing.T) {
	t.Run("handleServer with nil config", func(t *testing.T) {
		manager, bus := setupTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Accept, registry.Reject)

		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-server",
			Data: registry.Entry{
				ID:   "test-server",
				Kind: http.KindServer,
				Data: nil, // Send nil config
			},
		})

		collector.AssertEvent("test-server", registry.Reject)
	})
	t.Run("handleServer delete server linked to router and endpoint", func(t *testing.T) {
		manager, bus := setupTest(t)
		ctx := context.Background()
		err := manager.Start(ctx)
		require.NoError(t, err)
		defer manager.Stop()

		collector := newEventCollector(t, bus)
		defer collector.Close()
		collector.Listen(registry.System, registry.Accept)

		// Create a server
		serverConfig := map[string]interface{}{"addr": ":8080"}
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-server",
			Data:   registry.Entry{ID: "test-server", Kind: http.KindServer, Data: payload.New(serverConfig)},
		})
		collector.AssertEvent("test-server", registry.Accept)

		// Create a router linked to the server
		routerConfig := map[string]interface{}{
			"prefix": "/api",
			"meta":   map[string]interface{}{"server": "test-server"},
		}
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-router",
			Data:   registry.Entry{ID: "test-router", Kind: http.KindRouter, Data: payload.New(routerConfig)},
		})
		collector.AssertEvent("test-router", registry.Accept)

		// Create an endpoint linked to the server
		endpointConfig := map[string]interface{}{
			"path":   "/test",
			"method": "GET",
			"meta":   map[string]interface{}{"server": "test-server"},
		}
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   "test-endpoint",
			Data:   registry.Entry{ID: "test-endpoint", Kind: http.KindEndpoint, Data: payload.New(endpointConfig)},
		})
		collector.AssertEvent("test-endpoint", registry.Accept)

		// Assert router and endpoint are linked to the server
		assert.Equal(t, registry.ID("test-server"), manager.routerServers[registry.ID("test-router")])
		assert.Equal(t, registry.ID("test-server"), manager.endpointServers[registry.ID("test-endpoint")])

		// Delete the server
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Delete,
			Path:   "test-server",
			Data:   registry.Entry{ID: "test-server", Kind: http.KindServer},
		})
		collector.AssertEvent("test-server", registry.Accept)

		// Assert router and endpoint are no longer linked to any server
		_, routerExists := manager.routerServers[registry.ID("test-router")]
		assert.False(t, routerExists)
		_, endpointExists := manager.endpointServers[registry.ID("test-endpoint")]
		assert.False(t, endpointExists)
	})
}

func TestServerManager_RouterMigration_MultipleTimes(t *testing.T) {
	manager, bus := setupTest(t)
	ctx := context.Background()
	err := manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()

	collector := newEventCollector(t, bus)
	defer collector.Close()
	collector.Listen(registry.System, registry.Accept)

	// Create three servers
	servers := []string{"test-server-1", "test-server-2", "test-server-3"}
	for i, serverID := range servers {
		serverConfig := map[string]interface{}{"addr": fmt.Sprintf(":808%d", i)}
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   events.Path(serverID),
			Data:   registry.Entry{ID: registry.ID(serverID), Kind: http.KindServer, Data: payload.New(serverConfig)},
		})
		collector.AssertEvent(events.Path(serverID), registry.Accept)
	}

	// Create router on server 1
	routerConfig := map[string]interface{}{
		"prefix": "/api",
		"meta":   map[string]interface{}{"server": "test-server-1"},
	}
	bus.Send(ctx, events.Event{
		System: registry.System,
		Kind:   registry.Create,
		Path:   "test-router",
		Data:   registry.Entry{ID: "test-router", Kind: http.KindRouter, Data: payload.New(routerConfig)},
	})
	collector.AssertEvent("test-router", registry.Accept)

	// Assert router is on server 1
	assert.Equal(t, registry.ID("test-server-1"), manager.routerServers[registry.ID("test-router")])

	// Migrate to server 2
	updatedRouterConfig := map[string]interface{}{
		"prefix": "/api/v2",
		"meta":   map[string]interface{}{"server": "test-server-2"},
	}
	bus.Send(ctx, events.Event{
		System: registry.System,
		Kind:   registry.Update,
		Path:   "test-router",
		Data:   registry.Entry{ID: "test-router", Kind: http.KindRouter, Data: payload.New(updatedRouterConfig)},
	})
	collector.AssertEvent("test-router", registry.Accept)

	// Assert router is now on server 2
	assert.Equal(t, registry.ID("test-server-2"), manager.routerServers[registry.ID("test-router")])

	// Migrate to server 3
	updatedRouterConfig = map[string]interface{}{
		"prefix": "/api/v3",
		"meta":   map[string]interface{}{"server": "test-server-3"},
	}
	bus.Send(ctx, events.Event{
		System: registry.System,
		Kind:   registry.Update,
		Path:   "test-router",
		Data:   registry.Entry{ID: "test-router", Kind: http.KindRouter, Data: payload.New(updatedRouterConfig)},
	})
	collector.AssertEvent("test-router", registry.Accept)

	// Assert router is now on server 3
	assert.Equal(t, registry.ID("test-server-3"), manager.routerServers[registry.ID("test-router")])

	// Migrate back to server 1
	updatedRouterConfig = map[string]interface{}{
		"prefix": "/api/v1",
		"meta":   map[string]interface{}{"server": "test-server-1"},
	}
	bus.Send(ctx, events.Event{
		System: registry.System,
		Kind:   registry.Update,
		Path:   "test-router",
		Data:   registry.Entry{ID: "test-router", Kind: http.KindRouter, Data: payload.New(updatedRouterConfig)},
	})
	collector.AssertEvent("test-router", registry.Accept)

	// Assert router is now on server 1 again
	assert.Equal(t, registry.ID("test-server-1"), manager.routerServers[registry.ID("test-router")])
}

func TestServerManager_EndpointMigration_MultipleTimes(t *testing.T) {
	manager, bus := setupTest(t)
	ctx := context.Background()
	err := manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()

	collector := newEventCollector(t, bus)
	defer collector.Close()
	collector.Listen(registry.System, registry.Accept)

	// Create three servers
	servers := []string{"test-server-1", "test-server-2", "test-server-3"}
	for i, serverID := range servers {
		serverConfig := map[string]interface{}{"addr": fmt.Sprintf(":808%d", i)}
		bus.Send(ctx, events.Event{
			System: registry.System,
			Kind:   registry.Create,
			Path:   events.Path(serverID),
			Data:   registry.Entry{ID: registry.ID(serverID), Kind: http.KindServer, Data: payload.New(serverConfig)},
		})
		collector.AssertEvent(events.Path(serverID), registry.Accept)
	}

	// Create endpoint on server 1
	endpointConfig := map[string]interface{}{
		"path":   "/test",
		"method": "GET",
		"meta":   map[string]interface{}{"server": "test-server-1"},
	}
	bus.Send(ctx, events.Event{
		System: registry.System,
		Kind:   registry.Create,
		Path:   "test-endpoint",
		Data:   registry.Entry{ID: "test-endpoint", Kind: http.KindEndpoint, Data: payload.New(endpointConfig)},
	})
	collector.AssertEvent("test-endpoint", registry.Accept)

	// Assert endpoint is on server 1
	assert.Equal(t, registry.ID("test-server-1"), manager.endpointServers[registry.ID("test-endpoint")])

	// Migrate to server 2
	updatedEndpointConfig := map[string]interface{}{
		"path":   "/test/v2",
		"method": "POST",
		"meta":   map[string]interface{}{"server": "test-server-2"},
	}
	bus.Send(ctx, events.Event{
		System: registry.System,
		Kind:   registry.Update,
		Path:   "test-endpoint",
		Data:   registry.Entry{ID: "test-endpoint", Kind: http.KindEndpoint, Data: payload.New(updatedEndpointConfig)},
	})
	collector.AssertEvent("test-endpoint", registry.Accept)

	// Assert endpoint is now on server 2
	assert.Equal(t, registry.ID("test-server-2"), manager.endpointServers[registry.ID("test-endpoint")])

	// Migrate to server 3
	updatedEndpointConfig = map[string]interface{}{
		"path":   "/test/v3",
		"method": "PUT",
		"meta":   map[string]interface{}{"server": "test-server-3"},
	}
	bus.Send(ctx, events.Event{
		System: registry.System,
		Kind:   registry.Update,
		Path:   "test-endpoint",
		Data:   registry.Entry{ID: "test-endpoint", Kind: http.KindEndpoint, Data: payload.New(updatedEndpointConfig)},
	})
	collector.AssertEvent("test-endpoint", registry.Accept)

	// Assert endpoint is now on server 3
	assert.Equal(t, registry.ID("test-server-3"), manager.endpointServers[registry.ID("test-endpoint")])

	// Migrate back to server 1
	updatedEndpointConfig = map[string]interface{}{
		"path":   "/test/v1",
		"method": "GET",
		"meta":   map[string]interface{}{"server": "test-server-1"},
	}
	bus.Send(ctx, events.Event{
		System: registry.System,
		Kind:   registry.Update,
		Path:   "test-endpoint",
		Data:   registry.Entry{ID: "test-endpoint", Kind: http.KindEndpoint, Data: payload.New(updatedEndpointConfig)},
	})
	collector.AssertEvent("test-endpoint", registry.Accept)

	// Assert endpoint is now on server 1 again
	assert.Equal(t, registry.ID("test-server-1"), manager.endpointServers[registry.ID("test-endpoint")])
}

func TestServerManager_RouterUpdate_SameServer(t *testing.T) {
	manager, bus := setupTest(t)
	ctx := context.Background()
	err := manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()

	collector := newEventCollector(t, bus)
	defer collector.Close()
	collector.Listen(registry.System, registry.Accept)

	// Create a server
	serverConfig := map[string]interface{}{"addr": ":8080"}
	bus.Send(ctx, events.Event{
		System: registry.System,
		Kind:   registry.Create,
		Path:   "test-server",
		Data:   registry.Entry{ID: "test-server", Kind: http.KindServer, Data: payload.New(serverConfig)},
	})
	collector.AssertEvent("test-server", registry.Accept)

	// Create a router on the server
	routerConfig := map[string]interface{}{
		"prefix": "/api",
		"meta":   map[string]interface{}{"server": "test-server"},
	}
	bus.Send(ctx, events.Event{
		System: registry.System,
		Kind:   registry.Create,
		Path:   "test-router",
		Data:   registry.Entry{ID: "test-router", Kind: http.KindRouter, Data: payload.New(routerConfig)},
	})
	collector.AssertEvent("test-router", registry.Accept)

	// Assert router is on the server
	assert.Equal(t, registry.ID("test-server"), manager.routerServers[registry.ID("test-router")])

	// Update the router (same server)
	updatedRouterConfig := map[string]interface{}{
		"prefix": "/api/v2", // Updated prefix
		"meta":   map[string]interface{}{"server": "test-server"},
	}
	bus.Send(ctx, events.Event{
		System: registry.System,
		Kind:   registry.Update,
		Path:   "test-router",
		Data:   registry.Entry{ID: "test-router", Kind: http.KindRouter, Data: payload.New(updatedRouterConfig)},
	})
	collector.AssertEvent("test-router", registry.Accept)

	// Assert router is still on the same server (mapping should not change)
	assert.Equal(t, registry.ID("test-server"), manager.routerServers[registry.ID("test-router")])
}

func TestServerManager_EndpointUpdate_SameServer(t *testing.T) {
	manager, bus := setupTest(t)
	ctx := context.Background()
	err := manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()

	collector := newEventCollector(t, bus)
	defer collector.Close()
	collector.Listen(registry.System, registry.Accept)

	// Create a server
	serverConfig := map[string]interface{}{"addr": ":8080"}
	bus.Send(ctx, events.Event{
		System: registry.System,
		Kind:   registry.Create,
		Path:   "test-server",
		Data:   registry.Entry{ID: "test-server", Kind: http.KindServer, Data: payload.New(serverConfig)},
	})
	collector.AssertEvent("test-server", registry.Accept)

	// Create an endpoint on the server
	endpointConfig := map[string]interface{}{
		"path":   "/test",
		"method": "GET",
		"meta":   map[string]interface{}{"server": "test-server"},
	}
	bus.Send(ctx, events.Event{
		System: registry.System,
		Kind:   registry.Create,
		Path:   "test-endpoint",
		Data:   registry.Entry{ID: "test-endpoint", Kind: http.KindEndpoint, Data: payload.New(endpointConfig)},
	})
	collector.AssertEvent("test-endpoint", registry.Accept)

	// Assert endpoint is on the server
	assert.Equal(t, registry.ID("test-server"), manager.endpointServers[registry.ID("test-endpoint")])

	// Update the endpoint (same server)
	updatedEndpointConfig := map[string]interface{}{
		"path":   "/test/v2", // Updated path
		"method": "POST",     // Updated method
		"meta":   map[string]interface{}{"server": "test-server"},
	}
	bus.Send(ctx, events.Event{
		System: registry.System,
		Kind:   registry.Update,
		Path:   "test-endpoint",
		Data:   registry.Entry{ID: "test-endpoint", Kind: http.KindEndpoint, Data: payload.New(updatedEndpointConfig)},
	})
	collector.AssertEvent("test-endpoint", registry.Accept)

	// Assert endpoint is still on the same server (mapping should not change)
	assert.Equal(t, registry.ID("test-server"), manager.endpointServers[registry.ID("test-endpoint")])
}
