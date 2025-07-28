package pubsub

import (
	"context"
	"fmt"
	"sync"

	api "github.com/ponyruntime/pony/api/pubsub"
)

// Node represents a messaging node in the pub/sub system that manages a
// local collection of hosts. It is responsible for routing messages to the
// correct host within this node.
//
// This implementation does not handle routing to external nodes; all messages
// must be targeted to a host registered within this Node instance.
type Node struct {
	nodeID api.NodeID
	hosts  sync.Map // stores mapping: HostID -> api.Host
}

// NewNode creates a new, isolated messaging node with the specified ID.
func NewNode(nodeID api.NodeID) *Node {
	return &Node{
		nodeID: nodeID,
	}
}

// ID returns the node's identifier.
func (n *Node) ID() api.NodeID {
	return n.nodeID
}

// RegisterHost adds a new host to the node with the specified host ID.
// Returns an error if a host with the same ID is already registered.
func (n *Node) RegisterHost(hostID api.HostID, host api.Host) error {
	_, loaded := n.hosts.LoadOrStore(hostID, host)
	if loaded {
		return fmt.Errorf("host %s already exists in node %s", hostID, n.nodeID)
	}
	return nil
}

// UnregisterHost removes a host from the node by its host ID.
func (n *Node) UnregisterHost(hostID api.HostID) {
	n.hosts.Delete(hostID)
}

// Send delivers a package to its destination. The destination must be a host
// registered within this node.
// Returns an error if the destination is for an external node or if the local
// host is not found.
func (n *Node) Send(pkg *api.Package) error {
	// A package is local if its target node is this node, or if the node is unspecified.
	if pkg.Target.Node == "" || pkg.Target.Node == n.nodeID {
		if h, ok := n.hosts.Load(pkg.Target.Host); ok {
			host, ok := h.(api.Host)
			if !ok {
				// This indicates a programming error where a non-Host type was stored.
				return fmt.Errorf("host %s in node %s has invalid type", pkg.Target.Host, n.nodeID)
			}
			return host.Send(pkg)
		}
		return fmt.Errorf("host %s not found in node %s", pkg.Target.Host, n.nodeID)
	}

	// This node does not route messages to other nodes.
	return fmt.Errorf("cannot route to external node %s", pkg.Target.Node)
}

// Attach connects a process ID to a channel for receiving packages.
// The PID must belong to a host registered within this node.
// Returns an error if the PID refers to an external node or if the local host
// is not found.
func (n *Node) Attach(pid api.PID, ch chan *api.Package) (context.CancelFunc, error) {
	// Attach is only for local processes.
	if pid.Node == "" || pid.Node == n.nodeID {
		if h, ok := n.hosts.Load(pid.Host); ok {
			host, ok := h.(api.Host)
			if !ok {
				return nil, fmt.Errorf("host %s in node %s has invalid type", pid.Host, n.nodeID)
			}
			return host.Attach(pid, ch)
		}
		return nil, fmt.Errorf("host %s not found in node %s", pid.Host, n.nodeID)
	}

	// Cannot attach to a process on an external node.
	return nil, fmt.Errorf("cannot attach to external node %s", pid.Node)
}

// Detach disconnects a process ID from its receive channel.
// This is a no-op if the PID refers to an external node or if the host is not found.
func (n *Node) Detach(pid api.PID) {
	if pid.Node == "" || pid.Node == n.nodeID {
		if h, ok := n.hosts.Load(pid.Host); ok {
			if host, ok := h.(api.Host); ok {
				host.Detach(pid)
			}
		}
	}
}
