package api

import (
	"context"
	"github.com/ponyruntime/pony/api/payload"
)

type Event interface {
	Component() Component
	Kind() EventType
	Payload() payload.Payload
}

type EventBus interface {
	SubscribeAll(ctx context.Context, subID string, ch chan<- Event) error
	SubscribeP(ctx context.Context, subID string, subSystem Component, etype EventType, ch chan<- Event) error
	Unsubscribe(ctx context.Context, subID string)
	UnsubscribeP(ctx context.Context, subID string, subSystem Component, etype EventType)
	Len() uint
	Send(ctx context.Context, ev Event)
}

type Component string

const (

	// todo: deprecate here

	// SubSystemAll is a wildcard for all subsystems
	SubSystemAll Component = "*"
	// Transaction subsystem for config
	Transaction Component = "transaction"
	// Servers subsystem for modules (sql, wasm, etc)
	Servers Component = "server"
	// SubSystemRegistry is a routing subsystem
	SubSystemRegistry Component = "registry"
	// SubSystemEndpoints subsystem is an ingress subsystem
	SubSystemEndpoints Component = "server"
)

type EventType string

const (
	// EventsAll is a wildcard for all events
	EventsAll EventType = "*"

	// EventConfigurationUpdated thrown when a config updated.
	EventConfigurationUpdated EventType = "EventConfigurationUpdated"
	// EventFatalError thrown when a subsystem got a fatal error and the system should be shut down.
	EventFatalError EventType = "EventFatalError"
	// EventStop thrown when a subsystem(s) should be stopped.
	EventStop EventType = "EventStop"

	EventRegisterChange EventType = "EventRegisterChange"
	EventRegisterCommit EventType = "ConfirmCommit"

	EventBegin    EventType = "eventBegin"
	EventCommit   EventType = "eventCommit"
	EventRollback EventType = "eventRollback"
)
