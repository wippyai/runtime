package operation

import (
	"context"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
)

// System constants for the operation package.
const (
	// System identifies the operation system on the event bus.
	System events.System = "operation"

	// OpRegister is sent to operation nodes to request registration.
	OpRegister events.Kind = "operation.register"

	// OpDelete is sent to operation nodes to request their removal.
	OpDelete events.Kind = "operation.delete"

	// OpAccept is sent from operation nodes to indicate that registration has been accepted.
	OpAccept events.Kind = "operation.accept"

	// OpReject is sent from operation nodes to indicate that registration has been rejected.
	OpReject events.Kind = "operation.reject"
)

type (
	// ProgressKind represents a label for the current stage or state of an operation.
	ProgressKind = string

	// Progress provides a simple status update for an ongoing operation.
	// It is designed to communicate the current phase and any supplemental data that
	// may be useful for monitoring the operation’s progress.
	Progress struct {
		// Type describes the current stage or step of the operation.
		Type ProgressKind `json:"kind"`

		// Data optionally carries additional details relevant to the operation’s current state.
		Data payload.Payload `json:"data,omitempty"`
	}

	// Operation defines a one-off administrative task, such as running migrations, tests,
	// or other maintenance procedures. These tasks are executed on-demand rather than continuously.
	Operation interface {
		// Meta returns metadata describing the operation (e.g., its name, version, and other attributes).
		Meta() registry.Metadata

		// Execute performs the operation with the provided input.
		// It accepts a context for cancellation and deadline management, a set of input payloads,
		// and an optional progress channel to stream real-time status updates.
		// The method returns output payloads or an error if the execution fails.
		Execute(ctx context.Context, input payload.Payloads, progress chan<- Progress) (payload.Payloads, error)
	}

	// Registry defines the interface for managing and retrieving registered operations.
	Registry interface {
		// Find searches for operations matching the provided metadata.
		// It returns a slice of matching operations and an error if the search fails.
		Find(metadata registry.Metadata) ([]Operation, error)
	}
)
