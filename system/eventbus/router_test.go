package eventbus

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/events"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestEventRouter(t *testing.T) {
	t.Run("basic event routing", func(t *testing.T) {
		bus := NewBus()
		defer bus.Stop()

		var receivedEvents []events.Event
		var mu sync.Mutex
		eventReceived := make(chan struct{})

		handler := NewBaseHandler(
			Pattern{
				System: "test-system",
				Kind:   "test-kind",
			},
			func(ctx context.Context, evt events.Event) error {
				mu.Lock()
				receivedEvents = append(receivedEvents, evt)
				mu.Unlock()
				close(eventReceived)
				return nil
			},
		)

		router, err := StartRouter(context.Background(), bus, WithHandlers(handler))
		require.NoError(t, err)
		defer router.Stop()

		event := events.Event{
			System: "test-system",
			Kind:   "test-kind",
			Data:   []byte("test-data"),
		}
		bus.Send(context.Background(), event)

		select {
		case <-eventReceived:
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for event")
		}

		mu.Lock()
		require.Len(t, receivedEvents, 1)
		require.Equal(t, event, receivedEvents[0])
		mu.Unlock()
	})

	t.Run("multiple handlers with wildcards", func(t *testing.T) {
		bus := NewBus()
		defer bus.Stop()

		type handlerEvents struct {
			events    []events.Event
			mu        sync.Mutex
			expecting int
			done      chan struct{}
		}

		handlers := map[string]*handlerEvents{
			"exact":      {expecting: 1, done: make(chan struct{})},
			"singlestar": {expecting: 2, done: make(chan struct{})},
			"doublestar": {expecting: 3, done: make(chan struct{})},
		}

		checkAndSignal := func(name string, evt events.Event) {
			h := handlers[name]
			h.mu.Lock()
			h.events = append(h.events, evt)
			if len(h.events) == h.expecting {
				close(h.done)
			}
			h.mu.Unlock()
		}

		allHandlers := []EventHandler{
			// Exact match handler
			NewBaseHandler(
				Pattern{System: "system.service1", Kind: "created"},
				func(ctx context.Context, evt events.Event) error {
					checkAndSignal("exact", evt)
					return nil
				},
			),
			// Single star handler - matches one segment
			NewBaseHandler(
				Pattern{System: "system.*", Kind: "created"},
				func(ctx context.Context, evt events.Event) error {
					checkAndSignal("singlestar", evt)
					return nil
				},
			),
			// Double star handler - matches multiple segments
			NewBaseHandler(
				Pattern{System: "system.**", Kind: "created"},
				func(ctx context.Context, evt events.Event) error {
					checkAndSignal("doublestar", evt)
					return nil
				},
			),
		}

		router, err := StartRouter(context.Background(), bus, WithHandlers(allHandlers...))
		require.NoError(t, err)
		defer router.Stop()

		// Send events that match different patterns
		eventsData := []events.Event{
			{System: "system.service1", Kind: "created", Data: []byte("1")},
			{System: "system.service2", Kind: "created", Data: []byte("2")},
			{System: "system.service1.internal", Kind: "created", Data: []byte("3")},
		}

		for _, evt := range eventsData {
			bus.Send(context.Background(), evt)
			time.Sleep(time.Millisecond) // Small delay to ensure order
		}

		// Wait for all handlers with timeout
		for name, h := range handlers {
			select {
			case <-h.done:
				// ID received all expected events
			case <-time.After(time.Second):
				t.Fatalf("timeout waiting for %s handler", name)
			}
		}

		// Verify results
		t.Run("exact match handler", func(t *testing.T) {
			h := handlers["exact"]
			h.mu.Lock()
			defer h.mu.Unlock()
			require.Len(t, h.events, 1)
			require.Equal(t, eventsData[0], h.events[0])
		})

		t.Run("single star handler", func(t *testing.T) {
			h := handlers["singlestar"]
			h.mu.Lock()
			defer h.mu.Unlock()
			require.Len(t, h.events, 2)
			require.Contains(t, h.events, eventsData[0])
			require.Contains(t, h.events, eventsData[1])
		})

		t.Run("double star handler", func(t *testing.T) {
			h := handlers["doublestar"]
			h.mu.Lock()
			defer h.mu.Unlock()
			require.Len(t, h.events, 3)
			require.Contains(t, h.events, eventsData[0])
			require.Contains(t, h.events, eventsData[1])
			require.Contains(t, h.events, eventsData[2])
		})
	})

	t.Run("error handling and logging", func(t *testing.T) {
		bus := NewBus()
		defer bus.Stop()

		errorReceived := make(chan struct{})
		testLogger := zap.NewNop()

		handler := NewBaseHandler(
			Pattern{System: "test", Kind: "error"},
			func(ctx context.Context, evt events.Event) error {
				defer close(errorReceived)
				return fmt.Errorf("test error: %s", evt.Data)
			},
		)

		router, err := StartRouter(context.Background(), bus,
			WithLogger(testLogger),
			WithHandlers(handler),
		)
		require.NoError(t, err)
		defer router.Stop()

		event := events.Event{
			System: "test",
			Kind:   "error",
			Data:   []byte("error-data"),
		}
		bus.Send(context.Background(), event)

		select {
		case <-errorReceived:
			// Test passed
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for error handler")
		}
	})

	t.Run("concurrent operations stress test", func(t *testing.T) {
		bus := NewBus()
		defer bus.Stop()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		const (
			numHandlers      = 50
			eventsPerHandler = 100
		)

		var wg sync.WaitGroup
		var handlers []EventHandler
		wg.Add(numHandlers * eventsPerHandler)

		// Spawn handlers
		for i := 0; i < numHandlers; i++ {
			handlers = append(handlers, NewBaseHandler(
				Pattern{
					System: events.System(fmt.Sprintf("system-%d", i)),
					Kind:   "*",
				},
				func(ctx context.Context, evt events.Event) error {
					wg.Done()
					return nil
				},
			))
		}

		// Spawn router with all handlers
		router, err := StartRouter(ctx, bus, WithHandlers(handlers...))
		require.NoError(t, err)
		defer router.Stop()

		// Send events
		for i := 0; i < numHandlers; i++ {
			go func(idx int) {
				for j := 0; j < eventsPerHandler; j++ {
					select {
					case <-ctx.Done():
						return
					default:
						event := events.Event{
							System: events.System(fmt.Sprintf("system-%d", idx)),
							Kind:   events.Kind(fmt.Sprintf("event-%d", j)),
							Data:   []byte("test"),
						}
						bus.Send(ctx, event)
						time.Sleep(time.Microsecond)
					}
				}
			}(i)
		}

		// Wait for all events to be processed
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			// Test passed
		case <-ctx.Done():
			t.Fatal("test timed out")
		}
	})
}
