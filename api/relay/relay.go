// Package relay provides message relay and routing for inter-process communication.
package relay

import (
	"context"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
)

// System identifies the relay system in the event bus.
const System event.System = "relay"

// Event kinds for host operations.
const (
	HostRegister event.Kind = "host.register"
	HostDelete   event.Kind = "host.delete"
	HostAccept   event.Kind = "host.accept"
	HostReject   event.Kind = "host.reject"
)

// Event kinds for virtual node operations.
const (
	VirtualNodeRegister event.Kind = "vnode.register"
	VirtualNodeDelete   event.Kind = "vnode.delete"
	VirtualNodeAccept   event.Kind = "vnode.accept"
	VirtualNodeReject   event.Kind = "vnode.reject"
)

type (
	// NodeID uniquely identifies a node in the relay network.
	NodeID = string

	// HostID uniquely identifies a host within a node.
	HostID = string

	// Topic represents a message channel identifier.
	Topic = string

	// Message represents a single message with topic and payload.
	Message struct {
		Topic    Topic
		Payloads payload.Payloads
	}

	// Package combines source, target and messages for delivery.
	Package struct {
		Source   PID
		Target   PID
		Messages []*Message
	}

	// VirtualNodeInfo contains metadata about a virtual node.
	VirtualNodeInfo struct {
		NodeID   NodeID
		Receiver Receiver
	}
)

type (
	// Receiver defines the interface for message delivery.
	Receiver interface {
		Send(*Package) error
	}

	// Host defines an interface for components that receive and forward messages.
	Host interface {
		Receiver
	}

	// AttachableHost extends Host with channel-based message delivery.
	AttachableHost interface {
		Host
		Attach(PID, chan *Package) (context.CancelFunc, error)
		Detach(PID)
	}

	// Node represents a messaging node that hosts and routes messages.
	Node interface {
		Host
		ID() NodeID
		RegisterHost(HostID, Host) error
		UnregisterHost(HostID)
		GetHost(HostID) (Host, bool)
		Attach(PID, chan *Package) (context.CancelFunc, error)
		Detach(PID)
	}

	// NodeManager manages relay nodes and hosts.
	NodeManager interface {
		Node() Node
		Start(ctx context.Context) error
		Stop() error
	}
)

// AddMessage adds a new message to the package.
func (p *Package) AddMessage(topic Topic, payloads ...payload.Payload) {
	p.Messages = append(p.Messages, &Message{
		Topic:    topic,
		Payloads: payloads,
	})
}
