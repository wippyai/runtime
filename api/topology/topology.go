// Package topology provides process communication and lifecycle management.
package topology

import (
	"time"

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

// Event kind constants for process lifecycle events.
const (
	// KindCancel indicates a cancellation request.
	KindCancel Kind = "pid.cancel"
	// KindExit indicates a process has exited.
	KindExit Kind = "pid.exit"
	// KindLinkDown indicates a linked process is down.
	KindLinkDown Kind = "pid.link.down"
	// KindMonitorRequest requests monitoring of a remote PID.
	KindMonitorRequest Kind = "pid.monitor.request"
	// KindMonitorRelease releases monitoring of a remote PID.
	KindMonitorRelease Kind = "pid.monitor.release"
	// KindLinkRequest requests linking with a remote PID.
	KindLinkRequest Kind = "pid.link.request"
	// KindUnlinkRequest requests unlinking from a remote PID.
	KindUnlinkRequest Kind = "pid.unlink.request"
)

type (
	// Kind represents the type of a topology event
	Kind = string

	// PIDRegistry defines the interface for a Target registry with Erlang-style semantics
	PIDRegistry interface {
		// Register associates a name with a Target
		// Returns error if name is already taken
		Register(name string, p pid.PID) error

		// Unregister removes a name registration
		// Returns true if the name was registered and has been removed
		Unregister(name string) bool

		// Lookup finds the Target registered with a given name
		// Returns the Target and true if found, empty Target and false if not found
		Lookup(name string) (pid.PID, bool)

		// Remove completely removes a pid from a registry
		Remove(p pid.PID)
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
		// At is the timestamp when the event occurred
		At time.Time `json:"at"`
		// Kind identifies the type of event
		Kind Kind `json:"kind"`
		// From identifies the source process
		From pid.PID `json:"from"`
		// Result contains the exit result information
		Result *runtime.Result `json:"result"`
	}

	// CancelEvent represents a process cancellation request
	CancelEvent struct {
		// At is the timestamp when the event occurred
		At time.Time `json:"at"`
		// Kind identifies the type of event
		Kind Kind `json:"kind"`
		// From identifies the source process
		From pid.PID `json:"from"`
		// Deadline specifies when the cancellation should take effect
		Deadline time.Time `json:"deadline"`
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

// Cancel creates a package for requesting cancellation of a process.
// The package is sent to the target process with a specified deadline.
func Cancel(from, to pid.PID, deadline time.Time) *relay.Package {
	return relay.NewPackage(
		SystemPID,
		to,
		TopicEvents,
		payload.New(&CancelEvent{
			At:       time.Now(),
			From:     from,
			Kind:     KindCancel,
			Deadline: deadline,
		}),
	)
}

// Exit creates a package for notifying about a process exit.
// The package includes the process result and any error that occurred.
func Exit(p pid.PID, result payload.Payload, err error) *relay.Package {
	return relay.NewPackage(
		SystemPID,
		p,
		TopicEvents,
		payload.New(&ExitEvent{
			At:   time.Now(),
			From: p,
			Kind: KindExit,
			Result: &runtime.Result{
				Value: result,
				Error: err,
			},
		}),
	)
}

// MonitorRequest creates a package for requesting monitoring of a remote PID.
func MonitorRequest(caller, target pid.PID) *relay.Package {
	return relay.NewPackage(
		caller,
		target,
		TopicEvents,
		payload.New(&MonitorRequestEvent{
			At:     time.Now(),
			Kind:   KindMonitorRequest,
			Caller: caller,
			Target: target,
		}),
	)
}

// MonitorRelease creates a package for releasing monitoring of a remote PID.
func MonitorRelease(caller, target pid.PID) *relay.Package {
	return relay.NewPackage(
		caller,
		target,
		TopicEvents,
		payload.New(&MonitorReleaseEvent{
			At:     time.Now(),
			Kind:   KindMonitorRelease,
			Caller: caller,
			Target: target,
		}),
	)
}

// LinkRequest creates a package for requesting a link with a remote PID.
func LinkRequest(from, to pid.PID) *relay.Package {
	return relay.NewPackage(
		from,
		to,
		TopicEvents,
		payload.New(&LinkRequestEvent{
			At:   time.Now(),
			Kind: KindLinkRequest,
			From: from,
			To:   to,
		}),
	)
}

// UnlinkRequest creates a package for requesting unlinking from a remote PID.
func UnlinkRequest(from, to pid.PID) *relay.Package {
	return relay.NewPackage(
		from,
		to,
		TopicEvents,
		payload.New(&UnlinkRequestEvent{
			At:   time.Now(),
			Kind: KindUnlinkRequest,
			From: from,
			To:   to,
		}),
	)
}
