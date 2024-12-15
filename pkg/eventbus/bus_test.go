package eventbus

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

// Helper function to generate test eventbus
func newTestEvent(system events.System, kind events.Kind, data any) events.Event {
	return events.Event{
		System: system,
		Kind:   kind,
		Data:   payload.New(data),
	}
}

// Helper function to wait for a specified number of eventbus or timeout
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
			t.Fatalf("timed out waiting for eventbus. Received %d out of %d", len(receivedEvents), numEvents)
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

	event := newTestEvent("test-system", "test-kind", "test-payload")
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
	event1 := newTestEvent("test-system", "users.created", "test-payload")
	// Should receive this event
	event2 := newTestEvent("test-system", "users.updated", "test-payload")
	// Should NOT receive this event
	event3 := newTestEvent("test-system", "posts.created", "test-payload")

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

	event1 := newTestEvent("system1", "test-kind", "payload1")
	event2 := newTestEvent("system2", "test-kind", "payload2")

	bus.Send(context.Background(), event1)
	bus.Send(context.Background(), event2)

	receivedEvents := waitForEvents(t, ch, 2, time.Second)
	require.Len(t, receivedEvents, 2)
	require.Equal(t, event1, receivedEvents[0])
	require.Equal(t, event2, receivedEvents[1])
}

func TestUnsubscribe(t *testing.T) {
	bus, _ := newTestBus(t)
	defer bus.Stop()

	ch := make(chan events.Event, 10)
	subID, err := bus.Subscribe(context.Background(), "test-system", ch)
	require.NoError(t, err)

	// Send an event before unsubscribing
	event1 := newTestEvent("test-system", "test-kind", "payload1")
	bus.Send(context.Background(), event1)

	receivedEvents := waitForEvents(t, ch, 1, time.Second)
	require.Len(t, receivedEvents, 1)

	// Unsubscribe and verify no more eventbus are received
	bus.Unsubscribe(context.Background(), subID)

	event2 := newTestEvent("test-system", "test-kind", "payload2")
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
	event := newTestEvent("test-system", "test-kind", "payload")
	bus.Send(context.Background(), event)

	receivedEvents := waitForEvents(t, ch, 1, time.Second)
	require.Len(t, receivedEvents, 1)

	// Stop the bus
	bus.Stop()

	// Verify internal channel is closed
	_, ok := <-bus.fout

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
		System: "test-system",
		Kind:   "test-kind",
		Data:   nil,
	}

	bus.Send(context.Background(), event)

	// Verify no eventbus were sent and no logs were created
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
		System: "test-system",
		Kind:   "test-kind",
		Data:   nil,
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
				t.Errorf("Listen failed: %v", err)
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
			ch := make(chan events.Event, numGoroutines)
			_, err := b.Subscribe(context.Background(), "test-system", ch)
			if err != nil {
				t.Errorf("Listen failed: %v", err)
			}
		}()

		go func() {
			defer wg.Done()
			event := events.Event{
				System: "test-system",
				Kind:   "test-kind",
				Data:   payload.New([]byte("test-data")),
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
		System: "test-system",
		Kind:   "test-kind",
		Data:   payload.New([]byte("test-data")),
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
	case _, ok := <-b.fout:
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
	_, err := b.Subscribe(context.Background(), "test-system", ch1)
	if err != nil {
		t.Error("Listen failed: ", err)
	}
	_, err = b.Subscribe(context.Background(), "other-system", ch2)
	if err != nil {
		t.Error("Listen failed: ", err)
	}

	b.Stop()

	// Verify that subscriber channels are closed
	_, ok1 := <-ch1
	_, ok2 := <-ch2

	if ok1 || ok2 {
		t.Error("Subscriber channels should be closed when the bus is stopped")
	}
}

func TestSubscribePEmptyKind(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	b := NewBus(logger)
	defer b.Stop()

	ch := make(chan events.Event)
	_, err := b.SubscribeP(context.Background(), "test-system", "", ch)
	if err != nil {
		t.Errorf("SubscribeP with empty kind failed: %v", err)
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
		System: "test-system",
		Kind:   "test-kind",
		Data:   payload.New([]byte("test-data")),
	}
	b.Send(context.Background(), event)

	// Verify both subscribers receive the event
	<-ch1
	<-ch2
}

func TestMultipleSubscribersDifferentKinds(t *testing.T) {
	b := NewBus(zap.NewNop())
	defer b.Stop()

	ch1 := make(chan events.Event, 10)
	ch2 := make(chan events.Event, 10)

	id1, err := b.SubscribeP(context.Background(), "test-system", "users.*", ch1)
	require.NoError(t, err)
	defer b.Unsubscribe(context.Background(), id1)

	id2, err := b.SubscribeP(context.Background(), "test-system", "posts.*", ch2)
	require.NoError(t, err)
	defer b.Unsubscribe(context.Background(), id2)

	// Send eventbus that match different paths
	userEvent := newTestEvent("test-system", "users.created", "user-data")
	postEvent := newTestEvent("test-system", "posts.created", "post-data")
	otherEvent := newTestEvent("test-system", "other.created", "other-data")

	b.Send(context.Background(), userEvent)
	b.Send(context.Background(), postEvent)
	b.Send(context.Background(), otherEvent)

	// Verify only user subscriber receives user event
	select {
	case evt := <-ch1:
		require.Equal(t, userEvent, evt)
	case <-time.After(100 * time.Millisecond):
		t.Error("user subscriber should have received user event")
	}

	// Verify only post subscriber receives post event
	select {
	case evt := <-ch2:
		require.Equal(t, postEvent, evt)
	case <-time.After(100 * time.Millisecond):
		t.Error("post subscriber should have received post event")
	}

	select {
	case <-ch1:
		t.Error("user subscriber should not have received another event")
	case <-ch2:
		t.Error("post subscriber should not have received another event")
	case <-time.After(100 * time.Millisecond):
		//OK
	}
}
