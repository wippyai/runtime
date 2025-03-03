package process

import (
	"context"
	"errors"
	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"time"
)

// Event system and kind constants for the process package
const (
	// PrototypeSystem identifies the prototype registration system in the event bus.
	PrototypeSystem event.System = "prototype"

	// ProtoRegister is the event kind for registering a new process prototype.
	ProtoRegister event.Kind = "prototype.register"

	// ProtoDelete is the event kind for removing an existing process prototype.
	ProtoDelete event.Kind = "prototype.delete"

	// ProtoAccept is the event kind for accepting a new process prototype.
	ProtoAccept event.Kind = "prototype.accept"

	// ProtoReject is the event kind for rejecting a new process prototype.
	ProtoReject event.Kind = "prototype.reject"

	// HostSystem identifies the host registration system in the event bus.
	HostSystem event.System = "hosts"

	// HostRegister is the event kind for registering a new process host.
	HostRegister event.Kind = "hosts.register"

	// HostDelete is the event kind for removing an existing process host.
	HostDelete event.Kind = "hosts.delete"

	// HostAccept is the event kind for accepting a new process host.
	HostAccept event.Kind = "hosts.accept"

	// HostReject is the event kind for rejecting a new process host.
	HostReject event.Kind = "hosts.reject"
)

var (
	// ErrNoProcess indicates that no process is currently running.
	ErrNoProcess = errors.New("no process running")

	// ErrHostBusy indicates that the process host is already running at capacity.
	ErrHostBusy = errors.New("process host is busy")

	// ErrMaxProcesses indicates that the maximum number of processes has been reached.
	ErrMaxProcesses = errors.New("maximum number of processes reached")

	// ErrHostDead indicates that the process host is no longer available.
	ErrHostDead = errors.New("process host is dead")
)

type (
	// Prototype is a function that creates a new process instance.
	// It returns a Process and an error if the process creation fails.
	// Prototypes are registered in the system and used to instantiate processes on demand.
	Prototype func() (Process, error)

	// Factory manages process prototypes and handles process creation.
	// It provides a way to create process instances from their registry IDs.
	Factory interface {
		// Create instantiates a new process from the provided registry ID.
		// Returns an error if no prototype is registered for the ID or if process creation fails.
		Create(registry.ID) (Process, error)
	}

	// Process defines the interface for a runnable process in the system.
	// Processes can receive messages, start execution, and advance their state step by step.
	Process interface {
		pubsub.Receiver

		// Start initializes the process with the given context, PID, and input payloads.
		// It prepares the process for execution and returns an error if initialization fails.
		Start(context.Context, pubsub.PID, payload.Payloads) error

		// Step advances process state by one iteration.
		Step() error

		// Ready returns the size of the runner's queue that is ready to be processed.
		// Higher values typically indicate that process is lagging behind and needs more
		// resources.
		Ready() int
	}

	// Lifecycle encapsulates the supervision relationship between processes.
	// It defines how processes monitor and link to each other, affecting error
	// propagation and termination behavior. @see topology
	Lifecycle struct {
		// Parent is the PID of the parent process, used for monitoring and linking
		Parent pubsub.PID

		// Monitor indicates whether the parent process should monitor this process.
		// When monitoring is enabled, the parent receives notifications about the child's
		// termination but continues to run.
		Monitor bool

		// Link indicates whether the parent process should be linked to this process.
		// When linking is enabled, if either process terminates with an error, the other
		// process is also terminated. This creates a bi-directional dependency.
		Link bool
	}

	// Start contains the configuration needed to start a new process.
	// It specifies the host, process type, input, and supervision relationships.
	Start struct {
		// HostID is the identifier of the host where the process will run
		HostID pubsub.HostID

		// Source is the registry ID of the process prototype to instantiate
		Source registry.ID

		// UniqID is an optional unique identifier for the process instance.
		// If not provided, one will be generated automatically.
		UniqID string

		// Input contains the initialization data for the process
		Input payload.Payloads

		// Lifecycle defines the supervision relationships for this process
		// including monitoring and linking with the parent process.
		Lifecycle Lifecycle
	}

	// Launch contains the information needed to launch a process.
	// It is used by managed hosts to start a specific process instance and
	// includes both the process and its lifecycle configuration.
	Launch struct {
		// PID is the process identifier to assign to the new process
		PID pubsub.PID

		// Process is the process instance to start
		Process Process

		// Input contains the initialization data for the process
		Input payload.Payloads

		// Lifecycle defines the supervision relationships for this process
		// including monitoring and linking with the parent process.
		Lifecycle Lifecycle
	}

	// Terminator defines the interface for forcefully stopping a running process.
	Terminator interface {
		// Terminate forcefully stops a running process identified by pid.
		// Returns an error if the termination fails.
		Terminate(context.Context, pubsub.PID) error
	}

	// Canceller defines the interface for gracefully cancelling a running process.
	Canceller interface {
		// Cancel sends a cancellation signal to a process identified by pid.
		// from is the PID of the cancellation requester, and deadline specifies
		// when the process will be forcefully terminated if it doesn't stop gracefully.
		// Returns an error if the cancellation request cannot be sent.
		Cancel(context.Context, pubsub.PID, pubsub.PID, time.Time) error
	}

	// Manager defines the interface for process lifecycle management.
	// It combines process starting, termination, and cancellation capabilities,
	// and provides context management for process lifecycle events.
	Manager interface {
		Terminator
		Canceller

		// Start launches a new process according to the provided configuration.
		// Returns the PID of the started process or an error if the process
		// cannot be started.
		Start(ctx context.Context, start *Start) (pubsub.PID, error)

		// AttachLifecycle enhances a context with process lifecycle management.
		// It adds callbacks for process startup and completion events that manage
		// process registration, monitoring, linking, and cleanup in the topology.
		// The provided Lifecycle configuration determines the supervision behavior.
		AttachLifecycle(context.Context, Lifecycle) context.Context
	}

	// Host defines the interface for process execution environments.
	// Hosts are responsible for executing processes and can receive messages
	// and terminate running processes.
	Host interface {
		pubsub.Receiver
		Terminator
	}

	// Managed defines the interface for managed process hosts.
	// Managed hosts receive process instances from the manager and
	// are responsible for executing them locally.
	Managed interface {
		Host

		// Launch starts a process according to the provided launch configuration.
		// It handles both the process execution and its lifecycle management.
		// Returns the PID of the started process or an error if the process
		// cannot be started.
		Launch(ctx context.Context, launch *Launch) (pubsub.PID, error)
	}

	// Delegated defines the interface for delegated process hosts.
	// Delegated hosts receive process identifiers from the manager and
	// are responsible for creating and executing the processes themselves.
	Delegated interface {
		Host

		// Launch starts a process with the given PID and input.
		// Returns the PID of the started process or an error if the process
		// cannot be started.
		Launch(ctx context.Context, pid pubsub.PID, input payload.Payloads) (pubsub.PID, error)
	}
)
