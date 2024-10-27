package api

import (
	"context"
)

type EventBus interface {
	SubscribeAll(ctx context.Context, subID string, ch chan<- Event) error
	SubscribeP(ctx context.Context, subID string, subSystem Subsystem, etype EventType, ch chan<- Event) error
	Unsubscribe(ctx context.Context, subID string)
	UnsubscribeP(ctx context.Context, subID string, subSystem Subsystem, etype EventType)
	Len() uint
	Send(ctx context.Context, ev Event)
}

type Event interface {
	Type() EventType
	Subsystem() Subsystem
	Content() any
}

type Subsystem string

const (

	// todo: deprecate here

	// SubSystemAll is a wildcard for all subsystems
	SubSystemAll Subsystem = "*"
	// Transaction subsystem for configuration
	Transaction Subsystem = "transaction"
	// Servers subsystem for modules (sql, wasm, etc)
	Servers Subsystem = "server"
	// SubSystemRegistry is a routing subsystem
	SubSystemRegistry Subsystem = "registry"
	// SubSystemEndpoints subsystem is an ingress subsystem
	SubSystemEndpoints Subsystem = "server"
)

type EventType string

const (
	// EventsAll is a wildcard for all events
	EventsAll EventType = "*"
	// EventConfigurationUpdated thrown when a configuration updated.
	EventConfigurationUpdated EventType = "EventConfigurationUpdated"
	// EventFatalError thrown when a subsystem got a fatal error and the system should be shut down.
	EventFatalError EventType = "EventFatalError"
	// EventStop thrown when a subsystem(s) should be stopped.
	EventStop EventType = "EventStop"
)
