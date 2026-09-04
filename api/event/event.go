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
		// The caller must not close the channel until Unsubscribe returns;
		// that return is the barrier proving the bus can no longer send to it.
		// Once the request is accepted for dispatch, Subscribe waits for the
		// ownership decision even if the context is canceled. On error, the bus
		// does not retain the channel.
		Subscribe(context.Context, System, chan<- Event) (SubscriberID, error)

		// SubscribeP has the same channel ownership and Unsubscribe barrier as Subscribe.
		SubscribeP(context.Context, System, Kind, chan<- Event) (SubscriberID, error)

		// Unsubscribe removes a subscription using its SubscriberID. It is a
		// non-cancelable safety barrier: when it returns, no in-flight or future
		// bus operation can send to that subscription.
		Unsubscribe(context.Context, SubscriberID)

		// Send publishes an event to the bus. Delivery remains ordered and
		// lossless while the publisher and subscriber contexts stay active;
		// cancellation may abort queued or in-progress delivery.
		Send(context.Context, Event)
	}
)
