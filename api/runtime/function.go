package runtime

import (
	"context"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
)

// Event system and kind constants for the executor package
const (
	// FunctionSystem identifies the executor system in the event bus
	FunctionSystem events.System = "functions"

	// RegisterFunction is the event kind for registering a new handler
	RegisterFunction events.Kind = "functions.register"

	// DeleteFunction is the event kind for removing an existing handler
	DeleteFunction events.Kind = "functions.remove"

	// AcceptFunctionEvent is the event kind for accepting a new handler
	AcceptFunctionEvent events.Kind = "functions.accept"

	// RejectFunctionEvent is the event kind for rejecting a new handler
	RejectFunctionEvent events.Kind = "functions.reject"
)

type (
	// Task represents a unit of work to be executed by the executor.
	// It contains the execution context, target identifier, and input payloads.
	Task struct {
		Context  context.Context
		Target   registry.ID
		Payloads payload.Payloads
	}

	// Result represents the outcome of an executed task.
	// It contains either a successful payload or an error.
	Result struct {
		Payload payload.Payload
		Error   error
	}

	// Function is a function type that processes a Task and returns
	// a channel for streaming result(s) and any immediate error that occurs
	// during task initialization.
	Function func(Task) (chan *Result, error)

	// FunctionRegistry is the interface for executing tasks using functions.
	// It provides the core functionality for running tasks and obtaining their results.
	FunctionRegistry interface {
		// Execute processes the given task and returns a channel for getting the result(s)
		// and any immediate error that occurs during task initialization.
		Execute(Task) (chan *Result, error)
	}
)
