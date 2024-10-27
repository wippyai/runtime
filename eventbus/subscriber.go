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

	bus.SubscribeP(
		context.Background(),
		id,
		ch,
	)

	return &Subscriber{
		bus: bus,
		id:  id,
		ch:  ch,
	}
}

func (s *Subscriber) Wait(sub api.Subsystem, et api.EventType) api.Event {
	tout := time.After(10 * time.Second)
	for {
		select {
		case ev := <-s.ch:
			if ev.Subsystem() == sub && ev.Type() == et {
				return ev
			}
		case <-tout:
			return nil
		}
	}
}
