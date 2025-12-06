package option_c

import "context"

// Event represents any event that can flow through the event channel.
type Event interface {
	isEvent()
}

// YieldResult represents the result of a yield operation.
type YieldResult struct {
	Tag   uint64
	Value any
	Err   error
}

func (YieldResult) isEvent() {}

// Message represents an incoming message for the process.
type Message struct {
	Value any
}

func (Message) isEvent() {}

// Yield represents a yield request from the process.
type Yield struct {
	Tag       uint64
	Operation any
}

func (Yield) isEvent() {}

// EventChan is a channel for events.
type EventChan chan Event

// StepResult represents the result of a Step call.
type StepResult struct {
	Done   bool
	Output any
	Err    error
}

// InitResult represents the result of process initialization.
type InitResult struct {
	EventChan EventChan
	Err       error
}

// MethodCall contains the initial method and input.
type MethodCall struct {
	Ctx    context.Context
	Method string
	Input  any
}
