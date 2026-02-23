// SPDX-License-Identifier: MPL-2.0

// Package supervisor provides service lifecycle management and supervision.
package supervisor

import (
	"context"

	"github.com/wippyai/runtime/api/event"
)

// System identifies the supervisor system in the event bus.
const System event.System = "supervisor"

// Event kinds for service operations.
const (
	ServiceRegister event.Kind = "service.register"
	ServiceRemove   event.Kind = "service.remove"
	ServiceUpdate   event.Kind = "service.update"
	ServiceStart    event.Kind = "service.start"
	ServiceStop     event.Kind = "service.stop"
)

// Status constants for service states.
const (
	StatusUnknown  Status = "unknown"
	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
	StatusStopping Status = "stopping"
	StatusStopped  Status = "stopped"
	StatusExited   Status = "exited"
	StatusFailed   Status = "failed"
)

type (
	// Entry payload for supervisor registration event. Service will be identified by event path.
	Entry struct {
		Service Service
		Config  LifecycleConfig
	}

	// Status represents the operational status of a service.
	Status = string

	// Service defines the interface that must be implemented by any service managed by the supervisor.
	Service interface {
		// Start initiates the service. Service can post current status to the returned channel.
		// The context passed into start method is primary service context, service must exit if context is done.
		// The status channel needs to stay open while the service is running and only be closed when it's fully stopped or failed.
		Start(ctx context.Context) (<-chan any, error)
		// Stop terminates the service. The context passed into stop method is only for graceful stop, service must return error
		// if it cannot stop within the context deadline.
		Stop(ctx context.Context) error
	}
)
