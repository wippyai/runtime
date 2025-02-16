package pubsub

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/pubsub"
	"sync"
	"sync/atomic"
)

// Node is our nested pubsub that delegates messages to a host if the message is local,
// or forwards to an upstream host if the node in the PID does not match.
type Node struct {
	nodeID   pubsub.NodeID
	hosts    sync.Map                        // stores mapping: pubsub.HostID -> Host
	upstream atomic.Pointer[pubsub.Upstream] // Parent plane
}

// NewNode creates a new Node with our node's ID.
// The upstream parameter can be nil if you don't have an upstream.
func NewNode(nodeID pubsub.NodeID, upstream *pubsub.Upstream) *Node {
	return &Node{
		nodeID: nodeID,
	}
}

// RegisterHost registers a host under a host ID.
func (n *Node) RegisterHost(hostID pubsub.HostID, host pubsub.Host) {
	n.hosts.Store(hostID, host)
}

// UnregisterHost removes a host from the node.
func (n *Node) UnregisterHost(hostID pubsub.HostID) {
	n.hosts.Delete(hostID)
}

// Send routes the message. If the PID's Node is empty or matches our nodeID,
// it delegates the message to the corresponding host. Otherwise, it uses the upstream.
func (n *Node) Send(ctx context.Context, pid pubsub.PID, msgs ...*pubsub.Message) error {
	if pid.Node == "" || pid.Node == n.nodeID {
		if h, ok := n.hosts.Load(pid.Host); ok {
			host, ok := h.(Host)
			if !ok {
				return fmt.Errorf("host %s has invalid type", pid.Host)
			}
			return host.Send(ctx, pid, msgs...)
		}
		return fmt.Errorf("host %s not found in node", pid.Host)
	}

	return fmt.Errorf("no upstream available for non-local node %s, yet", pid.Node)
}

// Attach delegates the receiver attachment in the same way as Send.
func (n *Node) Attach(pid pubsub.PID, receiver pubsub.Downstream) (error, context.CancelFunc) {
	if pid.Node == "" || pid.Node == n.nodeID {
		if h, ok := n.hosts.Load(pid.Host); ok {
			host, ok := h.(Host)
			if !ok {
				return fmt.Errorf("host %s has invalid type", pid.Host), nil
			}
			return host.Attach(pid, receiver)
		}
		return fmt.Errorf("host %s not found in node", pid.Host), nil
	}

	return fmt.Errorf("no upstream available for non-local node %s", pid.Node), nil
}
