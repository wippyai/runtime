package runtime

import (
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
)

type (
	// Task represents a unit of work to be executed by the executor.
	// It contains the execution context, target identifier, and input payloads.
	Task struct {
		Handler  registry.ID      `json:"handler"`
		Payloads payload.Payloads `json:"payloads"`
	}

	// Result represents the outcome of an executed task.
	// It contains either a successful payload or an error.
	Result struct {
		Payload payload.Payload `json:"payload"`
		Error   error           `json:"error"`
	}
)
