package operation

import (
	"context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
)

// System constants for the operation package
const (
	// System identifies the operation system in the event bus
	System events.System = "operation"

	// OpRegister is sent TO operation nodes to request registration
	OpRegister events.Kind = "operation.register"

	// OpDelete is sent TO operation nodes to request removal
	OpDelete events.Kind = "operation.delete"

	// OpAccept is sent FROM operation nodes when registration is accepted
	OpAccept events.Kind = "operation.accept"

	// OpReject is sent FROM operation nodes when registration is rejected
	OpReject events.Kind = "operation.reject"
)

type (
	ProgressKind = string

	// Progress is a simple update about operation status
	Progress struct {
		// Type describes the current state or step, is many cases it will be operation specific.
		Type ProgressKind `json:"kind"`

		// Data is optional payload with additional information, specific to operation type.
		Data payload.Payload `json:"data,omitempty"`
	}

	// Operation represents a task that can be executed on demand
	// It's more focused on one-time tasks like migrations, tests,
	// or administrative procedures, unlike regular functions
	// which are meant for frequent, standard API calls.
	Operation interface {
		// Execute runs the operation with the given input
		// The progress channel allows reporting status updates during execution
		// This channel may be nil if progress reporting is not needed
		Execute(ctx context.Context, input payload.Payloads, progress chan<- Progress) (payload.Payload, error)
	}
)

// NewProgress creates a new progress update with the given message and optional data
func NewProgress(kind ProgressKind, data payload.Payload) Progress {
	return Progress{
		Type: kind,
		Data: data,
	}
}
