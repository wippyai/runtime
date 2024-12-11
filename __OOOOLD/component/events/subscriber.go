package eventsbus

import (
	"context"
	"github.com/ponyruntime/pony/api/events"
	"log"
	"time"
)

type Subscriber struct {
	bus *Bus
	id  string
	ch  <-chan events.Event
}

func NewSubscriber() *Subscriber {
	ch := make(chan events.Event, 10)

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

func (s *Subscriber) Wait(sub events.System, et events.Kind) events.Event {
	tout := time.After(10 * time.Second)
	for {
		select {
		case ev := <-s.ch:
			if ev.System() == sub && ev.Kind() == et {
				return ev
			}
		case <-tout:
			log.Println("timeout on", sub, et)
			return nil
		}
	}
}
