package api

import (
	"context"
)

type EventBus interface {
	SubscribeAll(ctx context.Context, subID string, ch chan<- Event) error
	SubscribeP(ctx context.Context, subID string, subSystem SubSystem, etype EventType, ch chan<- Event) error
	Unsubscribe(ctx context.Context, subID string)
	UnsubscribeP(ctx context.Context, subID string, subSystem SubSystem, etype EventType)
	Len() uint
	Send(ctx context.Context, ev Event)
}

type Event interface {
	Type() EventType
	SubSystem() SubSystem
	Content() any
}

type SubSystem string

const (
	// SubSystemAll is a wildcard for all subsystems
	SubSystemAll SubSystem = "*"
	// SubSystemConfiguration subsystem for configuration
	SubSystemConfiguration SubSystem = "configuration"
	// SubSystemRuntime subsystem for modules (sql, wasm, etc)
	SubSystemRuntime SubSystem = "runtime"
	// SubSystemRegistry is a routing subsystem
	SubSystemRegistry SubSystem = "registry"
	// SubSystemEndpoints subsystem is an ingress subsystem
	SubSystemEndpoints SubSystem = "endpoints"
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
