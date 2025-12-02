package process

import (
	"context"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
)

// Lifecycle option keys for process supervision
const (
	LifecycleParentKey  = "lifecycle.parent"
	LifecycleMonitorKey = "lifecycle.monitor"
	LifecycleLinkKey    = "lifecycle.link"
)

// StartMutator modifies a Start request before process launch.
type StartMutator func(ctx context.Context, start *Start) (context.Context, error)

// Option keys for special cases
const (
	// OptionPID allows caller to specify a desired PID (e.g., for portal)
	OptionPID = "pid"
)

// Start contains the configuration needed to start a new process.
type Start struct {
	// HostID is the identifier of the host where the process will run
	HostID relay.HostID

	// Source is the registry ID of the process to create
	Source registry.ID

	// Input contains the initialization data for the process
	Input payload.Payloads

	// Context contains context overrides to apply when starting this process
	Context []ctxapi.Pair

	// Options contains runtime configuration options for the process.
	// Special keys: OptionPID to specify desired PID.
	Options attrs.Attributes
}

// Host is a unified interface for process execution environments.
// Hosts create processes internally from Source using their factory.
// Host assigns PID internally unless OptionPID is specified in Options.
type Host interface {
	relay.Receiver

	// Run launches a process according to the provided configuration.
	// The host creates the process internally and assigns PID.
	Run(ctx context.Context, start *Start) (relay.PID, error)

	// Terminate forcefully stops a running process.
	Terminate(ctx context.Context, pid relay.PID) error
}

// Canceller defines the interface for gracefully canceling a running process.
type Canceller interface {
	// Cancel sends a cancellation signal to a process.
	Cancel(ctx context.Context, from, pid relay.PID, deadline time.Time) error
}

// Manager defines the interface for process lifecycle management.
type Manager interface {
	Canceller

	// Start launches a new process according to the provided configuration.
	Start(ctx context.Context, start *Start) (relay.PID, error)

	// Terminate forcefully stops a running process.
	Terminate(ctx context.Context, pid relay.PID) error
}

// HostLookup finds hosts by ID.
type HostLookup interface {
	GetHost(hostID string) (Host, bool)
}
