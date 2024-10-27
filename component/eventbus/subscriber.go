package eventsbus

import (
	"context"
	"github.com/ponyruntime/pony/api"
	"time"
)

type Subscriber struct {
	bus *Bus
	id  string
	ch  <-chan api.Event
}

func NewSubscriber() *Subscriber {
	ch := make(chan api.Event, 10)

	bus, id := GlobalEventBus()
	bus.SubscribeAll(context.Background(), id, ch)

	return &Subscriber{
		bus: bus,
		id:  id,
		ch:  ch,
	}
}

func (s *Subscriber) Close() {
	s.bus.Unsubscribe(context.Background(), s.id)
}

func (s *Subscriber) Wait(sub api.Component, et api.EventType) api.Event {
	tout := time.After(10 * time.Second)
	for {
		select {
		case ev := <-s.ch:
			if ev.Target() == sub && ev.Kind() == et {
				return ev
			}
		case <-tout:
			return nil
		}
	}
}
