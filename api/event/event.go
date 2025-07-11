// Package event provides an event bus implementation for distributing events across the system.
package event

import (
	"context"
)

type (
	// SubscriberID represents a unique identifier for a subscriber.
	SubscriberID = string

	// System represents a system or module that eventbus belong to
	System = string

	// Kind represents the specific type of an event within a system
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

	// Bus is an interface defining the functionality of an event bus.
	// It allows subscribers to listen for events and publishers to send events.
	Bus interface {
		// Subscribe subscribes a channel to events from a specific system.
		// Returns a unique subscriber ID and an error if the subscription fails.
		Subscribe(context.Context, System, chan<- Event) (SubscriberID, error)

		// SubscribeP subscribes a channel to events from a specific system and matching a specific pattern.
		// Returns a unique subscriber ID and an error if the subscription fails.
		SubscribeP(context.Context, System, Kind, chan<- Event) (SubscriberID, error)

		// Unsubscribe removes a subscription using its SubscriberID.
		Unsubscribe(context.Context, SubscriberID)

		// Send publishes an event to the bus.
		Send(context.Context, Event)
	}
)
