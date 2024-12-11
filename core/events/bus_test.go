package events

import (
	"context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"sync"
	"testing"
	"time"
)

// Helper function to create a new Bus with an observed logger for testing
func newTestBus(t *testing.T) (*Bus, *observer.ObservedLogs) {
	t.Helper()

	observedCore, observedLogs := observer.New(zapcore.DebugLevel)
	logger := zap.New(observedCore)

	return NewBus(logger), observedLogs
}

// Helper function to generate test events
func newTestEvent(system events.System, kind events.Kind, path string, data any) events.Event {
	return events.Event{
		System:  system,
		Kind:    kind,
		Path:    events.Path(path),
		Payload: payload.New(data),
	}
}

// Helper function to wait for a specified number of events or timeout
func waitForEvents(t *testing.T, ch chan events.Event, numEvents int, timeout time.Duration) []events.Event {
	t.Helper()
	receivedEvents := make([]events.Event, 0, numEvents)
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for i := 0; i < numEvents; i++ {
		select {
		case evt := <-ch:
			receivedEvents = append(receivedEvents, evt)
		case <-timer.C:
			t.Fatalf("timed out waiting for events. Received %d out of %d", len(receivedEvents), numEvents)
		}
	}

	return receivedEvents
}

func TestSubscribeAndSend(t *testing.T) {
	bus, logs := newTestBus(t)
	defer bus.Stop()

	ch := make(chan events.Event, 10)
	subID, err := bus.Subscribe(context.Background(), "test-system", ch)
	require.NoError(t, err)
	defer bus.Unsubscribe(context.Background(), subID)

	event := newTestEvent("test-system", "test-kind", "path/to/resource", "test-payload")
	bus.Send(context.Background(), event)

	receivedEvents := waitForEvents(t, ch, 1, time.Second)
	require.Equal(t, event, receivedEvents[0])

	// Wait a bit for logs to be written
	time.Sleep(50 * time.Millisecond)

	entries := logs.All()
	require.GreaterOrEqual(t, len(entries), 1) // Should see "sending" log

	var foundSending bool
	for _, entry := range entries {
		if entry.Message == "sending event" {
			foundSending = true
			// Verify log fields
			require.Equal(t, "test-system", entry.ContextMap()["system"])
			require.Equal(t, "test-kind", entry.ContextMap()["kind"])
			require.Equal(t, "path/to/resource", entry.ContextMap()["path"])
		}
	}
	require.True(t, foundSending, "should find 'sending event' log")
}

func TestSubscribeWithPathAndSend(t *testing.T) {
	bus, _ := newTestBus(t)
	defer bus.Stop()

	ch := make(chan events.Event, 10)
	subID, err := bus.SubscribeP(context.Background(), "test-system", "users.*", ch)
	require.NoError(t, err)
	defer bus.Unsubscribe(context.Background(), subID)

	// Should receive this event
	event1 := newTestEvent("test-system", "test-kind", "users.created", "test-payload")
	// Should receive this event
	event2 := newTestEvent("test-system", "test-kind", "users.updated", "test-payload")
	// Should NOT receive this event
	event3 := newTestEvent("test-system", "test-kind", "posts.created", "test-payload")

	bus.Send(context.Background(), event1)
	bus.Send(context.Background(), event2)
	bus.Send(context.Background(), event3)

	receivedEvents := waitForEvents(t, ch, 2, time.Second)
	require.Len(t, receivedEvents, 2)
	require.Equal(t, event1, receivedEvents[0])
	require.Equal(t, event2, receivedEvents[1])
}

func TestWildcardSystem(t *testing.T) {
	bus, _ := newTestBus(t)
	defer bus.Stop()

	ch := make(chan events.Event, 10)
	subID, err := bus.Subscribe(context.Background(), "*", ch)
	require.NoError(t, err)
	defer bus.Unsubscribe(context.Background(), subID)

	event1 := newTestEvent("system1", "test-kind", "resource1", "payload1")
	event2 := newTestEvent("system2", "test-kind", "resource2", "payload2")

	bus.Send(context.Background(), event1)
	bus.Send(context.Background(), event2)

	receivedEvents := waitForEvents(t, ch, 2, time.Second)
	require.Len(t, receivedEvents, 2)
	require.Equal(t, event1, receivedEvents[0])
	require.Equal(t, event2, receivedEvents[1])
}

func TestChannelFullBehavior(t *testing.T) {
	bus, logs := newTestBus(t)
	defer bus.Stop()

	// Create a channel with size 1 to test overflow
	ch := make(chan events.Event, 1)
	subID, err := bus.Subscribe(context.Background(), "test-system", ch)
	require.NoError(t, err)
	defer bus.Unsubscribe(context.Background(), subID)

	// Fill the channel
	event1 := newTestEvent("test-system", "test-kind", "resource", "payload1")
	event2 := newTestEvent("test-system", "test-kind", "resource", "payload2")

	bus.Send(context.Background(), event1)
	bus.Send(context.Background(), event2)

	// Wait for logs to be written
	time.Sleep(50 * time.Millisecond)

	// Check for warning log about full channel
	var foundWarning bool
	for _, entry := range logs.All() {
		if entry.Message == "subscriber channel full, dropping event" {
			foundWarning = true
			break
		}
	}
	require.True(t, foundWarning, "should log warning about full channel")
}

func TestUnsubscribe(t *testing.T) {
	bus, _ := newTestBus(t)
	defer bus.Stop()

	ch := make(chan events.Event, 10)
	subID, err := bus.Subscribe(context.Background(), "test-system", ch)
	require.NoError(t, err)

	// Send an event before unsubscribing
	event1 := newTestEvent("test-system", "test-kind", "resource", "payload1")
	bus.Send(context.Background(), event1)

	receivedEvents := waitForEvents(t, ch, 1, time.Second)
	require.Len(t, receivedEvents, 1)

	// Unsubscribe and verify no more events are received
	bus.Unsubscribe(context.Background(), subID)

	event2 := newTestEvent("test-system", "test-kind", "resource", "payload2")
	bus.Send(context.Background(), event2)

	// Verify channel is closed
	_, ok := <-ch
	require.False(t, ok, "channel should be closed after unsubscribe")
}

func TestBusStop(t *testing.T) {
	bus, _ := newTestBus(t)

	ch := make(chan events.Event, 10)
	_, err := bus.Subscribe(context.Background(), "test-system", ch)
	require.NoError(t, err)

	// Send event before stopping
	event := newTestEvent("test-system", "test-kind", "resource", "payload")
	bus.Send(context.Background(), event)

	receivedEvents := waitForEvents(t, ch, 1, time.Second)
	require.Len(t, receivedEvents, 1)

	// Stop the bus
	bus.Stop()

	// Verify internal channel is closed
	_, ok := <-bus.internalEvCh
	require.False(t, ok, "internal event channel should be closed after stop")
}

func TestNilPayload(t *testing.T) {
	bus, logs := newTestBus(t)
	defer bus.Stop()

	ch := make(chan events.Event, 10)
	subID, err := bus.Subscribe(context.Background(), "test-system", ch)
	require.NoError(t, err)
	defer bus.Unsubscribe(context.Background(), subID)

	// Create event with nil payload
	event := events.Event{
		System:  "test-system",
		Kind:    "test-kind",
		Path:    "resource",
		Payload: nil,
	}

	bus.Send(context.Background(), event)

	// Verify no events were sent and no logs were created
	select {
	case <-ch:
		t.Fatal("should not receive event with nil payload")
	case <-time.After(100 * time.Millisecond):
		// Expected behavior
	}

	require.Empty(t, logs.All(), "should not log anything for nil payload")
}

func TestSendWithNilPayload(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	b := NewBus(logger)
	defer b.Stop()

	event := events.Event{
		System:  "test-system",
		Kind:    "test-kind",
		Path:    "test.path",
		Payload: nil,
	}

	b.Send(context.Background(), event)
}

func TestConcurrentSubscribeUnsubscribe(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	b := NewBus(logger)
	defer b.Stop()

	var wg sync.WaitGroup
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := make(chan events.Event)
			subID, err := b.Subscribe(context.Background(), "test-system", ch)
			if err != nil {
				t.Errorf("Subscribe failed: %v", err)
				return
			}
			time.Sleep(time.Millisecond * 10) // Simulate some work
			b.Unsubscribe(context.Background(), subID)
		}()
	}

	wg.Wait()
}

func TestConcurrentSendSubscribe(t *testing.T) {
	b := NewBus(zap.NewNop())
	defer b.Stop()

	var wg sync.WaitGroup
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			ch := make(chan events.Event)
			_, err := b.Subscribe(context.Background(), "test-system", ch)
			if err != nil {
				t.Errorf("Subscribe failed: %v", err)
			}
		}()

		go func() {
			defer wg.Done()
			event := events.Event{
				System:  "test-system",
				Kind:    "test-kind",
				Path:    "test.path",
				Payload: payload.New([]byte("test-data")),
			}
			b.Send(context.Background(), event)
		}()
	}

	wg.Wait()
}

func TestUnsubscribeClosesChannel(t *testing.T) {
	b := NewBus(zap.NewNop())
	defer b.Stop()

	ch := make(chan events.Event)
	subID, _ := b.Subscribe(context.Background(), "test-system", ch)

	b.Unsubscribe(context.Background(), subID)

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("Channel should be closed after unsubscribe")
		}
	case <-time.After(time.Millisecond * 100):
		t.Error("Timeout waiting for channel closure")
	}
}

func TestNoEventsAfterUnsubscribe(t *testing.T) {
	b := NewBus(zap.NewNop())
	defer b.Stop()

	ch := make(chan events.Event)
	subID, _ := b.Subscribe(context.Background(), "test-system", ch)

	b.Unsubscribe(context.Background(), subID)

	event := events.Event{
		System:  "test-system",
		Kind:    "test-kind",
		Path:    "test.path",
		Payload: payload.New([]byte("test-data")),
	}
	b.Send(context.Background(), event)

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("Received event after unsubscribe")
		}
	case <-time.After(time.Millisecond * 100):
		// Expected behavior
	}
}

func TestStopBusClosesInternalChannel(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	b := NewBus(logger)

	b.Stop()

	select {
	case _, ok := <-b.internalEvCh:
		if ok {
			t.Error("Internal event channel should be closed after Stop")
		}
	case <-time.After(time.Millisecond * 100):
		t.Error("Timeout waiting for internal event channel closure")
	}
}

func TestStopWithActiveSubscribers(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	b := NewBus(logger)

	ch1 := make(chan events.Event)
	ch2 := make(chan events.Event)
	_, _ = b.Subscribe(context.Background(), "test-system", ch1)
	_, _ = b.Subscribe(context.Background(), "other-system", ch2)

	b.Stop()

	// Verify that subscriber channels are closed
	_, ok1 := <-ch1
	_, ok2 := <-ch2

	if ok1 || ok2 {
		t.Error("Subscriber channels should be closed when the bus is stopped")
	}
}

func TestSubscribePEmptyPath(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	b := NewBus(logger)
	defer b.Stop()

	ch := make(chan events.Event)
	_, err := b.SubscribeP(context.Background(), "test-system", "", ch)
	if err != nil {
		t.Errorf("SubscribeP with empty path failed: %v", err)
		// ...
	}
}

func TestMultipleSubscribersSameSystemPath(t *testing.T) {
	b := NewBus(zap.NewNop())
	defer b.Stop()

	ch1 := make(chan events.Event, 1)
	ch2 := make(chan events.Event, 1)
	_, _ = b.Subscribe(context.Background(), "test-system", ch1)
	_, _ = b.Subscribe(context.Background(), "test-system", ch2)

	event := events.Event{
		System:  "test-system",
		Kind:    "test-kind",
		Path:    "test.path",
		Payload: payload.New([]byte("test-data")),
	}
	b.Send(context.Background(), event)

	// Verify both subscribers receive the event
	<-ch1
	<-ch2
}
