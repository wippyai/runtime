package eventbus

import (
	"context"
	"sync"

	"github.com/ponyruntime/pony/api/events"
)

// Subscriber is a helper struct that simplifies subscribing to and handling events from an event bus.
type Subscriber struct {
	bus          events.Bus
	subscriberID events.SubscriberID
	handlerFunc  func(events.Event)
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

// NewSubscriber creates a new Subscriber that subscribes to events matching the given system and kind pattern.
// It starts an helpers goroutine that listens for events and calls the provided handlerFunc for each received event.
// The context provided will be used to start the listener and will be used during shutdown
func NewSubscriber(
	ctx context.Context,
	b events.Bus,
	system events.System,
	kind events.Kind,
	handlerFunc func(events.Event),
) (*Subscriber, error) {
	ctx, cancel := context.WithCancel(ctx)
	h := &Subscriber{
		bus:         b,
		handlerFunc: handlerFunc,
		ctx:         ctx,
		cancel:      cancel,
	}

	ch := make(chan events.Event)
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
		for evt := range ch {
			h.handlerFunc(evt)
		}
	}()

	go func() {
		<-h.ctx.Done()
		h.bus.Unsubscribe(context.Background(), h.subscriberID)
	}()

	return h, nil
}

// Close stops the helpers goroutine, unsubscribes from the event bus, and waits for the goroutine to exit.
func (s *Subscriber) Close() {
	s.cancel()
	s.wg.Wait()
}

// ID returns the subscriber ID.
func (s *Subscriber) ID() events.SubscriberID {
	return s.subscriberID
}
