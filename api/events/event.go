package events

import (
	"context"
)

type (
	// SubscriberID represents a unique identifier for a subscriber.
	SubscriberID string

	// System represents a system or module that events belong to (e.g., "config", "runtime", etc).
	System string

	// Kind represents the specific type of an event within a system (e.g., "config.created", "status.online").
	Kind string

	// Event is the fundamental structure representing an event.
	Event struct {
		// System is the system or module the event originates from.
		System System
		// Kind is the specific type of the event.
		Kind Kind
		// Data is the payload of the event, which can be any relevant data associated with the event.
		Data any
	}

	// Bus is an interface defining the pkg functionality of an event bus.
	// It allows subscribers to listen for events and publishers to send events.
	Bus interface {
		// Subscribe subscribes a channel to events from a specific system.
		Subscribe(context.Context, System, chan<- Event) (SubscriberID, error)
		// SubscribeP subscribes a channel to events from a specific system and matching a specific pattern.
		SubscribeP(context.Context, System, Kind, chan<- Event) (SubscriberID, error)
		// Unsubscribe unsubscribes a subscriber using its SubscriberID.
		Unsubscribe(context.Context, SubscriberID)
		// Send publishes an event to the bus.
		Send(context.Context, Event)
	}
)
