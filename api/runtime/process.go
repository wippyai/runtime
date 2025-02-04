package runtime

import (
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
)

// Event system and kind constants for the workflow package
const (
	// WorkflowSystem identifies the workflow system in the event bus.
	// This system handles registration and management of workflow handlers.
	WorkflowSystem events.System = "workflow"

	// RegisterWorkflowEvent is the event kind for registering a new workflow handler.
	// This event is used to dynamically add new workflow implementations at runtime.
	RegisterWorkflowEvent events.Kind = "workflow.set_handler"

	// DeleteWorkflowEvent is the event kind for removing an existing workflow handler.
	// This event allows for dynamic removal of workflow implementations.
	DeleteWorkflowEvent events.Kind = "workflow.remove_handler"
)

type (
	// RegisterWorkflow represents a request to register a new workflow handler
	// for a specific target ID. The handler can be any type, allowing for
	// flexible workflow implementations that can be type-checked at higher levels.
	RegisterWorkflow struct {
		Target  registry.ID
		Handler func() any
	}

	// DeleteWorkflow represents a request to remove a workflow handler
	// for a specific target ID. This enables dynamic workflow management
	// by allowing handlers to be removed at runtime.
	DeleteWorkflow struct {
		Target registry.ID
	}

	// WorkflowRegistry is the interface for managing workflow handlers.
	// It provides the core functionality for retrieving registered workflow
	// implementations. The interface uses 'any' return type to allow for
	// flexible workflow types that can be properly type-asserted by callers.
	WorkflowRegistry interface {
		// Get retrieves a registered workflow handler for the given ID.
		// Returns the handler as type any for flexible workflow implementations,
		// and an error if no handler is found or if retrieval fails.
		Get(id registry.ID) (func() any, error)
	}
)
