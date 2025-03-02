// Package pubsub provides a publish-subscribe messaging system for inter-component communication.
package pubsub

import (
	"context"
	"errors"
	"fmt"
	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"strings"
)

// System constants for node management
const (
	// System identifies the node management system in the event context
	System event.System = "node"

	// HostRegister is emitted to request host registration
	HostRegister event.Kind = "node.register_host"
	// HostDelete is emitted to request host removal
	HostDelete event.Kind = "node.remove_host"

	// HostAccept is emitted when a host registration is successful
	HostAccept event.Kind = "node.accept_host"
	// HostReject is emitted when a host registration fails
	HostReject event.Kind = "node.reject_host"
)

// Common errors returned by pubsub operations
var (
	// ErrAlreadyAttached indicates that a receiver is already attached to the specified PID
	ErrAlreadyAttached = errors.New("receiver already attached")
	// ErrHostNotFound indicates that the requested host could not be found
	ErrHostNotFound = errors.New("host not found")
	// ErrHostAlreadyExists indicates that a host with the given ID is already registered
	ErrHostAlreadyExists = errors.New("host already exists")
)

type (
	// NodeID uniquely identifies a node in the pubsub network
	NodeID = string

	// HostID uniquely identifies a host within a node
	HostID = string

	// PID represents a Process Identifier that uniquely identifies a process in the system.
	// It contains node, host, process ID, and a unique identifier components.
	PID struct {
		// Node identifies which node the process belongs to
		Node NodeID `json:"node"`
		// Host identifies which host the process belongs to
		Host HostID `json:"host"`
		// ID contains the process's registry identifier
		ID registry.ID `json:"id"`
		// UniqID contains a unique instance identifier
		UniqID string `json:"uniq_id"`
	}

	// Topic represents a string identifier for a message channel or category
	Topic = string

	// Message represents a single message in the pub/sub system containing a topic and payload data
	Message struct {
		// Topic identifies the message category
		Topic Topic
		// Payloads contains the actual message data
		Payloads payload.Payloads
	}

	// Host defines an interface for components that can receive and forward messages
	Host interface {
		Receiver
		// Attach connects a process (identified by PID) to a message channel
		// Returns a cancel function to detach and any error that occurred
		Attach(PID, chan *Package) (context.CancelFunc, error)
		// Detach disconnects a process (identified by PID) from the host
		Detach(PID)
	}

	// Node represents a messaging node that can host and route messages between multiple hosts
	Node interface {
		Host
		// ID returns the unique identifier for this node
		ID() NodeID
		// HostRegister adds a host to this node with the specified ID
		RegisterHost(HostID, Host) error
		// UnregisterHost removes a host from this node
		UnregisterHost(HostID)
	}

	// Receiver defines the interface for components that can send messages upstream in the pub/sub system
	Receiver interface {
		// Send dispatches a package to the upstream receiver
		Send(*Package) error
	}
)

// String formats the PID as a pipe-delimited string wrapped in curly braces.
// Without a node it looks like: "{host|ns:name|procname}"
// With a node it looks like: "{node@host|ns:name|procname}"
func (p PID) String() string {
	var formatted string
	if p.Node == "" {
		formatted = fmt.Sprintf("%s|%s|%s", p.Host, p.ID.String(), p.UniqID)
	} else {
		formatted = fmt.Sprintf("%s@%s|%s|%s", p.Node, p.Host, p.ID.String(), p.UniqID)
	}
	return fmt.Sprintf("{%s}", formatted)
}

// ParsePID parses a pipe-delimited string wrapped in curly braces into a PID.
// It accepts the following formats:
//   - "{host|ns:name|procname}"
//   - "{node@host|ns:name|procname}"
//
// Returns the parsed PID and any error that occurred during parsing.
func ParsePID(s string) (PID, error) {
	var pid PID

	// Done wrapping curly braces, if present.
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")

	parts := strings.Split(s, "|")
	if len(parts) != 3 {
		return pid, fmt.Errorf("invalid pid format: expected 3 parts separated by '|', got %d", len(parts))
	}

	// Parse the host part which may include a node using the "node@host" format.
	hostPart := parts[0]
	if idx := strings.Index(hostPart, "@"); idx >= 0 {
		pid.Node = hostPart[:idx]
		pid.Host = hostPart[idx+1:]
	} else {
		pid.Host = hostPart
	}

	// Parse the registry ID and process name.
	pid.ID = registry.ParseID(parts[1])
	pid.UniqID = parts[2]

	return pid, nil
}
