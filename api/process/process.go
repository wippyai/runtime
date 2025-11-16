// Package process provides process abstraction and lifecycle.
package process

import (
	stdcontext "context"
	"errors"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
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

const (
	// StepContinue indicates the process has more work and should be rescheduled immediately
	StepContinue StepResult = iota
	// StepIdle indicates the process has no work and should wait for new messages
	StepIdle
	// StepDone indicates the process has finished execution
	StepDone
)

// Lifecycle option keys for process supervision
const (
	// LifecycleParentKey is the option key for the parent process PID
	LifecycleParentKey = "lifecycle.parent"
	// LifecycleMonitorKey is the option key for monitor mode (bool)
	LifecycleMonitorKey = "lifecycle.monitor"
	// LifecycleLinkKey is the option key for link mode (bool)
	LifecycleLinkKey = "lifecycle.link"
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

	// ErrTerminated indicates that the process has been terminated.
	ErrTerminated = errors.New("process terminated")
)

type (
	// StepResult indicates the state of a process after a Step() execution
	StepResult int

	// StartMutator is a function that modifies a Start request before process launch.
	// Mutators can add context pairs, options, or lifecycle hooks based on the start configuration.
	// They can also modify and return the context, allowing external context setting.
	// They are executed in registration order by the Manager before creating the Launch request.
	StartMutator func(ctx stdcontext.Context, start *Start) (stdcontext.Context, error)

	// OnStart is a lifecycle hook called after a process starts.
	// It receives the context, process PID, and process instance.
	OnStart func(ctx stdcontext.Context, pid relay.PID, proc Process)

	// OnComplete is a lifecycle hook called when a process completes.
	// It receives the context, process PID, and the result (value or error).
	OnComplete func(ctx stdcontext.Context, pid relay.PID, result *runtime.Result)

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
		relay.Receiver

		// Start initializes the process with the given context, Target, and input payloads.
		// It prepares the process for execution and returns an error if initialization fails.
		Start(stdcontext.Context, relay.PID, payload.Payloads) error

		// Step advances process state by one iteration.
		// Returns StepResult indicating next action and error if process failed.
		Step() (StepResult, error)

		// Terminate notifies the process about termination, triggers lifecycle handling.
		Terminate()
	}

	Workflow interface {
		Process
		Commands() []runtime.Command
	}

	// Lifecycle encapsulates the supervision relationship between processes.
	// It defines how processes monitor and link to each other, affecting error
	// propagation and termination behavior. @see topology
	Lifecycle struct {
		// Parent is the Target of the parent process, used for monitoring and linking
		Parent relay.PID

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
		HostID relay.HostID

		// Source is the registry ID of the process prototype to instantiate
		Source registry.ID

		// UniqID is an optional unique identifier for the process instance.
		// If not provided, one will be generated automatically.
		UniqID string

		// Input contains the initialization data for the process
		Input payload.Payloads

		// Context contains context overrides to apply when starting this process.
		// These pairs are set in the new FrameContext after inheritance but before sealing.
		// Can include actor, scope, custom values, or any other context keys.
		Context []context.Pair

		// Options contains runtime configuration options for the process.
		// Can include lifecycle parameters (lifecycle.parent, lifecycle.monitor, lifecycle.link),
		// timeouts, retry settings, or any other runtime options.
		Options attrs.Attributes

		// OnStart contains user-defined lifecycle hooks to execute after the process starts.
		// Mutators can append additional hooks during Start processing.
		OnStart []OnStart

		// OnComplete contains user-defined lifecycle hooks to execute when the process completes.
		// Mutators can append additional hooks during Start processing.
		OnComplete []OnComplete
	}

	// Launch contains the information needed to launch a process on a managed host.
	// It is used by managed hosts to start a specific process instance and
	// includes both the process and its lifecycle configuration.
	// This structure is created by the Manager from a Start request after applying mutators.
	Launch struct {
		// PID is the process identifier to assign to the new process
		PID relay.PID

		// Source is the registry ID of the process being launched
		Source registry.ID

		// Process is the process instance to start
		Process Process

		// Input contains the initialization data for the process
		Input payload.Payloads

		// Context contains context overrides to apply when launching this process.
		// These pairs are set in the new FrameContext after inheritance but before sealing.
		// Includes user-provided pairs and mutator additions.
		Context []context.Pair

		// Options contains runtime configuration options for the process.
		// Includes user-provided options and mutator additions.
		// Lifecycle parameters are stored here (lifecycle.parent, lifecycle.monitor, lifecycle.link).
		Options attrs.Attributes

		// OnStart contains lifecycle hooks to execute after the process starts.
		// Includes user hooks, mutator hooks, and topology hooks.
		OnStart []OnStart

		// OnComplete contains lifecycle hooks to execute when the process completes.
		// Includes user hooks, mutator hooks, and topology hooks.
		OnComplete []OnComplete
	}

	// Dispatch contains the information needed to dispatch a process to a delegated host.
	// It is used by delegated hosts (e.g., Temporal workers) to create and start a process remotely.
	// Unlike Launch, Dispatch does not include the Process instance since delegated hosts
	// create their own processes from the Source registry ID.
	// This structure is created by the Manager from a Start request after applying mutators.
	Dispatch struct {
		// PID is the process identifier to assign to the new process
		PID relay.PID

		// Source is the registry ID of the process type to instantiate
		Source registry.ID

		// Input contains the initialization data for the process
		Input payload.Payloads

		// Context contains context overrides to apply when launching this process.
		// These pairs are set in the new FrameContext after inheritance but before sealing.
		// Includes user-provided pairs and mutator additions.
		Context []context.Pair

		// Options contains runtime configuration options for the process.
		// Includes user-provided options and mutator additions.
		// Lifecycle parameters are stored here (lifecycle.parent, lifecycle.monitor, lifecycle.link).
		Options attrs.Attributes
	}

	// Terminator defines the interface for forcefully stopping a running process.
	Terminator interface {
		// Terminate forcefully stops a running process identified by pid.
		// Returns an error if the termination fails.
		Terminate(stdcontext.Context, relay.PID) error
	}

	// Canceller defines the interface for gracefully canceling a running process.
	Canceller interface {
		// Cancel sends a cancellation signal to a process identified by pid.
		// from is the Target of the cancellation requester, and deadline specifies
		// when the process will be forcefully terminated if it doesn't stop gracefully.
		// Returns an error if the cancellation request cannot be sent.
		Cancel(stdcontext.Context, relay.PID, relay.PID, time.Time) error
	}

	// Manager defines the interface for process lifecycle management.
	// It combines process starting, termination, and cancellation capabilities.
	Manager interface {
		Terminator
		Canceller

		// Start launches a new process according to the provided configuration.
		// Returns the Target of the started process or an error if the process
		// cannot be started.
		Start(ctx stdcontext.Context, start *Start) (relay.PID, error)
	}

	// Host defines the interface for process execution environments.
	// Hosts are responsible for executing processes and can receive messages
	// and terminate running processes.
	Host interface {
		relay.Receiver
		Terminator
	}

	// Managed defines the interface for managed process hosts.
	// Managed hosts receive process instances from the manager and
	// are responsible for executing them locally.
	Managed interface {
		Host

		// Launch starts a process according to the provided launch configuration.
		// It handles both the process execution and its lifecycle management.
		// Returns the Target of the started process or an error if the process
		// cannot be started.
		Launch(ctx stdcontext.Context, launch *Launch) (relay.PID, error)
	}

	// Delegated defines the interface for delegated process hosts.
	// Delegated hosts receive dispatch information from the manager and
	// are responsible for creating and executing the processes themselves remotely.
	// Examples include Temporal workers or other external executors.
	Delegated interface {
		Host

		// Dispatch starts a process remotely with the given lifecycle and dispatch configuration.
		// The delegated host creates the process instance from dispatch.Source.
		// Returns the PID of the started process or an error if the process cannot be started.
		Dispatch(ctx stdcontext.Context, lf Lifecycle, dispatch *Dispatch) (relay.PID, error)
	}
)
