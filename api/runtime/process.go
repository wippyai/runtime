package runtime

import (
	"context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
)

// Event system and kind constants for the workflow package
const (
	// ProcessSystem identifies the workflow system in the event bus.
	// This system handles registration and management of workflow handlers.
	ProcessSystem events.System = "processes"

	// RegisterProcessPrototype is the event kind for registering a new workflow handler.
	// This event is used to dynamically add new workflow implementations at runtime.
	RegisterProcessPrototype events.Kind = "workflow.set_handler"

	// DeleteProcessPrototype is the event kind for removing an existing workflow handler.
	// This event allows for dynamic removal of workflow implementations.
	DeleteProcessPrototype events.Kind = "workflow.remove_handler"

	// AcceptProcessPrototype is the event kind for accepting a new workflow handler.
	AcceptProcessPrototype events.Kind = "workflow.accept_handler"

	// RejectProcessPrototype is the event kind for rejecting a new workflow handler.
	RejectProcessPrototype events.Kind = "workflow.reject_handler"
)

type (
	Process interface {
		Start(context.Context, payload.Payloads) (chan payload.Payloads, error)
		GetLayer(any) any
		Step() error
		Stop() error
	}

	// todo: flavors

	// todo: kill
	// RegisterWorkflow represents a request to register a new workflow handler
	// for a specific target ID. The handler can be any type, allowing for
	// flexible workflow implementations that can be type-checked at higher levels.
	RegisterWorkflow struct {
		Target  registry.ID
		Handler func() any
		Flavor  string
	}

	// todo: kill
	// DeleteWorkflow represents a request to remove a workflow handler
	// for a specific target ID. This enables dynamic workflow management
	// by allowing handlers to be removed at runtime.
	DeleteWorkflow struct {
		Target registry.ID
	}

	// ProcessRegistry is the interface for managing workflow handlers.
	// It provides the core functionality for retrieving registered workflow
	// implementations. The interface uses 'any' return type to allow for
	// flexible workflow types that can be properly type-asserted by callers.
	ProcessRegistry interface {
		// Get retrieves a registered workflow handler for the given ID.
		// Returns the handler as type any for flexible workflow implementations,
		// and an error if no handler is found or if retrieval fails.
		Get(id registry.ID) (func() any, error)

		Make(id registry.ID, flavor string) (Process, error)
	}
)
