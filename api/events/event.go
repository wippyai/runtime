package events

import (
	"context"
	"github.com/ponyruntime/pony/api/payload"
	"strings"
)

type (
	SubscriberID string
	System       string
	Path         string
	Kind         string

	Event struct {
		System  System
		Path    Path
		Kind    Kind
		Payload payload.Payload
	}

	Bus interface {
		Subscribe(context.Context, System, chan<- Event) (SubscriberID, error)
		SubscribeP(context.Context, System, Path, chan<- Event) (SubscriberID, error)
		Unsubscribe(context.Context, SubscriberID)
		Send(context.Context, Event)
	}

	Consumer interface {
		Register(context.Context, Event) error
	}
)

func NewPath(path ...string) Path {
	return Path(strings.Join(path, "."))
}
