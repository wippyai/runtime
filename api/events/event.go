package events

import (
	"context"
	ctxapi "github.com/ponyruntime/pony/api/context"
)

type (
	// SubscriberID represents a unique identifier for a subscriber.
	SubscriberID = string

	// System represents a system or module that eventbus belong to (e.g., "listener", "runtime", etc).
	System = string

	// Kind represents the specific type of an event within a system (e.g., "listener.created", "supervisor.online").
	Kind = string

	// Path contains unique Alias of related entity or system.
	Path = string

	// Event is the fundamental structure representing an event.
	Event struct {
		// System is the system or module the event originates from.
		System System
		// Kind is the specific type of the event.
		Kind Kind
		// Path is the path of the event.
		Path Path
		// Data is the payload of the event, which can be any relevant data associated with the event.
		Data any
	}

	// Bus is an interface defining the pkg functionality of an event bus.
	// It allows subscribers to listen for eventbus and publishers to send eventbus.
	Bus interface {
		// Subscribe subscribes a channel to eventbus from a specific system.
		Subscribe(context.Context, System, chan<- Event) (SubscriberID, error)
		// SubscribeP subscribes a channel to eventbus from a specific system and matching a specific pattern.
		SubscribeP(context.Context, System, Kind, chan<- Event) (SubscriberID, error)
		// Unsubscribe unsubscribes a subscriber using its SubscriberID.
		Unsubscribe(context.Context, SubscriberID)
		// Send publishes an event to the bus.
		Send(context.Context, Event)
	}
)

func GetBus(ctx context.Context) Bus {
	return ctx.Value(ctxapi.BusCtx).(Bus)
}
