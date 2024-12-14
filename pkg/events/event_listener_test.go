package events

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/events"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// newTestBus is a helper function from your provided code.
// I'm including it here for completeness of the test.
func newTestBusForEvents(t *testing.T) events.Bus {
	t.Helper()
	return NewBus(zap.NewNop())
}
func TestEventListener_NewEventHandler(t *testing.T) {
	b := newTestBusForEvents(t)

	// Test subscribing with a specific kind
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var receivedEvents []events.Event
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(2)
	handlerFunc := func(b events.Bus, evt events.Event) {
		mu.Lock()
		receivedEvents = append(receivedEvents, evt)
		mu.Unlock()
		wg.Done()
	}

	handler, err := NewEventListener(ctx, b, "test-system", "test-kind.*", handlerFunc)
	require.NoError(t, err)
	defer handler.Close()

	// Send events
	event1 := events.Event{System: "test-system", Kind: "test-kind.created", Data: "data1"}
	event2 := events.Event{System: "test-system", Kind: "test-kind.updated", Data: "data2"}
	event3 := events.Event{System: "other-system", Kind: "test-kind.created", Data: "data3"} // Should not be received

	b.Send(context.Background(), event1)
	b.Send(context.Background(), event2)
	b.Send(context.Background(), event3)

	// Wait for the eventListener goroutine to exit
	wg.Wait()

	// Verify received events
	mu.Lock()
	require.Len(t, receivedEvents, 2)
	require.Equal(t, event1, receivedEvents[0])
	require.Equal(t, event2, receivedEvents[1])
	mu.Unlock()
}

func TestEventListener_NewEventHandler_NoKind(t *testing.T) {
	b := newTestBusForEvents(t)

	// Test subscribing without a specific kind
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var receivedEvents []events.Event
	var mu sync.Mutex // Mutex to protect receivedEvents
	var wg sync.WaitGroup
	wg.Add(2)
	handlerFunc := func(b events.Bus, evt events.Event) {
		mu.Lock()
		receivedEvents = append(receivedEvents, evt)
		mu.Unlock()
		wg.Done()
	}

	handler, err := NewEventListener(ctx, b, "test-system", "", handlerFunc)
	require.NoError(t, err)
	defer handler.Close()

	// Send events
	event1 := events.Event{System: "test-system", Kind: "test-kind.created", Data: "data1"}
	event2 := events.Event{System: "test-system", Kind: "test-kind.updated", Data: "data2"}
	event3 := events.Event{System: "other-system", Kind: "test-kind.created", Data: "data3"} // Should not be received

	b.Send(context.Background(), event1)
	b.Send(context.Background(), event2)
	b.Send(context.Background(), event3)

	// Wait for the eventListener goroutine to exit
	wg.Wait()

	// Verify received events
	mu.Lock()
	require.Len(t, receivedEvents, 2)
	require.Equal(t, event1, receivedEvents[0])
	require.Equal(t, event2, receivedEvents[1])
	mu.Unlock()
}

func TestEventListener_Close(t *testing.T) {
	b := newTestBusForEvents(t)

	// Test closing the event handler
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var receivedEvents []events.Event
	var mu sync.Mutex // Mutex to protect receivedEvents
	var wg sync.WaitGroup
	wg.Add(1)
	handlerFunc := func(b events.Bus, evt events.Event) {
		mu.Lock()
		receivedEvents = append(receivedEvents, evt)
		mu.Unlock()
		wg.Done()
	}

	handler, err := NewEventListener(ctx, b, "test-system", "test-kind.*", handlerFunc)
	require.NoError(t, err)

	// Send events
	event1 := events.Event{System: "test-system", Kind: "test-kind.created", Data: "data1"}
	event2 := events.Event{System: "test-system", Kind: "test-kind.updated", Data: "data2"}

	b.Send(context.Background(), event1)

	wg.Wait()

	// Close the handler
	handler.Close()

	// Send another event
	b.Send(context.Background(), event2)

	// Verify received events
	mu.Lock()
	require.Len(t, receivedEvents, 1)
	require.Equal(t, event1, receivedEvents[0])
	mu.Unlock()
}

func TestEventListener_ContextCancellation(t *testing.T) {
	b := newTestBusForEvents(t)
	ctx, cancel := context.WithCancel(context.Background())

	var receivedEvents []events.Event
	var mu sync.Mutex
	handlerFunc := func(b events.Bus, evt events.Event) {
		mu.Lock()
		receivedEvents = append(receivedEvents, evt)
		mu.Unlock()
	}

	handler, err := NewEventListener(ctx, b, "test-system", "", handlerFunc)
	require.NoError(t, err)
	defer handler.Close() // Ensure handler is closed even if the test fails

	// Cancel the context
	cancel()

	time.Sleep(100 * time.Millisecond)

	event1 := events.Event{System: "test-system", Kind: "test-kind.created", Data: "data1"}
	b.Send(context.Background(), event1)

	mu.Lock()
	require.Len(t, receivedEvents, 0)
	mu.Unlock()
}
