package executor

import (
	"context"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
)

// Event system and kind constants for the executor package
const (
	// System identifies the executor system in the event bus
	System events.System = "executor"

	// RegisterHandlerEvent is the event kind for registering a new handler
	RegisterHandlerEvent events.Kind = "executor.set_handler"

	// DeleteHandlerEvent is the event kind for removing an existing handler
	DeleteHandlerEvent events.Kind = "executor.remove_handler"
)

type (
	// RegisterHandler represents a request to register a new executor handler
	// for a specific target ID.
	RegisterHandler struct {
		Target  registry.ID
		Handler ExecutorHandler
	}

	// DeleteHandler represents a request to remove an executor handler
	// for a specific target ID.
	DeleteHandler struct {
		Target registry.ID
	}

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

	// ExecutorHandler is a function type that processes a Task and returns
	// a channel for streaming result(s) and any immediate error that occurs
	// during task initialization.
	ExecutorHandler func(Task) (chan *Result, error)

	// Executor is the interface for executing tasks.
	// It provides the core functionality for running tasks and obtaining their results.
	Executor interface {
		// Execute processes the given task and returns a channel for getting the result(s)
		// and any immediate error that occurs during task initialization.
		Execute(Task) (chan *Result, error)
	}
)
