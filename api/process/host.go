package process

import (
	"context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/runtime"
)

// Event system and kind constants for the workflow package
const (
	HostSystem   events.System = "hosts"
	RegisterHost events.Kind   = "hosts.register"
	DeleteHost   events.Kind   = "hosts.remove"
	AcceptHost   events.Kind   = "hosts.accept"
	RejectHost   events.Kind   = "hosts.reject"
)

type (
	// Host core interface for process control
	Host interface {
		Send(ctx context.Context, pid PID, msg payload.Payloads) error
		Terminate(ctx context.Context, pid PID) error
	}

	// Managed handles local process operations
	Managed interface {
		Host
		Launch(ctx context.Context, pid PID, task runtime.Task, prototype Process) (PID, error)
	}

	// Delegated handles remote process operations
	Delegated interface {
		Host
		Launch(ctx context.Context, pid PID, task runtime.Task) (PID, error)
	}
)
