package events

import (
	"context"
	"strings"
)

type (
	SubscriberID string
	System       string
	Kind         string

	Event struct {
		System System
		Kind   Kind
		Data   any
	}

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

func NewKind(path ...string) Kind {
	return Kind(strings.Join(path, "."))
}
