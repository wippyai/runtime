package runtime

import (
	"context"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
)

type (
	Target struct {
		// Namespace is the namespace of the target.
		NS registry.Namespace `json:"namespace"`
		// ID is the unique identifier of the target.
		ID registry.ID `json:"id"`
		// Name is the human-readable name of the target. Optional.
		Name string `json:"name,omitempty"`
	}

	// Task represents a unit of work to be executed by the executor.
	// It contains the execution context, target identifier, and input payloads.
	Task struct {
		Context  context.Context
		Target   Target
		Payloads payload.Payloads
	}

	// Result represents the outcome of an executed task.
	// It contains either a successful payload or an error.
	Result struct {
		Payload payload.Payload
		Error   error
	}
)
