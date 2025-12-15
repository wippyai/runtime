package relay

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/pid"
	api "github.com/wippyai/runtime/api/relay"
)

// Node represents a messaging node in the pub/sub system that manages a
// local collection of hosts. It is responsible for routing messages to the
// correct host within this node.
//
// This implementation does not handle routing to external nodes; all messages
// must be targeted to a host registered within this Node instance.
type Node struct {
	nodeID pid.NodeID
	hosts  sync.Map // stores mapping: HostID -> api.Receiver
}

// NewNode creates a new, isolated messaging node with the specified ID.
func NewNode(nodeID pid.NodeID) *Node {
	return &Node{
		nodeID: nodeID,
	}
}

// ID returns the node's identifier.
func (n *Node) ID() pid.NodeID {
	return n.nodeID
}

// RegisterHost adds a new host to the node with the specified host ID.
// Returns an error if a host with the same ID is already registered.
func (n *Node) RegisterHost(hostID pid.HostID, host api.Receiver) error {
	_, loaded := n.hosts.LoadOrStore(hostID, host)
	if loaded {
		return api.NewHostExistsError(hostID, n.nodeID)
	}
	return nil
}

// UnregisterHost removes a host from the node by its host ID.
func (n *Node) UnregisterHost(hostID pid.HostID) {
	n.hosts.Delete(hostID)
}

// GetHost returns a host by ID if it exists.
func (n *Node) GetHost(hostID pid.HostID) (api.Receiver, bool) {
	h, ok := n.hosts.Load(hostID)
	if !ok {
		return nil, false
	}
	receiver, ok := h.(api.Receiver)
	if !ok {
		return nil, false
	}
	return receiver, true
}

// Send delivers a package to its destination. The destination must be a host
// registered within this node.
func (n *Node) Send(pkg *api.Package) error {
	if pkg == nil {
		return NewNilPackageError()
	}

	if pkg.Target.Node != "" && pkg.Target.Node != n.nodeID {
		return api.NewExternalNodeError(pkg.Target.Node)
	}

	h, ok := n.hosts.Load(pkg.Target.Host)
	if !ok {
		return api.NewHostNotFoundError(pkg.Target.Host, n.nodeID)
	}

	receiver, ok := h.(api.Receiver)
	if !ok {
		return NewInvalidHostTypeError(pkg.Target.Host, n.nodeID)
	}

	return receiver.Send(pkg)
}

// Attach connects a process ID to a channel for receiving packages.
// Only works with hosts that implement AttachableReceiver.
func (n *Node) Attach(p pid.PID, ch chan *api.Package) (context.CancelFunc, error) {
	if p.Node != "" && p.Node != n.nodeID {
		return nil, api.NewExternalNodeError(p.Node)
	}

	h, ok := n.hosts.Load(p.Host)
	if !ok {
		return nil, api.NewHostNotFoundError(p.Host, n.nodeID)
	}

	attachable, ok := h.(api.AttachableReceiver)
	if !ok {
		return nil, api.NewHostNotAttachableError(p.Host)
	}

	return attachable.Attach(p, ch)
}

// Detach disconnects a process ID from its receive channel.
func (n *Node) Detach(p pid.PID) {
	if p.Node != "" && p.Node != n.nodeID {
		return
	}

	h, ok := n.hosts.Load(p.Host)
	if !ok {
		return
	}

	if attachable, ok := h.(api.AttachableReceiver); ok {
		attachable.Detach(p)
	}
}
