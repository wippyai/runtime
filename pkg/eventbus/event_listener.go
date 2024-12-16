package eventbus

import (
	"context"
	"github.com/ponyruntime/pony/api/events"
	"sync"
)

// EventHandler is a helper struct that simplifies subscribing to and handling events from an event bus.
type EventHandler struct {
	bus          events.Bus
	subscriberID events.SubscriberID
	handlerFunc  func(events.Bus, events.Event)
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

// NewEventListener creates a new EventHandler that subscribes to events matching the given system and kind pattern.
// It starts an internal goroutine that listens for events and calls the provided handlerFunc for each received event.
// The context provided will be used to start the listener and will be used during shutdown
func NewEventListener(
	ctx context.Context,
	b events.Bus,
	system events.System,
	kind events.Kind,
	handlerFunc func(events.Bus, events.Event),
) (*EventHandler, error) {
	ctx, cancel := context.WithCancel(ctx)
	h := &EventHandler{
		bus:         b,
		handlerFunc: handlerFunc,
		ctx:         ctx,
		cancel:      cancel,
	}

	ch := make(chan events.Event)
	var err error
	if kind == "" {
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
			h.handlerFunc(h.bus, evt)
		}
	}()

	go func() {
		<-h.ctx.Done()
		h.bus.Unsubscribe(context.Background(), h.subscriberID)
	}()

	return h, nil
}

// Close stops the internal goroutine, unsubscribes from the event bus, and waits for the goroutine to exit.
func (h *EventHandler) Close() {
	h.cancel()
	h.wg.Wait()
}
