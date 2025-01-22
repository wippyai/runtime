package supervisor

import (
	"context"
	"errors"
	"github.com/ponyruntime/pony/api/events"
)

// Supervisor system constants define the event types and identifiers used by the supervisor.
const (
	// System identifies the supervisor system in the event context
	System events.System = "supervisor"
	// Register represents an event for registering a new service
	Register events.Kind = "supervisor.service.register"
	// Remove represents an event for removing a service
	Remove events.Kind = "supervisor.service.remove"
	// Update represents an event for updating service status
	Update events.Kind = "supervisor.service.status"

	// Service lifecycle event constants define the different lifecycle states

	// Start represents an event to start a service
	Start events.Kind = "supervisor.service.start"
	// Stop represents an event to stop a service
	Stop events.Kind = "supervisor.service.stop"

	// Service status constants define the possible operational states of a service

	// Unknown indicates the service status is currently unknown
	Unknown Status = "unknown"
	// Starting indicates the service is currently starting up
	Starting Status = "starting"
	// Running indicates the service is currently running and operational
	Running Status = "running"
	// Stopping indicates the service is in the process of a graceful shutdown
	Stopping Status = "stopping"
	// Stopped indicates the service has stopped and is no longer running
	Stopped Status = "stopped"
	// Failed indicates the service has failed and is not running
	Failed Status = "failed"
)

var (
	// Terminated error is returned when a service is terminated, disables supervision.
	Terminated = errors.New("service terminated")
	// Exited error is returned when a service exits on its own, disables supervision.
	Exited = errors.New("service exited")
)

type (
	// Entry payload for supervisor registration event. Service will be identified by event path.
	Entry struct {
		Service Service
		Config  LifecycleConfig
	}

	// Status represents the operational status of a service.
	Status string

	// Service defines the interface that must be implemented by any service managed by the supervisor.
	Service interface {
		// Start initiates the service. Service can post current status to the returned channel.
		// The context passed into start method is primary service context, service must exit if context is done.
		Start(ctx context.Context) (<-chan any, error)
		// Stop terminates the service. The context passed into stop method is only for graceful stop, service must return error
		// if it cannot stop within the context deadline.
		Stop(ctx context.Context) error
	}
)
