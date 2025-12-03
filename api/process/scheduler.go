package process

import (
	"context"
	"time"

	"github.com/wippyai/runtime/api/relay"
)

// SchedulerKind identifies the scheduler implementation.
const (
	KindGlobal   SchedulerKind = "global"
	KindStealing SchedulerKind = "stealing"
)

// Lifecycle option keys for process supervision.
const (
	LifecycleParentKey  = "lifecycle.parent"
	LifecycleMonitorKey = "lifecycle.monitor"
	LifecycleLinkKey    = "lifecycle.link"
	OptionPID           = "pid"
)

// SchedulerKind identifies the scheduler implementation.
type SchedulerKind string

type (
	// LifecycleRegistry manages multiple lifecycle handlers that are called
	// by schedulers during process lifecycle events.
	LifecycleRegistry interface {
		Lifecycle
		// Register adds a lifecycle handler with the given name.
		Register(name string, lc Lifecycle)
		// Unregister removes a lifecycle handler by name.
		Unregister(name string)
	}

	// Host is a unified interface for process execution environments.
	Host interface {
		relay.Receiver
		Run(ctx context.Context, start *Start) (relay.PID, error)
		Terminate(ctx context.Context, pid relay.PID) error
	}

	// Canceller defines the interface for gracefully canceling a running process.
	Canceller interface {
		Cancel(ctx context.Context, from, pid relay.PID, deadline time.Time) error
	}

	// Manager defines the interface for process lifecycle management.
	Manager interface {
		Canceller
		Start(ctx context.Context, start *Start) (relay.PID, error)
		Terminate(ctx context.Context, pid relay.PID) error
	}
)
