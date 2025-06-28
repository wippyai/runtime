// Package pubsub provides a publish-subscribe messaging system for inter-component communication.
package pubsub

import (
	"context"
	"errors"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/payload"
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
		// RegisterHost adds a host to this node with the specified ID
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
