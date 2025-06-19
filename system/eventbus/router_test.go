package eventbus

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"sync/atomic"

	"github.com/ponyruntime/pony/api/event"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestEventRouter(t *testing.T) {
	t.Run("basic event routing", func(t *testing.T) {
		bus := NewBus()
		defer bus.Stop()

		var receivedEvents []event.Event
		var mu sync.Mutex
		eventReceived := make(chan struct{})

		handler := NewBaseHandler(
			Pattern{
				System: "test-system",
				Kind:   "test-kind",
			},
			func(_ context.Context, evt event.Event) error {
				mu.Lock()
				receivedEvents = append(receivedEvents, evt)
				mu.Unlock()
				close(eventReceived)
				return nil
			},
		)

		router, err := StartRouter(context.Background(), bus, WithHandlers(handler))
		require.NoError(t, err)
		defer func() { assert.NoError(t, router.Stop()) }()

		e := event.Event{
			System: "test-system",
			Kind:   "test-kind",
			Data:   []byte("test-data"),
		}
		bus.Send(context.Background(), e)

		select {
		case <-eventReceived:
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for event")
		}

		mu.Lock()
		require.Len(t, receivedEvents, 1)
		require.Equal(t, e, receivedEvents[0])
		mu.Unlock()
	})

	t.Run("multiple handlers with wildcards", func(t *testing.T) {
		bus := NewBus()
		defer bus.Stop()

		type handlerEvents struct {
			events    []event.Event
			mu        sync.Mutex
			expecting int
			done      chan struct{}
		}

		handlers := map[string]*handlerEvents{
			"exact":      {expecting: 1, done: make(chan struct{})},
			"singlestar": {expecting: 2, done: make(chan struct{})},
			"doublestar": {expecting: 3, done: make(chan struct{})},
		}

		checkAndSignal := func(name string, evt event.Event) {
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
				func(_ context.Context, evt event.Event) error {
					checkAndSignal("exact", evt)
					return nil
				},
			),
			// Single star handler - matches one segment
			NewBaseHandler(
				Pattern{System: "system.*", Kind: "created"},
				func(_ context.Context, evt event.Event) error {
					checkAndSignal("singlestar", evt)
					return nil
				},
			),
			// Double star handler - matches multiple segments
			NewBaseHandler(
				Pattern{System: "system.**", Kind: "created"},
				func(_ context.Context, evt event.Event) error {
					checkAndSignal("doublestar", evt)
					return nil
				},
			),
		}

		router, err := StartRouter(context.Background(), bus, WithHandlers(allHandlers...))
		require.NoError(t, err)
		defer func() { assert.NoError(t, router.Stop()) }()

		// send events that match different patterns
		eventsData := []event.Event{
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
				// Source received all expected events
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
			func(_ context.Context, evt event.Event) error {
				defer close(errorReceived)
				return fmt.Errorf("test error: %s", evt.Data)
			},
		)

		router, err := StartRouter(context.Background(), bus,
			WithLogger(testLogger),
			WithHandlers(handler),
		)
		require.NoError(t, err)
		defer func() { assert.NoError(t, router.Stop()) }()

		e := event.Event{
			System: "test",
			Kind:   "error",
			Data:   []byte("error-data"),
		}
		bus.Send(context.Background(), e)

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
					System: fmt.Sprintf("system-%d", i),
					Kind:   "*",
				},
				func(_ context.Context, _ event.Event) error {
					wg.Done()
					return nil
				},
			))
		}

		// Spawn router with all handlers
		router, err := StartRouter(ctx, bus, WithHandlers(handlers...))
		require.NoError(t, err)
		defer func() { assert.NoError(t, router.Stop()) }()

		// send events
		for i := 0; i < numHandlers; i++ {
			go func(idx int) {
				for j := 0; j < eventsPerHandler; j++ {
					select {
					case <-ctx.Done():
						return
					default:
						e := event.Event{
							System: fmt.Sprintf("system-%d", idx),
							Kind:   fmt.Sprintf("event-%d", j),
							Data:   []byte("test"),
						}
						bus.Send(ctx, e)
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

func TestRouterAddHandlerAfterStart(t *testing.T) {
	bus := NewBus()
	defer bus.Stop()

	var receivedEvents []event.Event
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(2) // We expect 2 events

	// Create initial handler
	initialHandler := NewBaseHandler(
		Pattern{System: "test-system", Kind: "initial"},
		func(_ context.Context, evt event.Event) error {
			mu.Lock()
			receivedEvents = append(receivedEvents, evt)
			mu.Unlock()
			wg.Done()
			return nil
		},
	)

	router, err := StartRouter(context.Background(), bus, WithHandlers(initialHandler))
	require.NoError(t, err)
	defer router.Stop()

	// Add new handler after router is started
	newHandler := NewBaseHandler(
		Pattern{System: "test-system", Kind: "new"},
		func(_ context.Context, evt event.Event) error {
			mu.Lock()
			receivedEvents = append(receivedEvents, evt)
			mu.Unlock()
			wg.Done()
			return nil
		},
	)

	err = router.addHandler(newHandler)
	require.NoError(t, err)

	// Send events for both handlers
	initialEvent := event.Event{
		System: "test-system",
		Kind:   "initial",
		Data:   []byte("initial-data"),
	}
	newEvent := event.Event{
		System: "test-system",
		Kind:   "new",
		Data:   []byte("new-data"),
	}

	bus.Send(context.Background(), initialEvent)
	bus.Send(context.Background(), newEvent)

	// Wait for both events to be processed with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Both events processed
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for events")
	}

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, receivedEvents, 2)
	require.Contains(t, receivedEvents, initialEvent)
	require.Contains(t, receivedEvents, newEvent)
}

func TestRouterOptions(t *testing.T) {
	bus := NewBus()
	defer bus.Stop()

	// Test WithLogger
	logger := zap.NewNop()
	router, err := StartRouter(context.Background(), bus, WithLogger(logger))
	require.NoError(t, err)
	require.NotNil(t, router.log)
	router.Stop()

	// Test WithHandlers
	handler := NewBaseHandler(
		Pattern{System: "test-system", Kind: "test-kind"},
		func(_ context.Context, _ event.Event) error {
			return nil
		},
	)

	router, err = StartRouter(context.Background(), bus, WithHandlers(handler))
	require.NoError(t, err)
	require.NotNil(t, router.subscribers)
	require.Len(t, router.subscribers, 1)
	router.Stop()
}

func TestRouterHandlerErrorHandling(t *testing.T) {
	bus := NewBus()
	defer bus.Stop()

	var (
		errorReceived sync.WaitGroup
		errorCount    atomic.Int32
	)

	// Create a handler that returns an error
	handler := NewBaseHandler(
		Pattern{System: "test-system", Kind: "error"},
		func(_ context.Context, _ event.Event) error {
			errorCount.Add(1)
			errorReceived.Done()
			return fmt.Errorf("test error")
		},
	)

	router, err := StartRouter(context.Background(), bus, WithHandlers(handler))
	require.NoError(t, err)
	defer router.Stop()

	// Send multiple events
	numEvents := 3
	errorReceived.Add(numEvents)
	for i := 0; i < numEvents; i++ {
		e := event.Event{
			System: "test-system",
			Kind:   "error",
			Data:   []byte(fmt.Sprintf("data-%d", i)),
		}
		bus.Send(context.Background(), e)
	}

	// Wait for all errors to be processed
	errorReceived.Wait()

	// Verify all events were processed despite errors
	require.Equal(t, int32(numEvents), errorCount.Load())
}

func TestRouterShutdown(t *testing.T) {
	t.Run("graceful shutdown", func(t *testing.T) {
		bus := NewBus()
		defer bus.Stop()

		shutdownComplete := make(chan struct{})
		eventProcessed := make(chan struct{})
		handler := NewBaseHandler(
			Pattern{System: "test", Kind: "shutdown"},
			func(_ context.Context, evt event.Event) error {
				time.Sleep(100 * time.Millisecond) // Simulate some work
				close(eventProcessed)
				return nil
			},
		)

		router, err := StartRouter(context.Background(), bus, WithHandlers(handler))
		require.NoError(t, err)

		// Send an event that will be processed during shutdown
		e := event.Event{
			System: "test",
			Kind:   "shutdown",
			Data:   []byte("shutdown-test"),
		}
		bus.Send(context.Background(), e)

		// Wait for event to be processed before starting shutdown
		select {
		case <-eventProcessed:
			// Event processed, proceed with shutdown
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for event processing")
		}

		// Start shutdown in a goroutine
		go func() {
			assert.NoError(t, router.Stop())
			close(shutdownComplete)
		}()

		// Verify shutdown completes within reasonable time
		select {
		case <-shutdownComplete:
			// Test passed
		case <-time.After(time.Second):
			t.Fatal("shutdown timeout")
		}
	})

	t.Run("multiple shutdown calls", func(t *testing.T) {
		bus := NewBus()
		defer bus.Stop()

		router, err := StartRouter(context.Background(), bus)
		require.NoError(t, err)

		// First shutdown should succeed
		require.NoError(t, router.Stop())
		// Subsequent shutdowns should not error
		require.NoError(t, router.Stop())
		require.NoError(t, router.Stop())
	})
}

func TestRouterPatternMatching(t *testing.T) {
	t.Run("empty pattern matching", func(t *testing.T) {
		bus := NewBus()
		defer bus.Stop()

		eventReceived := make(chan struct{})
		handler := NewBaseHandler(
			Pattern{System: "", Kind: ""},
			func(_ context.Context, evt event.Event) error {
				close(eventReceived)
				return nil
			},
		)

		router, err := StartRouter(context.Background(), bus, WithHandlers(handler))
		require.NoError(t, err)
		defer router.Stop()

		e := event.Event{
			System: "",
			Kind:   "",
			Data:   []byte("empty-pattern"),
		}
		bus.Send(context.Background(), e)

		select {
		case <-eventReceived:
			// Test passed
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for event")
		}
	})

	t.Run("complex wildcard patterns", func(t *testing.T) {
		bus := NewBus()
		defer bus.Stop()

		type testCase struct {
			pattern     Pattern
			event       event.Event
			shouldMatch bool
		}

		testCases := []testCase{
			{
				pattern:     Pattern{System: "a.*.c", Kind: "x.*.z"},
				event:       event.Event{System: "a.b.c", Kind: "x.y.z"},
				shouldMatch: true,
			},
			{
				pattern:     Pattern{System: "a.**", Kind: "x"},
				event:       event.Event{System: "a.b.c.d", Kind: "x"},
				shouldMatch: true,
			},
			{
				pattern:     Pattern{System: "a.*", Kind: "x"},
				event:       event.Event{System: "a.b.c", Kind: "x"},
				shouldMatch: false,
			},
		}

		for i, tc := range testCases {
			t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
				eventReceived := make(chan struct{})
				handler := NewBaseHandler(
					tc.pattern,
					func(_ context.Context, evt event.Event) error {
						close(eventReceived)
						return nil
					},
				)

				router, err := StartRouter(context.Background(), bus, WithHandlers(handler))
				require.NoError(t, err)
				defer router.Stop()

				bus.Send(context.Background(), tc.event)

				select {
				case <-eventReceived:
					if !tc.shouldMatch {
						t.Error("unexpected pattern match")
					}
				case <-time.After(100 * time.Millisecond):
					if tc.shouldMatch {
						t.Error("expected pattern match")
					}
				}
			})
		}
	})
}

//
//func TestRouterHandlerRemoval(t *testing.T) {
//	bus := NewBus()
//	defer bus.Stop()
//
//	var receivedEvents []event.Event
//	var mu sync.Mutex
//	eventReceived := make(chan struct{})
//
//	handler := NewBaseHandler(
//		Pattern{System: "test", Kind: "remove"},
//		func(_ context.Context, evt event.Event) error {
//			mu.Lock()
//			receivedEvents = append(receivedEvents, evt)
//			mu.Unlock()
//			close(eventReceived)
//			return nil
//		},
//	)
//
//	router, err := StartRouter(context.Background(), bus, WithHandlers(handler))
//	require.NoError(t, err)
//	defer router.Stop()
//
//	// Send initial event
//	e1 := event.Event{
//		System: "test",
//		Kind:   "remove",
//		Data:   []byte("before-removal"),
//	}
//	bus.Send(context.Background(), e1)
//
//	select {
//	case <-eventReceived:
//		// First event received
//	case <-time.After(time.Second):
//		t.Fatal("timeout waiting for first event")
//	}
//
//	// Remove handler
//	require.NoError(t, router.removeHandler(handler))
//
//	// Reset channel for second event
//	eventReceived = make(chan struct{})
//
//	// Send second event
//	e2 := event.Event{
//		System: "test",
//		Kind:   "remove",
//		Data:   []byte("after-removal"),
//	}
//	bus.Send(context.Background(), e2)
//
//	// Verify second event is not received
//	select {
//	case <-eventReceived:
//		t.Error("unexpected event received after handler removal")
//	case <-time.After(100 * time.Millisecond):
//		// Expected timeout
//	}
//
//	mu.Lock()
//	defer mu.Unlock()
//	require.Len(t, receivedEvents, 1)
//	require.Equal(t, e1, receivedEvents[0])
//}
