package pubsub

import (
	"context"
	"fmt"
	api "github.com/ponyruntime/pony/api/pubsub"
	"sync"
	"sync/atomic"
)

// Node is a nested pubsub that delegates messages to a host if the message is local,
// or forwards to an upstream host if the node in the PID does not match.
type Node struct {
	nodeID   api.NodeID
	hosts    sync.Map                     // stores mapping: HostID -> Host
	upstream atomic.Pointer[api.Upstream] // Parent plane
}

// NewNode creates a new Node with the specified node ID.
// The upstream parameter can be nil if you don't have an upstream.
func NewNode(nodeID api.NodeID, upstream *api.Upstream) *Node {
	n := &Node{
		nodeID: nodeID,
	}
	if upstream != nil {
		n.upstream.Store(upstream)
	}
	return n
}

// RegisterHost registers a host under a host ID.
func (n *Node) RegisterHost(hostID api.HostID, host Host) {
	n.hosts.Store(hostID, host)
}

// UnregisterHost removes a host from the node.
func (n *Node) UnregisterHost(hostID api.HostID) {
	n.hosts.Delete(hostID)
}

// Send routes the message batch. If the PID's Node is empty or matches our nodeID,
// it delegates the message to the corresponding host. Otherwise, it uses the upstream.
func (n *Node) Send(ctx context.Context, pid api.PID, batch *api.Batch) error {
	// Handle local messages
	if pid.Node == "" || pid.Node == n.nodeID {
		if h, ok := n.hosts.Load(pid.Host); ok {
			host, ok := h.(Host)
			if !ok {
				return fmt.Errorf("host %s has invalid type", pid.Host)
			}
			return host.Send(ctx, pid, batch)
		}
		return fmt.Errorf("host %s not found in node", pid.Host)
	}

	// Handle upstream messages if we have an upstream configured
	if upstream := n.upstream.Load(); upstream != nil {
		return (*upstream).Send(ctx, pid, batch)
	}

	return fmt.Errorf("no upstream available for non-local node %s", pid.Node)
}

// Attach delegates the receiver attachment using the same routing logic as Send.
func (n *Node) Attach(pid api.PID, ch chan *api.Batch) (error, context.CancelFunc) {
	if pid.Node == "" || pid.Node == n.nodeID {
		if h, ok := n.hosts.Load(pid.Host); ok {
			host, ok := h.(Host)
			if !ok {
				return fmt.Errorf("host %s has invalid type", pid.Host), nil
			}
			return host.Attach(pid, ch)
		}
		return fmt.Errorf("host %s not found in node", pid.Host), nil
	}

	return fmt.Errorf("no upstream available for non-local node %s", pid.Node), nil
}
