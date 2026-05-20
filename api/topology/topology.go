// SPDX-License-Identifier: MPL-2.0

// Package topology provides process communication and lifecycle management.
package topology

import (
	"context"
	"time"

	"github.com/wippyai/runtime/api/globalreg"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

// System constants for host and topic identifiers
const (
	// ControlHost identifies the control node in the pub/sub system
	ControlHost pid.HostID = "node:control"

	// TopicInbox is the inbox topic for process messages
	TopicInbox relay.Topic = "@pid/inbox"
	// TopicEvents is the events topic for process lifecycle events
	TopicEvents relay.Topic = "@pid/events"
)

// SystemPID is the sender PID for topology system messages.
var SystemPID = pid.PID{UniqID: "topology"}

// Registration mode constants.
const (
	// Local is the default; the name is visible only on the registering node.
	Local RegistrationMode = 0
	// Global registers the name cluster-wide via Raft consensus.
	Global RegistrationMode = 1
	// Eventual registers the name cluster-wide via gossip/CRDT.
	// No distributed lock; converges after partition heal.
	// Sized for ~100k user-session-class names per cluster.
	Eventual RegistrationMode = 2
)

// Event kind constants for process lifecycle events.
const (
	// Cancel indicates a cancellation request.
	Cancel Kind = "pid.cancel"
	// Exit indicates a process has exited.
	Exit Kind = "pid.exit"
	// LinkDown indicates a linked process is down.
	LinkDown Kind = "pid.link.down"
	// MonitorRequest requests monitoring of a remote PID.
	MonitorRequest Kind = "pid.monitor.request"
	// MonitorRelease releases monitoring of a remote PID.
	MonitorRelease Kind = "pid.monitor.release"
	// LinkRequest requests linking with a remote PID.
	LinkRequest Kind = "pid.link.request"
	// UnlinkRequest requests unlinking from a remote PID.
	UnlinkRequest Kind = "pid.unlink.request"
)

type (
	// Kind represents the type of a topology event
	Kind = string

	// RegistrationMode controls whether a name is registered locally or globally.
	RegistrationMode int

	// PIDRegistry defines the interface for a Target registry with Erlang-style semantics
	PIDRegistry interface {
		// Register associates a name with a PID atomically.
		// Returns (p, nil) on success.
		// Returns (existingPID, ErrNameAlreadyRegistered) if name is taken by a different PID.
		// Re-registering the same name with same PID is allowed and returns (p, nil).
		Register(name string, p pid.PID) (pid.PID, error)

		// Unregister removes a name registration
		// Returns true if the name was registered and has been removed
		Unregister(name string) bool

		// Lookup finds the Target registered with a given name.
		// Checks the global registry first (if available), then local.
		// Returns the Target and true if found, empty Target and false if not found
		Lookup(name string) (pid.PID, bool)

		// Remove completely removes a pid from a registry
		Remove(p pid.PID)
	}

	// GlobalRegistry provides cluster-wide name registration via Raft consensus.
	// Reads are served from the local replica; writes go through Raft.
	// Fencing tokens protect against stale references.
	GlobalRegistry interface {
		// Lookup reads from the local Raft FSM replica. See
		// globalreg.Registry.Lookup for option semantics.
		Lookup(ctx context.Context, name string, opts ...globalreg.LookupOption) (globalreg.LookupResult, error)

		// Deprecated: use Lookup(ctx, name, globalreg.WithFence()).
		LookupWithFence(name string) globalreg.LookupResult

		// Deprecated: use globalreg.ValidateFence(ctx, reg, name, token).
		ValidateFence(name string, token uint64) error
	}

	// EventualRegistry provides cluster-wide name registration via gossip/CRDT.
	// Eventually consistent — converges after partition heal. No fencing tokens
	// (no total order). Sized for ~100k user-session-class names. The Lookup
	// surface reuses globalreg's option types for API parity; WithFence() is
	// accepted and yields FenceToken=0 (eventual registry has no total order).
	EventualRegistry interface {
		Lookup(ctx context.Context, name string, opts ...globalreg.LookupOption) (globalreg.LookupResult, error)
	}

	// Monitor defines the interface for process monitoring
	Monitor interface {
		// Monitor attaches a caller to monitor a specific pid.
		// Returns error if pid is not registered or already being monitored by caller.
		Monitor(caller, target pid.PID) error

		// Demonitor removes a caller's monitoring of a specific pid.
		Demonitor(caller, target pid.PID) error
	}

	// Links defines the interface for managing process links
	Links interface {
		// Link establishes a bidirectional link between two processes.
		// Both processes must be registered first.
		Link(from, to pid.PID) error

		// Unlink removes a bidirectional link between two processes.
		Unlink(from, to pid.PID) error

		// GetLinks returns all processes linked to the given pid
		GetLinks(p pid.PID) []pid.PID
	}

	// Topology combines monitoring and linking capabilities
	Topology interface {
		Monitor
		Links

		// Register registers a pid that can be monitored.
		// This should be called before any process can be monitored.
		Register(p pid.PID) error

		// Complete notifies watchers/links and removes the pid in one operation.
		Complete(p pid.PID, result *runtime.Result)

		// Remove completely removes a pid and all its watchers, destroying all links.
		Remove(p pid.PID)
	}

	// ExitEvent represents a process exit notification
	ExitEvent struct {
		At     time.Time       `json:"at"`
		Result *runtime.Result `json:"result"`
		From   pid.PID         `json:"from"`
		Kind   Kind            `json:"kind"`
	}

	// CancelEvent represents a process cancellation request
	CancelEvent struct {
		At       time.Time `json:"at"`
		Deadline time.Time `json:"deadline"`
		From     pid.PID   `json:"from"`
		Kind     Kind      `json:"kind"`
	}

	// MonitorRequestEvent requests monitoring of a PID
	MonitorRequestEvent struct {
		At     time.Time `json:"at"`
		Kind   Kind      `json:"kind"`
		Caller pid.PID   `json:"caller"`
		Target pid.PID   `json:"target"`
	}

	// MonitorReleaseEvent releases monitoring of a PID
	MonitorReleaseEvent struct {
		At     time.Time `json:"at"`
		Kind   Kind      `json:"kind"`
		Caller pid.PID   `json:"caller"`
		Target pid.PID   `json:"target"`
	}

	// LinkRequestEvent requests bidirectional link with a PID
	LinkRequestEvent struct {
		At   time.Time `json:"at"`
		Kind Kind      `json:"kind"`
		From pid.PID   `json:"from"`
		To   pid.PID   `json:"to"`
	}

	// UnlinkRequestEvent requests removing bidirectional link
	UnlinkRequestEvent struct {
		At   time.Time `json:"at"`
		Kind Kind      `json:"kind"`
		From pid.PID   `json:"from"`
		To   pid.PID   `json:"to"`
	}
)

// CancelPackage creates a package for requesting cancellation of a process.
// The package is sent to the target process with a specified deadline.
func CancelPackage(from, to pid.PID, deadline time.Time) *relay.Package {
	return relay.NewPackage(
		SystemPID,
		to,
		TopicEvents,
		payload.New(&CancelEvent{
			At:       time.Now(),
			From:     from,
			Kind:     Cancel,
			Deadline: deadline,
		}),
	)
}

// MonitorRequestPackage creates a package for requesting monitoring of a remote PID.
func MonitorRequestPackage(caller, target pid.PID) *relay.Package {
	return relay.NewPackage(
		caller,
		target,
		TopicEvents,
		payload.New(&MonitorRequestEvent{
			At:     time.Now(),
			Kind:   MonitorRequest,
			Caller: caller,
			Target: target,
		}),
	)
}

// MonitorReleasePackage creates a package for releasing monitoring of a remote PID.
func MonitorReleasePackage(caller, target pid.PID) *relay.Package {
	return relay.NewPackage(
		caller,
		target,
		TopicEvents,
		payload.New(&MonitorReleaseEvent{
			At:     time.Now(),
			Kind:   MonitorRelease,
			Caller: caller,
			Target: target,
		}),
	)
}

// LinkRequestPackage creates a package for requesting a link with a remote PID.
func LinkRequestPackage(from, to pid.PID) *relay.Package {
	return relay.NewPackage(
		from,
		to,
		TopicEvents,
		payload.New(&LinkRequestEvent{
			At:   time.Now(),
			Kind: LinkRequest,
			From: from,
			To:   to,
		}),
	)
}

// UnlinkRequestPackage creates a package for requesting unlinking from a remote PID.
func UnlinkRequestPackage(from, to pid.PID) *relay.Package {
	return relay.NewPackage(
		from,
		to,
		TopicEvents,
		payload.New(&UnlinkRequestEvent{
			At:   time.Now(),
			Kind: UnlinkRequest,
			From: from,
			To:   to,
		}),
	)
}
