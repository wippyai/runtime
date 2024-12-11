package api

import (
	"context"

	"github.com/ponyruntime/pony/api/payload"
)

type Event interface {
	System() System
	Kind() EventType
	Payload() payload.Payload
}

type EventType string

/** Only core wide events are here. */
const (
	// EventsAll is a wildcard for all events
	EventsAll EventType = "*"

	// EventConfigurationUpdated thrown when a chart updated.
	EventConfigurationUpdated EventType = "EventConfigurationUpdated"
	// EventFatalError thrown when a subsystem got a fatal error and the core should be shut down.
	EventFatalError EventType = "EventFatalError"
	// EventStop thrown when a subsystem(s) should be stopped.
	EventStop EventType = "EventStop"

	EventRegisterChange EventType = "EventRegisterChange"
	EventRegisterCommit EventType = "ConfirmCommit"
	EventApplyError     EventType = "EventApplyError"

	EventBegin    EventType = "eventBegin"
	EventApply    EventType = "eventCommit"
	ChangeDiscard EventType = "eventRollback"
)

type EventBus interface {
	SubscribeAll(ctx context.Context, subID string, ch chan<- Event) error
	SubscribeP(ctx context.Context, subID string, c System, et EventType, ch chan<- Event) error
	Unsubscribe(ctx context.Context, subID string)
	UnsubscribeP(ctx context.Context, subID string, c System, et EventType)
	Len() uint
	Send(ctx context.Context, e Event)
}

type System string

const (
	All           System = "*"
	Execution     System = "execution"
	Configuration System = "chart"
	Listeners     System = "listeners"

	// ChangeGroup subsystem for chart
	ChangeGroup System = "transaction"
	// Servers subsystem for modules (sql, wasm, etc)
	Servers System = "http"
	// SubSystemRegistry is a routing subsystem
	SubSystemRegistry System = "registry"
	// SubSystemEndpoints subsystem is an ingress subsystem
	SubSystemEndpoints System = "http"
)
