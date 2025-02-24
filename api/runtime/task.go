package runtime

import (
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
)

type (
	// Task represents a unit of work to be executed by the runtime system.
	Task struct {
		// ID uniquely identifies the function/process/operation to be executed
		ID registry.ID `json:"id"`

		// Payloads contains the input data for the function execution
		Payloads payload.Payloads `json:"payloads"`
	}

	// Result represents the outcome of an executed task.
	// It can contain either a successful payload or an error,
	// allowing for both successful and failed execution handling.
	Result struct {
		// Payload contains the successful execution result data
		Payload payload.Payload `json:"payload"`

		// Error contains any error that occurred during task execution
		Error error `json:"error"`
	}
)
