package eventbus

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/stretchr/testify/require"
)

// Helper function to create a new Bus for testing
func newTestBus(t *testing.T) *Bus {
	t.Helper()

	return NewBus()
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
	bus := newTestBus(t)
	defer bus.Stop()

	ch := make(chan events.Event, 10)
	subID, err := bus.Subscribe(context.Background(), "test-system", ch)
	require.NoError(t, err)
	defer bus.Unsubscribe(context.Background(), subID)

	event := newTestEvent("test-system", "test-kind", "test-payload")
	bus.Send(context.Background(), event)

	receivedEvents := waitForEvents(t, ch, 1, time.Second)
	require.Equal(t, event, receivedEvents[0])
}

func TestSubscribeWithPathAndSend(t *testing.T) {
	bus := newTestBus(t)
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
	bus := newTestBus(t)
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
	bus := newTestBus(t)
	defer bus.Stop()

	ch := make(chan events.Event, 10)
	subID, err := bus.Subscribe(context.Background(), "test-system", ch)
	require.NoError(t, err)

	// send an event before unsubscribing
	event1 := newTestEvent("test-system", "test-kind", "payload1")
	bus.Send(context.Background(), event1)

	receivedEvents := waitForEvents(t, ch, 1, time.Second)
	require.Len(t, receivedEvents, 1)

	// Unsubscribe and verify no more events are received
	bus.Unsubscribe(context.Background(), subID)

	event2 := newTestEvent("test-system", "test-kind", "payload2")
	bus.Send(context.Background(), event2)

	// Verify no new events are received
	select {
	case evt := <-ch:
		t.Errorf("received unexpected event after unsubscribe: %v", evt)
	case <-time.After(100 * time.Millisecond):
		// Expected: no events received after unsubscribe
	}
}
func TestBusStop(t *testing.T) {
	bus := newTestBus(t)

	ch := make(chan events.Event, 10)
	_, err := bus.Subscribe(context.Background(), "test-system", ch)
	require.NoError(t, err)

	// send event before stopping
	event := newTestEvent("test-system", "test-kind", "payload")
	bus.Send(context.Background(), event)

	receivedEvents := waitForEvents(t, ch, 1, time.Second)
	require.Len(t, receivedEvents, 1)

	// stop the bus
	bus.Stop()
}

func TestSendWithNilPayload(t *testing.T) {
	b := NewBus()
	defer b.Stop()

	event := events.Event{
		System: "test-system",
		Kind:   "test-kind",
		Data:   nil,
	}

	b.Send(context.Background(), event)
}

func TestConcurrentSubscribeUnsubscribe(t *testing.T) {
	b := NewBus()
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
	b := NewBus()
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
	b := NewBus()
	defer b.Stop()

	ch := make(chan events.Event)
	subID, _ := b.Subscribe(context.Background(), "test-system", ch)

	b.Unsubscribe(context.Background(), subID)

	// Send an event after unsubscribe
	event := events.Event{
		System: "test-system",
		Kind:   "test-kind",
		Data:   payload.New([]byte("test-data")),
	}
	b.Send(context.Background(), event)

	// Verify no events are received after unsubscribe
	select {
	case evt := <-ch:
		t.Errorf("received event after unsubscribe: %v", evt)
	case <-time.After(100 * time.Millisecond):
		// Expected: no events received
	}
}

func TestNoEventsAfterUnsubscribe(t *testing.T) {
	b := NewBus()
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
	b := NewBus()

	b.Stop()
}

func TestStopWithActiveSubscribers(t *testing.T) {
	b := NewBus()

	ch1 := make(chan events.Event)
	ch2 := make(chan events.Event)
	_, err := b.Subscribe(context.Background(), "test-system", ch1)
	require.NoError(t, err)
	_, err = b.Subscribe(context.Background(), "other-system", ch2)
	require.NoError(t, err)

	b.Stop()

	// Send events after stop
	event := newTestEvent("test-system", "test-kind", "test-data")
	b.Send(context.Background(), event)

	// Verify that no events are received after stop
	select {
	case evt := <-ch1:
		t.Errorf("received unexpected event after stop on ch1: %v", evt)
	case evt := <-ch2:
		t.Errorf("received unexpected event after stop on ch2: %v", evt)
	case <-time.After(100 * time.Millisecond):
		// Expected: no events received after stop
	}
}

func TestSubscribePEmptyKind(t *testing.T) {
	b := NewBus()
	defer b.Stop()

	ch := make(chan events.Event)
	_, err := b.SubscribeP(context.Background(), "test-system", "", ch)
	if err != nil {
		t.Errorf("SubscribeP with empty kind failed: %v", err)
	}
}

func TestMultipleSubscribersSameSystemPath(t *testing.T) {
	b := NewBus()
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
	b := NewBus()
	defer b.Stop()

	ch1 := make(chan events.Event, 10)
	ch2 := make(chan events.Event, 10)

	id1, err := b.SubscribeP(context.Background(), "test-system", "users.*", ch1)
	require.NoError(t, err)
	defer b.Unsubscribe(context.Background(), id1)

	id2, err := b.SubscribeP(context.Background(), "test-system", "posts.*", ch2)
	require.NoError(t, err)
	defer b.Unsubscribe(context.Background(), id2)

	// send eventbus that match different paths
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

func TestStopWithPendingUnsubscribe(t *testing.T) {
	b := NewBus()

	// Create a subscriber
	ch := make(chan events.Event)
	subID, err := b.Subscribe(context.Background(), "test-system", ch)
	require.NoError(t, err)

	// Fill up the actions channel with unsubscribe requests
	// The channel buffer is 100, so we'll add more than that
	var wg sync.WaitGroup
	for i := 0; i < 150; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Unsubscribe(context.Background(), subID)
		}()
	}

	// Call Stop() while unsubscribe requests are being processed
	go func() {
		time.Sleep(10 * time.Millisecond) // Give some time for unsubscribe requests to queue up
		b.Stop()
	}()

	wg.Wait()
}

func TestMultipleUnsubscribeSameID(t *testing.T) {
	b := NewBus()
	defer b.Stop()

	ch := make(chan events.Event)
	subID, err := b.Subscribe(context.Background(), "test-system", ch)
	require.NoError(t, err)

	// Try to unsubscribe multiple times concurrently
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Unsubscribe(context.Background(), subID)
		}()
	}

	wg.Wait()
}

func TestUnsubscribeAfterStop(t *testing.T) {
	b := NewBus()

	ch := make(chan events.Event)
	subID, err := b.Subscribe(context.Background(), "test-system", ch)
	require.NoError(t, err)

	b.Stop()

	// Should not panic when unsubscribing after stop
	b.Unsubscribe(context.Background(), subID)
}
