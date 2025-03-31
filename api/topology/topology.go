// Package topology provides process communication and lifecycle management.
package topology

import (
	"errors"
	"time"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/runtime"
)

// System constants for host and topic identifiers
const (
	// ControlHost identifies the control node in the pub/sub system
	ControlHost pubsub.HostID = "node:control"

	// TopicInbox is the inbox topic for process messages
	TopicInbox pubsub.Topic = "@pid/inbox"
	// TopicEvents is the events topic for process lifecycle events
	TopicEvents pubsub.Topic = "@pid/events"
)

// Event kind constants for process lifecycle events
const (
	// KindCancel indicates a cancellation request
	KindCancel Kind = "pid.cancel"
	// KindExit indicates a process has exited
	KindExit Kind = "pid.exit"

	// KindLinkDown indicates a linked process is down
	KindLinkDown Kind = "pid.link.down"
	// KindLinkEstablished indicates a link has been established
	KindLinkEstablished Kind = "pid.link.established"
	// KindLinkRemoved indicates a link has been removed
	KindLinkRemoved Kind = "pid.link.removed"
)

// PIDRegistry errors that can occur during name registration operations
var (
	// ErrNameAlreadyRegistered indicates a name is already associated with a Target
	ErrNameAlreadyRegistered = errors.New("name already registered")
)

type (
	// Kind represents the type of a topology event
	Kind = string

	// PIDRegistry defines the interface for a Target registry with Erlang-style semantics
	PIDRegistry interface {
		// Register associates a name with a Target
		// Returns error if name is already taken
		Register(name string, pid pubsub.PID) error

		// Unregister removes a name registration
		// Returns true if the name was registered and has been removed
		Unregister(name string) bool

		// Lookup finds the Target registered with a given name
		// Returns the Target and true if found, empty Target and false if not found
		Lookup(name string) (pubsub.PID, bool)

		// Remove completely removes a pid from a registry
		Remove(pid pubsub.PID)
	}

	// Monitor defines the interface for process monitoring
	Monitor interface {
		// Wait attaches a caller to monitor a specific pid.
		// Returns error if pid is not registered or already being monitored by caller.
		Wait(caller, pid pubsub.PID) error

		// Release removes a caller's monitoring of a specific pid.
		Release(caller, pid pubsub.PID) error
	}

	// Links defines the interface for managing process links
	Links interface {
		// Link establishes a bidirectional link between two processes.
		// Both processes must be registered first.
		Link(from, to pubsub.PID) error

		// Unlink removes a bidirectional link between two processes.
		Unlink(from, to pubsub.PID) error

		// GetLinks returns all processes linked to the given pid
		GetLinks(pid pubsub.PID) []pubsub.PID
	}

	// Topology combines monitoring and linking capabilities
	Topology interface {
		Monitor
		Links

		// Register registers a pid that can be monitored.
		// This should be called before any process can be monitored.
		Register(pid pubsub.PID) error

		// Notify sends exit event to all watchers and links of a pid.
		Notify(pid pubsub.PID, result *runtime.Result)

		// Remove completely removes a pid and all its watchers, destroying all links.
		Remove(pid pubsub.PID)
	}

	// ExitEvent represents a process exit notification
	ExitEvent struct {
		// At is the timestamp when the event occurred
		At time.Time `json:"at"`
		// Kind identifies the type of event
		Kind Kind `json:"kind"`
		// From identifies the source process
		From pubsub.PID `json:"from"`
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
		From pubsub.PID `json:"from"`
		// Deadline specifies when the cancellation should take effect
		Deadline time.Time `json:"deadline"`
	}
)

// Cancel creates a package for requesting cancellation of a process.
// The package is sent to the target process with a specified deadline.
func Cancel(from, to pubsub.PID, deadline time.Time) *pubsub.Package {
	return pubsub.NewPackage(
		pubsub.PID{UniqID: "topology"},
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
func Exit(pid pubsub.PID, result payload.Payload, err error) *pubsub.Package {
	return pubsub.NewPackage(
		pubsub.PID{UniqID: "topology"},
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
