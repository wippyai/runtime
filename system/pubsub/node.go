package pubsub

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	api "github.com/ponyruntime/pony/api/pubsub"
)

// Node represents a messaging node in the pub/sub system that manages multiple hosts
// and routes messages between them. Nodes can also forward messages to upstream
// receivers for inter-node communication in distributed setups.
type Node struct {
	nodeID   api.NodeID
	hosts    sync.Map                     // stores mapping: HostID -> api.Host
	upstream atomic.Pointer[api.Receiver] // Parent plane
}

// NewNode creates a new messaging node with the specified node id and optional
// upstream receiver. If upstream is not nil, the node will forward messages
// to it when they are destined for other nodes.
func NewNode(nodeID api.NodeID, upstream *api.Receiver) *Node {
	n := &Node{
		nodeID: nodeID,
	}
	if upstream != nil {
		n.upstream.Store(upstream)
	}
	return n
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

// Send delivers a package to its destination. If the destination is in this node,
// it routes to the appropriate host. Otherwise, it forwards to the upstream
// receiver if one is configured.
// Returns an error if the destination host is not found or upstream is not configured
// for external nodes.
func (n *Node) Send(pkg *api.Package) error {
	// Handle local messages
	if pkg.Target.Node == "" || pkg.Target.Node == n.nodeID {
		if h, ok := n.hosts.Load(pkg.Target.Host); ok {
			host, ok := h.(api.Host)
			if !ok {
				return fmt.Errorf("host %s has invalid type", pkg.Target.Host)
			}

			return host.Send(pkg)
		}
		return fmt.Errorf("host %s not found in node", pkg.Target.Host)
	}

	// Transparent hosts can manage routing for us (if represented locally), e.g. Temporal, NATS, etc
	if h, ok := n.hosts.Load(pkg.Target.Host); ok {
		host, ok := h.(api.TransparentHost)
		if !ok {
			return fmt.Errorf("host %s has invalid type", pkg.Target.Host)
		}

		err := host.Send(pkg)
		if err != nil {
			return fmt.Errorf("host %s failed to send package: %w", pkg.Target.Host, err)
		}

		return nil
	}

	// Handle upstream messages if we have an upstream configured
	if upstream := n.upstream.Load(); upstream != nil {
		return (*upstream).Send(pkg)
	}

	return fmt.Errorf("no upstream available for non-local node %s", pkg.Target.Node)
}

// Attach connects a process ID to a channel for receiving packages.
// Returns a cancel function to detach the channel and an error if the host
// is not found or the Target refers to a non-local node.
func (n *Node) Attach(pid api.PID, ch chan *api.Package) (context.CancelFunc, error) {
	if pid.Node == "" || pid.Node == n.nodeID {
		if h, ok := n.hosts.Load(pid.Host); ok {
			host, ok := h.(api.Host)
			if !ok {
				return nil, fmt.Errorf("host %s has invalid type", pid.Host)
			}
			return host.Attach(pid, ch)
		}
		return nil, fmt.Errorf("host %s not found in node", pid.Host)
	}

	return nil, fmt.Errorf("no upstream available for non-local node %s", pid.Node)
}

// Detach disconnects a process ID from its receive channel.
// If the Target refers to a non-local node or the host is not found, this is a no-op.
func (n *Node) Detach(pid api.PID) {
	if pid.Node == "" || pid.Node == n.nodeID {
		if h, ok := n.hosts.Load(pid.Host); ok {
			if host, ok := h.(api.Host); ok {
				host.Detach(pid)
			}
		}
	}
}
