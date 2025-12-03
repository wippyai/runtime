// Package topology provides process communication and lifecycle management.
package topology

import (
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

// System constants for host and topic identifiers
const (
	// ControlHost identifies the control node in the pub/sub system
	ControlHost relay.HostID = "node:control"

	// TopicInbox is the inbox topic for process messages
	TopicInbox relay.Topic = "@pid/inbox"
	// TopicEvents is the events topic for process lifecycle events
	TopicEvents relay.Topic = "@pid/events"
)

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
		Register(name string, pid relay.PID) error

		// Unregister removes a name registration
		// Returns true if the name was registered and has been removed
		Unregister(name string) bool

		// Lookup finds the Target registered with a given name
		// Returns the Target and true if found, empty Target and false if not found
		Lookup(name string) (relay.PID, bool)

		// Remove completely removes a pid from a registry
		Remove(pid relay.PID)
	}

	// Monitor defines the interface for process monitoring
	Monitor interface {
		// Wait attaches a caller to monitor a specific pid.
		// Returns error if pid is not registered or already being monitored by caller.
		Wait(caller, pid relay.PID) error

		// Release removes a caller's monitoring of a specific pid.
		Release(caller, pid relay.PID) error
	}

	// Links defines the interface for managing process links
	Links interface {
		// Link establishes a bidirectional link between two processes.
		// Both processes must be registered first.
		Link(from, to relay.PID) error

		// Unlink removes a bidirectional link between two processes.
		Unlink(from, to relay.PID) error

		// GetLinks returns all processes linked to the given pid
		GetLinks(pid relay.PID) []relay.PID
	}

	// Topology combines monitoring and linking capabilities
	Topology interface {
		Monitor
		Links

		// Register registers a pid that can be monitored.
		// This should be called before any process can be monitored.
		Register(pid relay.PID) error

		// Notify sends exit event to all watchers and links of a pid.
		Notify(pid relay.PID, result *runtime.Result)

		// Remove completely removes a pid and all its watchers, destroying all links.
		Remove(pid relay.PID)
	}

	// ExitEvent represents a process exit notification
	ExitEvent struct {
		// At is the timestamp when the event occurred
		At time.Time `json:"at"`
		// Kind identifies the type of event
		Kind Kind `json:"kind"`
		// From identifies the source process
		From relay.PID `json:"from"`
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
		From relay.PID `json:"from"`
		// Deadline specifies when the cancellation should take effect
		Deadline time.Time `json:"deadline"`
	}

	// MonitorRequestEvent requests monitoring of a PID
	MonitorRequestEvent struct {
		At     time.Time `json:"at"`
		Kind   Kind      `json:"kind"`
		Caller relay.PID `json:"caller"`
		Target relay.PID `json:"target"`
	}

	// MonitorReleaseEvent releases monitoring of a PID
	MonitorReleaseEvent struct {
		At     time.Time `json:"at"`
		Kind   Kind      `json:"kind"`
		Caller relay.PID `json:"caller"`
		Target relay.PID `json:"target"`
	}

	// LinkRequestEvent requests bidirectional link with a PID
	LinkRequestEvent struct {
		At   time.Time `json:"at"`
		Kind Kind      `json:"kind"`
		From relay.PID `json:"from"`
		To   relay.PID `json:"to"`
	}

	// UnlinkRequestEvent requests removing bidirectional link
	UnlinkRequestEvent struct {
		At   time.Time `json:"at"`
		Kind Kind      `json:"kind"`
		From relay.PID `json:"from"`
		To   relay.PID `json:"to"`
	}
)

// Cancel creates a package for requesting cancellation of a process.
// The package is sent to the target process with a specified deadline.
func Cancel(from, to relay.PID, deadline time.Time) *relay.Package {
	return relay.NewPackage(
		relay.PID{UniqID: "topology"},
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
func Exit(pid relay.PID, result payload.Payload, err error) *relay.Package {
	return relay.NewPackage(
		relay.PID{UniqID: "topology"},
		pid,
		TopicEvents,
		payload.New(&ExitEvent{
			At:   time.Now(),
			From: pid,
			Kind: KindExit,
			Result: &runtime.Result{
				Value: result,
				Error: err,
			},
		}),
	)
}

// MonitorRequest creates a package for requesting monitoring of a remote PID.
func MonitorRequest(caller, target relay.PID) *relay.Package {
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
func MonitorRelease(caller, target relay.PID) *relay.Package {
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
func LinkRequest(from, to relay.PID) *relay.Package {
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
func UnlinkRequest(from, to relay.PID) *relay.Package {
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
