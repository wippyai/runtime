// SPDX-License-Identifier: MPL-2.0

// Package event provides an event bus implementation for distributing events across the system.
package event

import (
	"context"
)

type (
	// SubscriberID is a unique identifier for a subscriber.
	SubscriberID = string

	// System is a system or module that events belong to.
	System = string

	// Kind is the specific type of an event within a system.
	Kind = string

	// Path contains unique alias of related entity or system.
	Path = string

	// Event is the fundamental structure representing an event.
	Event struct {
		Data   any
		System System
		Kind   Kind
		Path   Path
	}

	// Bus defines the functionality of an event bus.
	Bus interface {
		// Subscribe subscribes a channel to events from a specific system.
		Subscribe(context.Context, System, chan<- Event) (SubscriberID, error)

		// SubscribeP subscribes a channel to events matching a specific system and kind.
		SubscribeP(context.Context, System, Kind, chan<- Event) (SubscriberID, error)

		// Unsubscribe removes a subscription using its SubscriberID.
		Unsubscribe(context.Context, SubscriberID)

		// Send publishes an event to the bus.
		Send(context.Context, Event)
	}
)
