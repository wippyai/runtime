package envstorage

import (
	"github.com/ponyruntime/pony/api/event"
)

// Event bus constants for environment storage operations
const (
	// System represents the environment storage event bus system identifier
	System event.System = "envstorage"

	// Register is a command event to register a new environment variable
	Register event.Kind = "envstorage.register"
	// Delete is a command event to remove an environment variable
	Delete event.Kind = "envstorage.delete"

	// Accept is emitted when an environment variable command is successfully processed
	Accept event.Kind = "envstorage.accept"
	// Reject is emitted when an environment variable command fails
	Reject event.Kind = "envstorage.reject"
)
