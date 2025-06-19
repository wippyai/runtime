package eventbus

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/event"
	"github.com/stretchr/testify/require"
)

// newTestBus is a helper function from your provided code.
// I'm including it here for completeness of the test.
func newTestBusForEvents(t *testing.T) event.Bus {
	t.Helper()
	return NewBus()
}

func TestEventListener_NewEventListener(t *testing.T) {
	b := newTestBusForEvents(t)

	// Test subscribing with a specific kind
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var receivedEvents []event.Event
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(2)
	handlerFunc := func(evt event.Event) {
		mu.Lock()
		receivedEvents = append(receivedEvents, evt)
		mu.Unlock()
		wg.Done()
	}

	handler, err := NewSubscriber(ctx, b, "test-system", "test-kind.*", handlerFunc)
	require.NoError(t, err)
	defer handler.Close()

	// send eventbus
	event1 := event.Event{System: "test-system", Kind: "test-kind.created", Data: "data1"}
	event2 := event.Event{System: "test-system", Kind: "test-kind.updated", Data: "data2"}
	event3 := event.Event{System: "other-system", Kind: "test-kind.created", Data: "data3"} // Should not be received

	b.Send(context.Background(), event1)
	b.Send(context.Background(), event2)
	b.Send(context.Background(), event3)

	// wait for the eventListener goroutine to exit
	wg.Wait()

	// Verify received eventbus
	mu.Lock()
	require.Len(t, receivedEvents, 2)
	require.Equal(t, event1, receivedEvents[0])
	require.Equal(t, event2, receivedEvents[1])
	mu.Unlock()
}

func TestEventListener_NewEventListener_NoKind(t *testing.T) {
	b := newTestBusForEvents(t)

	// Test subscribing without a specific kind
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var receivedEvents []event.Event
	var mu sync.Mutex // Mutex to protect receivedEvents
	var wg sync.WaitGroup
	wg.Add(2)
	handlerFunc := func(evt event.Event) {
		mu.Lock()
		receivedEvents = append(receivedEvents, evt)
		mu.Unlock()
		wg.Done()
	}

	handler, err := NewSubscriber(ctx, b, "test-system", "", handlerFunc)
	require.NoError(t, err)
	defer handler.Close()

	// send eventbus
	event1 := event.Event{System: "test-system", Kind: "test-kind.created", Data: "data1"}
	event2 := event.Event{System: "test-system", Kind: "test-kind.updated", Data: "data2"}
	event3 := event.Event{System: "other-system", Kind: "test-kind.created", Data: "data3"} // Should not be received

	b.Send(context.Background(), event1)
	b.Send(context.Background(), event2)
	b.Send(context.Background(), event3)

	// wait for the eventListener goroutine to exit
	wg.Wait()

	// Verify received eventbus
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

	var receivedEvents []event.Event
	var mu sync.Mutex // Mutex to protect receivedEvents
	var wg sync.WaitGroup
	wg.Add(1)
	handlerFunc := func(evt event.Event) {
		mu.Lock()
		receivedEvents = append(receivedEvents, evt)
		mu.Unlock()
		wg.Done()
	}

	handler, err := NewSubscriber(ctx, b, "test-system", "test-kind.*", handlerFunc)
	require.NoError(t, err)

	// send eventbus
	event1 := event.Event{System: "test-system", Kind: "test-kind.created", Data: "data1"}
	event2 := event.Event{System: "test-system", Kind: "test-kind.updated", Data: "data2"}

	b.Send(context.Background(), event1)

	wg.Wait()

	// close the handler
	handler.Close()

	// send another event
	b.Send(context.Background(), event2)

	// Verify received eventbus
	mu.Lock()
	require.Len(t, receivedEvents, 1)
	require.Equal(t, event1, receivedEvents[0])
	mu.Unlock()
}

func TestEventListener_ContextCancellation(t *testing.T) {
	b := newTestBusForEvents(t)
	ctx, cancel := context.WithCancel(context.Background())

	var receivedEvents []event.Event
	var mu sync.Mutex
	handlerFunc := func(evt event.Event) {
		mu.Lock()
		receivedEvents = append(receivedEvents, evt)
		mu.Unlock()
	}

	handler, err := NewSubscriber(ctx, b, "test-system", "", handlerFunc)
	require.NoError(t, err)
	defer handler.Close() // Ensure handler is closed even if the test fails

	// Cancel the context
	cancel()

	time.Sleep(100 * time.Millisecond)

	event1 := event.Event{System: "test-system", Kind: "test-kind.created", Data: "data1"}
	b.Send(context.Background(), event1)

	mu.Lock()
	require.Len(t, receivedEvents, 0)
	mu.Unlock()
}

func TestSubscriberConcurrentHandlerExecution(t *testing.T) {
	b := newTestBusForEvents(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		mu            sync.Mutex
		executionTime time.Duration
		startTime     = time.Now()
		handlerDone   sync.WaitGroup
	)

	// Create a handler that simulates concurrent execution
	handlerFunc := func(evt event.Event) {
		defer handlerDone.Done()
		time.Sleep(50 * time.Millisecond) // Simulate work
		mu.Lock()
		executionTime = time.Since(startTime)
		mu.Unlock()
	}

	handler, err := NewSubscriber(ctx, b, "test-system", "test-kind.*", handlerFunc)
	require.NoError(t, err)
	defer handler.Close()

	// Send multiple events concurrently
	numEvents := 5
	handlerDone.Add(numEvents)
	for i := 0; i < numEvents; i++ {
		go func(idx int) {
			e := event.Event{
				System: "test-system",
				Kind:   fmt.Sprintf("test-kind.event-%d", idx),
				Data:   fmt.Sprintf("data-%d", idx),
			}
			b.Send(context.Background(), e)
		}(i)
	}

	// Wait for all handlers to complete
	handlerDone.Wait()

	// Verify concurrent execution
	mu.Lock()
	defer mu.Unlock()
	require.Less(t, executionTime, time.Duration(numEvents)*100*time.Millisecond,
		"Handlers should execute concurrently")
}

func TestSubscriberHandlerTimeout(t *testing.T) {
	b := newTestBusForEvents(t)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	var (
		mu            sync.Mutex
		handlerCalled bool
	)
	handlerFunc := func(evt event.Event) {
		mu.Lock()
		handlerCalled = true
		mu.Unlock()
		time.Sleep(200 * time.Millisecond) // Simulate work longer than context timeout
	}

	handler, err := NewSubscriber(ctx, b, "test-system", "test-kind.*", handlerFunc)
	require.NoError(t, err)
	defer handler.Close()

	// Send an event
	e := event.Event{
		System: "test-system",
		Kind:   "test-kind.event",
		Data:   "test-data",
	}
	b.Send(context.Background(), e)

	// Wait for context timeout
	time.Sleep(150 * time.Millisecond)

	// Verify handler was called but didn't complete
	mu.Lock()
	wasCalled := handlerCalled
	mu.Unlock()
	require.True(t, wasCalled, "Handler should have been called")
}
