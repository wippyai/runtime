package events

import (
	"context"
)

type (
	Bus interface {
		Subscribe(context.Context, System, chan<- Event) (SubscriberID, error)
		SubscribeP(context.Context, System, Kind, chan<- Event) (SubscriberID, error)
		Unsubscribe(context.Context, SubscriberID)
		Send(context.Context, Event)
	}

	Consumer interface {
		Register(context.Context, Event)
	}
)
