package api

import (
	"context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
	"time"
)

const (
	System events.System = "supervisor"

	Start events.Kind = "supervisor.component.start"
	Stop  events.Kind = "supervisor.component.stop"

	Query  events.Kind = "supervisor.component.query"
	Report events.Kind = "supervisor.component.status"

	StatusUnknown  Status = "unknown"
	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
	StatusStopping Status = "stopping"
	StatusStopped  Status = "stopped"
	StatusFailed   Status = "failed"
)

type (
	RetryPolicy struct {
		Delay       string `json:"delay" yaml:"delay"`
		MaxAttempts int    `json:"max_attempts" yaml:"max_attempts"`
	}

	Lifecycle struct {
		AutoStart bool        `json:"auto_start" yaml:"auto_start"`
		Restart   RetryPolicy `json:"restart" yaml:"restart"`
	}

	Status string

	OperationalStatus struct {
		Status  Status
		Message string
	}

	Entry struct {
		ID        registry.ID
		Added     time.Time
		Updated   time.Time
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
