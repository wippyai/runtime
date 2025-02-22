package process

import (
	"context"
	"errors"
	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/topology"
	"time"
)

// Event system and kind constants for the workflow package
const (
	// PrototypeSystem identifies the workflow system in the event bus.
	PrototypeSystem events.System = "prototype"

	// ProtoRegister is the event kind for registering a new process prototype.
	ProtoRegister events.Kind = "prototype.register"

	// ProtoDelete is the event kind for removing an existing process prototype.
	ProtoDelete events.Kind = "prototype.remove"

	// ProtoAccept is the event kind for accepting a new process prototype.
	ProtoAccept events.Kind = "prototype.accept"

	// ProtoReject is the event kind for rejecting a new process prototype.
	ProtoReject events.Kind = "prototype.reject"

	HostSystem   events.System = "hosts"
	HostRegister events.Kind   = "hosts.register"
	HostDelete   events.Kind   = "hosts.remove"
	HostAccept   events.Kind   = "hosts.accept"
	HostReject   events.Kind   = "hosts.reject"

	TopicEvents = topology.TopicEvents
)

var (
	// ErrNoProcess indicates that no process is currently running
	ErrNoProcess    = errors.New("no process running")
	ErrHostBusy     = errors.New("process host is busy")
	ErrMaxProcesses = errors.New("maximum number of processes reached")
	ErrHostDead     = errors.New("process host is dead")
	ErrTerminated   = errors.New("process terminated")
)

type (
	// Prototype is a function that creates a new process instance.
	Prototype func() (Process, error)

	// Factory manages process prototypes and handles process creation
	Factory interface {
		Create(registry.ID) (Process, error)
	}

	// Process defines the interface for a runnable process in the system
	Process interface {
		pubsub.Receiver

		Start(context.Context, pubsub.PID, payload.Payloads) error

		// Step advances process state by one iteration
		Step() (bool, error)
	}

	// StartProcess contains the configuration needed to start a new process
	StartProcess struct {
		HostID   pubsub.HostID
		ID       registry.ID
		UniqID   string
		Payloads payload.Payloads
	}

	// Manager defines the interface for process lifecycle management
	Manager interface {
		Start(ctx context.Context, start *StartProcess) (pubsub.PID, error)
		StartMonitored(context.Context, pubsub.PID, *StartProcess) (pubsub.PID, error)
		Terminate(ctx context.Context, pid pubsub.PID) error
		Cancel(ctx context.Context, from, pid pubsub.PID, deadline time.Time) error
		Topology() Topology
	}

	// Host defines the interface for process execution environments
	Host interface {
		pubsub.Receiver
		Terminate(ctx context.Context, pid pubsub.PID) error
	}

	// LaunchProcess contains the information needed to launch a process
	LaunchProcess struct {
		PID     pubsub.PID
		Process Process
		Input   payload.Payloads
	}

	// Managed defines the interface for managed process hosts
	Managed interface {
		Host
		Launch(ctx context.Context, launch *LaunchProcess) (pubsub.PID, error)
	}

	// Delegated defines the interface for delegated process hosts
	Delegated interface {
		Host
		Launch(ctx context.Context, pid pubsub.PID, input payload.Payloads) (pubsub.PID, error)
	}

	Topology interface {
		Monitor() topology.Monitor
		AttachToContext(ctx context.Context) context.Context
	}
)

// GetProcessManager retrieves the process Manager from the context
func GetProcessManager(ctx context.Context) Manager {
	return ctx.Value(contextapi.ProcessesCtx).(Manager)
}

func GetTopology(ctx context.Context) Topology {
	m, ok := ctx.Value(contextapi.ProcessesCtx).(Manager)
	if !ok {
		panic("process manager not found in context")
	}

	if m.Topology() == nil {
		panic("process manager topology is nil")
	}

	return m.Topology()
}
