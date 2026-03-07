// SPDX-License-Identifier: MPL-2.0

package eventbus

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/event"
)

// Subscriber is a helper struct that simplifies subscribing to and handling events from an event bus.
type Subscriber struct {
	bus          event.Bus
	ctx          context.Context
	handlerFunc  func(event.Event)
	cancel       context.CancelFunc
	subscriberID event.SubscriberID
	wg           sync.WaitGroup
}

// NewSubscriber creates a new Subscriber that subscribes to events matching the given system and kind pattern.
// It starts a helper goroutine that listens for events and calls the provided handlerFunc for each received event.
// The context provided will be used to start the listener and will be used during shutdown.
func NewSubscriber(
	ctx context.Context,
	b event.Bus,
	system event.System,
	kind event.Kind,
	handlerFunc func(event.Event),
) (*Subscriber, error) {
	ctx, cancel := context.WithCancel(ctx)
	h := &Subscriber{
		bus:         b,
		handlerFunc: handlerFunc,
		ctx:         ctx,
		cancel:      cancel,
	}

	ch := make(chan event.Event, subscriberChanBuffer)
	var err error
	if kind == "" || kind == "*" {
		h.subscriberID, err = b.Subscribe(ctx, system, ch)
	} else {
		h.subscriberID, err = b.SubscribeP(ctx, system, kind, ch)
	}

	if err != nil {
		cancel()
		return nil, err
	}

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()

		for {
			select {
			case evt, ok := <-ch:
				if !ok {
					return
				}
				select {
				case <-h.ctx.Done():
					return
				default:
					h.handlerFunc(evt)
				}
			case <-h.ctx.Done():
				return
			}
		}
	}()

	h.wg.Add(1)
	go func() { //nolint:gosec // G118: cleanup goroutine intentionally uses background context for unsubscribe after parent cancellation
		defer h.wg.Done()

		<-h.ctx.Done()
		h.bus.Unsubscribe(context.Background(), h.subscriberID)
		close(ch)
	}()

	return h, nil
}

// Close stops the helpers goroutine, unsubscribes from the event bus, and waits for the goroutine to exit.
func (s *Subscriber) Close() {
	s.cancel()
	s.wg.Wait() // blocks here waiting for goroutines
}

// ID returns the subscriber ID.
func (s *Subscriber) ID() event.SubscriberID {
	return s.subscriberID
}
