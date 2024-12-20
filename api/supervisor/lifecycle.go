package supervisor

import (
	"context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
)

const (
	// System is the event system for the supervisor component.
	System events.System = "supervisor"
	// Start is the event kind for starting an entity.
	Start events.Kind = "supervisor.component.start"
	// Stop is the event kind for stopping an entity.
	Stop events.Kind = "supervisor.component.stop"

	// UpdateStatus is the event kind for updating the status of an entity. Typically sent by component.
	UpdateStatus events.Kind = "supervisor.component.status"
)

type (
	Status string

	RetryPolicy struct {
		Delay       string `json:"delay" yaml:"delay"`
		MaxAttempts int    `json:"max_attempts" yaml:"max_attempts"`
	}

	Lifecycle struct {
		AutoStart bool        `json:"auto_start" yaml:"auto_start"`
		Restart   RetryPolicy `json:"restart" yaml:"restart"`
	}

	OperationalStatus struct {
		Status  Status
		Message string
	}

	Entry struct {
		ID        registry.ID
		Lifecycle Lifecycle
		Status    OperationalStatus
	}

	Supervisor interface {
		// Start initiates the start process for a managed entity.
		Start(ctx context.Context, id registry.ID) error
		// Stop initiates the stop process for a managed entity.
		Stop(ctx context.Context, id registry.ID) error
		// Restart initiates the restart process for a managed entity.
		Restart(ctx context.Context, id registry.ID) error
		// List returns the current operational status of all managed entities.
		List() ([]Entry, error)
	}
)
