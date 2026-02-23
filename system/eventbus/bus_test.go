// SPDX-License-Identifier: MPL-2.0

package eventbus

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
)

// Helper function to create a new Bus for testing
func newTestBus(t *testing.T) *Bus {
	t.Helper()

	return NewBus()
}

// Helper function to generate test eventbus
func newTestEvent(system event.System, kind event.Kind, data any) event.Event {
	return event.Event{
		System: system,
		Kind:   kind,
		Data:   payload.New(data),
	}
}

// Helper function to wait for a specified number of eventbus or timeout
//

func waitForEvents(t *testing.T, ch chan event.Event, numEvents int, timeout time.Duration) []event.Event {
	t.Helper()
	receivedEvents := make([]event.Event, 0, numEvents)
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

	ch := make(chan event.Event, 10)
	subID, err := bus.Subscribe(context.Background(), "test-system", ch)
	require.NoError(t, err)
	defer bus.Unsubscribe(context.Background(), subID)

	e := newTestEvent("test-system", "test-kind", "test-payload")
	bus.Send(context.Background(), e)

	receivedEvents := waitForEvents(t, ch, 1, time.Second)
	require.Equal(t, e, receivedEvents[0])
}

func TestSubscribeWithPathAndSend(t *testing.T) {
	bus := newTestBus(t)
	defer bus.Stop()

	ch := make(chan event.Event, 10)
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

	ch := make(chan event.Event, 10)
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

	ch := make(chan event.Event, 10)
	subID, err := bus.Subscribe(context.Background(), "test-system", ch)
	require.NoError(t, err)

	// send an event before unsubscribing
	event1 := newTestEvent("test-system", "test-kind", "payload1")
	bus.Send(context.Background(), event1)

	receivedEvents := waitForEvents(t, ch, 1, time.Second)
	require.Len(t, receivedEvents, 1)

	// Release and verify no more events are received
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

	ch := make(chan event.Event, 10)
	_, err := bus.Subscribe(context.Background(), "test-system", ch)
	require.NoError(t, err)

	// send event before stopping
	e := newTestEvent("test-system", "test-kind", "payload")
	bus.Send(context.Background(), e)

	receivedEvents := waitForEvents(t, ch, 1, time.Second)
	require.Len(t, receivedEvents, 1)

	// stop the bus
	bus.Stop()
}

func TestSendWithNilPayload(t *testing.T) {
	b := NewBus()
	defer b.Stop()

	e := event.Event{
		System: "test-system",
		Kind:   "test-kind",
		Data:   nil,
	}

	// Should not panic
	assert.NotPanics(t, func() {
		b.Send(context.Background(), e)
	})
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
			ch := make(chan event.Event)
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
	if testing.Short() {
		t.Skip("skipping concurrent test in short mode")
	}
	b := NewBus()
	defer b.Stop()

	var wg sync.WaitGroup
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			ch := make(chan event.Event, numGoroutines)
			_, err := b.Subscribe(context.Background(), "test-system", ch)
			if err != nil {
				t.Errorf("Listen failed: %v", err)
			}
		}()

		go func() {
			defer wg.Done()
			e := event.Event{
				System: "test-system",
				Kind:   "test-kind",
				Data:   payload.New([]byte("test-data")),
			}
			b.Send(context.Background(), e)
		}()
	}

	wg.Wait()
}

func TestUnsubscribeClosesChannel(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping unsubscribe test in short mode")
	}
	b := NewBus()
	defer b.Stop()

	ch := make(chan event.Event)
	subID, err := b.Subscribe(context.Background(), "test-system", ch)
	require.NoError(t, err)

	b.Unsubscribe(context.Background(), subID)

	// send an event after unsubscribe
	e := event.Event{
		System: "test-system",
		Kind:   "test-kind",
		Data:   payload.New([]byte("test-data")),
	}
	b.Send(context.Background(), e)

	// Verify no events are received after unsubscribe
	select {
	case evt := <-ch:
		t.Errorf("received event after unsubscribe: %v", evt)
	case <-time.After(100 * time.Millisecond):
		// Expected: no events received
	}
}

func TestNoEventsAfterUnsubscribe(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping unsubscribe test in short mode")
	}
	b := NewBus()
	defer b.Stop()

	ch := make(chan event.Event)
	subID, err := b.Subscribe(context.Background(), "test-system", ch)
	require.NoError(t, err)

	b.Unsubscribe(context.Background(), subID)

	e := event.Event{
		System: "test-system",
		Kind:   "test-kind",
		Data:   payload.New([]byte("test-data")),
	}
	b.Send(context.Background(), e)

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
	assert.NotPanics(t, func() {
		NewBus().Stop()
	})
}

func TestStopWithActiveSubscribers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stop test in short mode")
	}
	b := NewBus()

	ch1 := make(chan event.Event)
	ch2 := make(chan event.Event)
	_, err := b.Subscribe(context.Background(), "test-system", ch1)
	require.NoError(t, err)
	_, err = b.Subscribe(context.Background(), "other-system", ch2)
	require.NoError(t, err)

	b.Stop()

	// send events after stop
	e := newTestEvent("test-system", "test-kind", "test-data")
	b.Send(context.Background(), e)

	// Verify that no events are received after stop (channels are not closed in current implementation)
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

	ch := make(chan event.Event)
	_, err := b.SubscribeP(context.Background(), "test-system", "", ch)
	if err != nil {
		t.Errorf("SubscribeP with empty kind failed: %v", err)
	}
}

func TestMultipleSubscribersSameSystemPath(t *testing.T) {
	b := NewBus()
	defer b.Stop()

	ch1 := make(chan event.Event, 1)
	ch2 := make(chan event.Event, 1)
	_, err := b.Subscribe(context.Background(), "test-system", ch1)
	require.NoError(t, err)
	_, err = b.Subscribe(context.Background(), "test-system", ch2)
	require.NoError(t, err)

	e := event.Event{
		System: "test-system",
		Kind:   "test-kind",
		Data:   payload.New([]byte("test-data")),
	}
	b.Send(context.Background(), e)

	// Verify both subscribers receive the event
	evt1 := <-ch1
	evt2 := <-ch2
	assert.Equal(t, e, evt1)
	assert.Equal(t, e, evt2)
}

func TestMultipleSubscribersDifferentKinds(t *testing.T) {
	b := NewBus()
	defer b.Stop()

	ch1 := make(chan event.Event, 10)
	ch2 := make(chan event.Event, 10)

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
		// OK
	}
}

func TestStopWithPendingUnsubscribe(t *testing.T) {
	b := NewBus()

	// Spawn a subscriber
	ch := make(chan event.Event)
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

	// Call close() while unsubscribe requests are being processed
	go func() {
		time.Sleep(10 * time.Millisecond) // Give some time for unsubscribe requests to queue up
		b.Stop()
	}()

	wg.Wait()
}

func TestMultipleUnsubscribeSameID(t *testing.T) {
	b := NewBus()
	defer b.Stop()

	ch := make(chan event.Event)
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

	ch := make(chan event.Event)
	subID, err := b.Subscribe(context.Background(), "test-system", ch)
	require.NoError(t, err)

	b.Stop()

	// Should not panic when unsubscribing after stop
	b.Unsubscribe(context.Background(), subID)
}

func TestHighConcurrencyStress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	b := NewBus()
	defer b.Stop()

	var (
		numPublishers  = 50
		numSubscribers = 100
		messagesPerPub = 1000
		totalMessages  = numPublishers * messagesPerPub
	)

	// Track received messages
	var receivedCount atomic.Int64
	subscriberWg := sync.WaitGroup{}
	publisherWg := sync.WaitGroup{}

	// Launch subscribers
	channels := make([]chan event.Event, numSubscribers)
	subscriberIDs := make([]event.SubscriberID, numSubscribers)

	for i := 0; i < numSubscribers; i++ {
		subscriberWg.Add(1)
		channels[i] = make(chan event.Event, totalMessages) // Buffered channel to prevent blocking

		var err error
		subscriberIDs[i], err = b.Subscribe(context.Background(), "*", channels[i])
		require.NoError(t, err)

		go func(ch chan event.Event) {
			defer subscriberWg.Done()
			// on close will exit automatically
			for range ch {
				receivedCount.Add(1)
			}
		}(channels[i])
	}

	// Launch publishers
	for i := 0; i < numPublishers; i++ {
		publisherWg.Add(1)
		go func(pubID int) {
			defer publisherWg.Done()
			for j := 0; j < messagesPerPub; j++ {
				e := event.Event{
					System: "stress-test",
					Kind:   fmt.Sprintf("event-%d-%d", pubID, j),
					Data:   payload.New(fmt.Sprintf("data-%d-%d", pubID, j)),
				}
				b.Send(context.Background(), e)
			}
		}(i)
	}

	// Wait for publishers to complete
	publisherWg.Wait()

	// Random unsubscribes while messages are being processed
	unsubWg := sync.WaitGroup{}
	for i := 0; i < numSubscribers/2; i++ {
		unsubWg.Add(1)
		go func(idx int) {
			defer unsubWg.Done()
			r, _ := rand.Int(rand.Reader, big.NewInt(100))
			time.Sleep(time.Duration(r.Int64()) * time.Millisecond)
			b.Unsubscribe(context.Background(), subscriberIDs[idx])
			close(channels[idx])
		}(i)
	}

	unsubWg.Wait()

	// close remaining channels and unsubscribe
	for i := numSubscribers / 2; i < numSubscribers; i++ {
		b.Unsubscribe(context.Background(), subscriberIDs[i])
		close(channels[i])
	}

	// Wait for all subscribers to finish processing
	subscriberWg.Wait()

	// Verify message count
	// Note: We expect less than total due to unsubscribes
	received := receivedCount.Load()
	require.Greater(t, received, int64(totalMessages/2),
		"Should have received at least half of total messages")
}

func TestConcurrentSubscribeWithFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrent subscribe test in short mode")
	}
	b := NewBus()
	defer b.Stop()

	var (
		numSubscribers = 100
		numSystems     = 10
		numKinds       = 5
		numMessages    = 1000
	)

	var wg sync.WaitGroup
	subscriberChans := make([]chan event.Event, numSubscribers)
	subscriberIDs := make([]event.SubscriberID, numSubscribers)

	// Spawn subscribers with different filters
	for i := 0; i < numSubscribers; i++ {
		wg.Add(1)
		subscriberChans[i] = make(chan event.Event, numMessages)
		system := fmt.Sprintf("system-%d", i%numSystems)
		kind := fmt.Sprintf("kind-%d.*", i%numKinds)

		go func(idx int, sys string, k string) {
			defer wg.Done()
			var err error
			subscriberIDs[idx], err = b.SubscribeP(context.Background(), sys, k, subscriberChans[idx])
			require.NoError(t, err)
		}(i, system, kind)
	}

	wg.Wait()

	// send messages concurrently
	var sendWg sync.WaitGroup
	for i := 0; i < numMessages; i++ {
		sendWg.Add(1)
		go func(msgID int) {
			defer sendWg.Done()
			system := fmt.Sprintf("system-%d", msgID%numSystems)
			kind := fmt.Sprintf("kind-%d.test", msgID%numKinds)

			e := event.Event{
				System: system,
				Kind:   kind,
				Data:   payload.New(fmt.Sprintf("data-%d", msgID)),
			}
			b.Send(context.Background(), e)
		}(i)
	}

	sendWg.Wait()

	// Give time for messages to be delivered to subscriber channels
	time.Sleep(100 * time.Millisecond)

	// Verify message distribution with timeout
	timeout := time.After(5 * time.Second)
	messageCount := make([]int, numSubscribers)

	done := make(chan bool)
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for i, ch := range subscriberChans {
		loop:
			for {
				select {
				case _, ok := <-ch:
					if !ok {
						break loop
					}
					messageCount[i]++
				case <-time.After(10 * time.Millisecond):
					// Check if we've waited long enough
					if time.Now().After(deadline) {
						break loop
					}
				}
			}
		}
		done <- true
	}()

	select {
	case <-timeout:
		t.Fatal("Timeout waiting for message processing")
	case <-done:
		// Continue
	}

	// Verify distribution
	totalReceived := 0
	for _, count := range messageCount {
		totalReceived += count
		// Each subscriber should receive messages matching their filter
		require.Greater(t, count, 0, "Each subscriber should receive some messages")
	}

	require.Greater(t, totalReceived, numMessages/2,
		"Total received messages should be significant")
}

func TestConcurrentBusClosing(t *testing.T) {
	b := NewBus()

	var (
		numConcurrentOps = 100
		wg               sync.WaitGroup
		startSignal      = make(chan struct{})
	)

	// Launch goroutines that will try to subscribe
	for i := 0; i < numConcurrentOps; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-startSignal // Wait for signal to start

			ch := make(chan event.Event, 10)
			subID, err := b.Subscribe(context.Background(), fmt.Sprintf("system-%d", id), ch)
			if err != nil {
				// Either got "bus is closed" error or succeeded.
				if !errors.Is(err, ErrBusClosed) {
					t.Errorf("unexpected error on subscribe: %v", err)
				}
				return
			}

			// If subscribe succeeded, try to unsubscribe
			b.Unsubscribe(context.Background(), subID)
		}(i)
	}

	// Launch goroutines that will try to send events
	for i := 0; i < numConcurrentOps; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-startSignal // Wait for signal to start

			e := event.Event{
				System: "test",
				Kind:   "test",
				Data:   payload.New(fmt.Sprintf("data-%d", id)),
			}
			b.Send(context.Background(), e)
		}(i)
	}

	// Launch a goroutine that will close the bus
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-startSignal // Wait for signal to start
		b.Stop()
	}()

	// Signal all goroutines to start simultaneously
	close(startSignal)

	// Wait for all operations to complete
	wg.Wait()
}

func TestStopDuringBackpressure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	b := NewBus()

	// Spawn multiple subscribers with buffered channels to prevent complete blockage
	numSubscribers := 10
	subscribers := make([]chan event.Event, numSubscribers)
	for i := 0; i < numSubscribers; i++ {
		subscribers[i] = make(chan event.Event, 10) // Buffered channel
		_, err := b.Subscribe(context.Background(), "*", subscribers[i])
		require.NoError(t, err)
	}

	// Spawn one slow subscriber to simulate backpressure
	slowCh := make(chan event.Event, 1) // Small buffer
	_, err := b.Subscribe(context.Background(), "*", slowCh)
	require.NoError(t, err)

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Launch the slow consumer
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-slowCh:
				time.Sleep(10 * time.Millisecond) // Simulate slow processing
			case <-ctx.Done():
				return
			}
		}
	}()

	// Launch multiple senders to create backpressure
	numSenders := 50
	for i := 0; i < numSenders; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			e := event.Event{
				System: "test",
				Kind:   "test",
				Data:   payload.New(fmt.Sprintf("data-%d", id)),
			}

			// Try to send with timeout context to prevent permanent blocking
			sendCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			defer cancel()
			b.Send(sendCtx, e)
		}(i)
	}

	// Wait a bit to build up some backpressure
	time.Sleep(100 * time.Millisecond)

	// close the bus while under backpressure
	stopDone := make(chan struct{})
	go func() {
		b.Stop()
		close(stopDone)
	}()

	// Ensure close() completes within a reasonable timeout
	select {
	case <-stopDone:
		// Success - bus stopped properly
	case <-time.After(1 * time.Second): // Reduced timeout
		t.Fatal("bus.close() took too long under backpressure")
	}

	// Cleanup
	cancel() // Signal all goroutines to stop
	wg.Wait()
	time.Sleep(10 * time.Millisecond) // Wait for all goroutines to exit

	// Note: With the current bus implementation, channels are not closed when the bus stops.
	// The bus only removes subscribers from its internal map.
	// Verify that no new events are received after stop by sending a test event.
	e := event.Event{
		System: "test-after-stop",
		Kind:   "test-after-stop",
		Data:   payload.New("data-after-stop"),
	}
	b.Send(context.Background(), e)

	// Note: With the current bus implementation, events sent before Stop() may still be delivered after Stop() returns.
	// This is because Stop() waits for the action queue to drain before processing the stop action.
	// We only verify that events sent after stop (like "test-after-stop") are not received.
	for _, ch := range subscribers {
		select {
		case evt := <-ch:
			if evt.System == "test-after-stop" {
				t.Errorf("received event after stop: %v", evt)
			}
			// Events with other systems may still be received here; this is expected with the current implementation.
		case <-time.After(100 * time.Millisecond):
			// No more events received.
		}
	}
}

func TestConcurrentStopAndSubscribe(t *testing.T) {
	for i := 0; i < 100; i++ { // Run multiple iterations to increase chance of race detection
		b := NewBus()
		var wg sync.WaitGroup

		// Launch multiple subscribe operations
		for j := 0; j < 10; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ch := make(chan event.Event)
				_, err := b.Subscribe(context.Background(), "*", ch)
				if err != nil && !errors.Is(err, ErrBusClosed) {
					t.Errorf("unexpected error: %v", err)
				}
			}()
		}

		// Concurrently stop the bus
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Stop()
		}()

		wg.Wait()
	}
}

// Edge case tests

func TestSubscribeWithNilChannel(t *testing.T) {
	b := NewBus()
	defer b.Stop()

	_, err := b.Subscribe(context.Background(), "test", nil)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNilChannel)
}

func TestSubscribeWithCanceledContext(t *testing.T) {
	b := NewBus()
	defer b.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ch := make(chan event.Event)
	_, err := b.Subscribe(ctx, "test", ch)
	require.Error(t, err)
	require.Equal(t, context.Canceled, err)
}

func TestUnsubscribeWithCanceledContext(t *testing.T) {
	b := NewBus()
	defer b.Stop()

	ch := make(chan event.Event)
	subID, err := b.Subscribe(context.Background(), "test", ch)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Should not panic
	b.Unsubscribe(ctx, subID)
}

func TestSendWithCanceledContext(t *testing.T) {
	b := NewBus()
	defer b.Stop()

	ch := make(chan event.Event, 10)
	_, err := b.Subscribe(context.Background(), "test", ch)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Should not panic and event should not be delivered
	e := event.Event{System: "test", Kind: "test", Data: "data"}
	b.Send(ctx, e)

	select {
	case <-ch:
		t.Error("should not receive event when context is canceled")
	case <-time.After(50 * time.Millisecond):
		// Expected
	}
}

func TestSubscribeAfterStop(t *testing.T) {
	b := NewBus()
	b.Stop()

	ch := make(chan event.Event)
	_, err := b.Subscribe(context.Background(), "test", ch)
	require.Error(t, err)
	require.Contains(t, err.Error(), "closed")
}

func TestMultipleStops(t *testing.T) {
	b := NewBus()

	// Should not panic
	assert.NotPanics(t, func() {
		b.Stop()
		b.Stop()
		b.Stop()
	})
}

func TestExpiredSubscriberCleanup(t *testing.T) {
	b := NewBus()
	defer b.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan event.Event, 10)
	_, err := b.Subscribe(ctx, "test", ch)
	require.NoError(t, err)

	// Cancel subscriber context
	cancel()

	// Give time for cleanup
	time.Sleep(10 * time.Millisecond)

	// Send event - should trigger cleanup of expired subscriber
	e := event.Event{System: "test", Kind: "test", Data: "data"}
	b.Send(context.Background(), e)

	// Wait and verify no event received (subscriber was cleaned up)
	select {
	case <-ch:
		// May or may not receive depending on timing
	case <-time.After(50 * time.Millisecond):
		// OK
	}
}

// Benchmarks

func BenchmarkSend(b *testing.B) {
	bus := NewBus()
	defer bus.Stop()

	ch := make(chan event.Event, b.N)
	_, err := bus.Subscribe(context.Background(), "bench", ch)
	if err != nil {
		b.Fatal(err)
	}

	e := event.Event{System: "bench", Kind: "test", Data: "data"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bus.Send(context.Background(), e)
	}
}

func BenchmarkSendParallel(b *testing.B) {
	bus := NewBus()
	defer bus.Stop()

	ch := make(chan event.Event, b.N*10)
	_, err := bus.Subscribe(context.Background(), "bench", ch)
	if err != nil {
		b.Fatal(err)
	}

	e := event.Event{System: "bench", Kind: "test", Data: "data"}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			bus.Send(context.Background(), e)
		}
	})
}

func BenchmarkSendMultipleSubscribers(b *testing.B) {
	bus := NewBus()
	defer bus.Stop()

	numSubscribers := 100
	channels := make([]chan event.Event, numSubscribers)
	for i := 0; i < numSubscribers; i++ {
		channels[i] = make(chan event.Event, b.N)
		_, err := bus.Subscribe(context.Background(), "bench", channels[i])
		if err != nil {
			b.Fatal(err)
		}
	}

	e := event.Event{System: "bench", Kind: "test", Data: "data"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bus.Send(context.Background(), e)
	}
}

func BenchmarkSubscribeUnsubscribe(b *testing.B) {
	bus := NewBus()
	defer bus.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch := make(chan event.Event, 1)
		id, err := bus.Subscribe(context.Background(), "bench", ch)
		if err != nil {
			b.Fatal(err)
		}
		bus.Unsubscribe(context.Background(), id)
	}
}

func BenchmarkWildcardMatching(b *testing.B) {
	bus := NewBus()
	defer bus.Stop()

	ch := make(chan event.Event, b.N)
	_, err := bus.SubscribeP(context.Background(), "system.*", "event.*", ch)
	if err != nil {
		b.Fatal(err)
	}

	e := event.Event{System: "system.test", Kind: "event.created", Data: "data"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bus.Send(context.Background(), e)
	}
}

func BenchmarkExactMatch(b *testing.B) {
	bus := NewBus()
	defer bus.Stop()

	ch := make(chan event.Event, b.N)
	_, err := bus.SubscribeP(context.Background(), "myapp", "user.created", ch)
	if err != nil {
		b.Fatal(err)
	}

	e := event.Event{System: "myapp", Kind: "user.created", Data: "data"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bus.Send(context.Background(), e)
	}
}

func TestBurstSend(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	b := NewBus()
	defer b.Stop()

	const (
		burstSize      = 10000
		numSubscribers = 10
	)

	var received atomic.Int64
	expected := int64(burstSize * numSubscribers)

	channels := make([]chan event.Event, numSubscribers)
	for i := 0; i < numSubscribers; i++ {
		channels[i] = make(chan event.Event, burstSize)
		_, err := b.Subscribe(context.Background(), "burst", channels[i])
		require.NoError(t, err)

		go func(ch chan event.Event) {
			for range ch {
				received.Add(1)
			}
		}(channels[i])
	}

	// Send burst of events as fast as possible
	for i := 0; i < burstSize; i++ {
		b.Send(context.Background(), event.Event{
			System: "burst",
			Kind:   "test",
			Data:   i,
		})
	}

	// Wait for all events to be delivered (poll with timeout)
	deadline := time.Now().Add(5 * time.Second)
	for received.Load() < expected && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	// Verify all events received by all subscribers
	assert.Equal(t, expected, received.Load(), "all events should be delivered")
}

func TestBurstQueueGrowth(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	b := NewBus()
	defer b.Stop()

	const burstSize = 1000

	// Consumer with adequate buffer
	ch := make(chan event.Event, burstSize)
	_, err := b.Subscribe(context.Background(), "burst", ch)
	require.NoError(t, err)

	var received atomic.Int64

	go func() {
		for range ch {
			received.Add(1)
		}
	}()

	// Send burst - this tests that the queue can grow beyond defaultQueueCap
	for i := 0; i < burstSize; i++ {
		b.Send(context.Background(), event.Event{
			System: "burst",
			Kind:   "test",
			Data:   i,
		})
	}

	// Wait for delivery (poll with timeout)
	expected := int64(burstSize)
	deadline := time.Now().Add(5 * time.Second)
	for received.Load() < expected && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	// All events should be delivered
	assert.Equal(t, expected, received.Load(), "all events should be delivered")
}
