package pubsub

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/pubsub"
	"sync"
	"sync/atomic"
)

// Node is a nested pubsub that delegates messages to a host if the message is local,
// or forwards to an upstream host if the node in the PID does not match.
type Node struct {
	nodeID   pubsub.NodeID
	hosts    sync.Map                        // stores mapping: HostID -> Host
	upstream atomic.Pointer[pubsub.Upstream] // Parent plane
}

// NewNode creates a new Node with the specified node ID.
// The upstream parameter can be nil if you don't have an upstream.
func NewNode(nodeID NodeID, upstream *Upstream) *Node {
	n := &Node{
		nodeID: nodeID,
	}
	if upstream != nil {
		n.upstream.Store(upstream)
	}
	return n
}

// RegisterHost registers a host under a host ID.
func (n *Node) RegisterHost(hostID HostID, host Host) {
	n.hosts.Store(hostID, host)
}

// UnregisterHost removes a host from the node.
func (n *Node) UnregisterHost(hostID HostID) {
	n.hosts.Delete(hostID)
}

// Send routes the message. If the PID's Node is empty or matches our nodeID,
// it delegates the message to the corresponding host. Otherwise, it uses the upstream.
func (n *Node) Send(ctx context.Context, pid PID, msgs ...*Message) error {
	// Handle local messages
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

	// Handle upstream messages if we have an upstream configured
	if upstream := n.upstream.Load(); upstream != nil {
		return (*upstream).Send(ctx, pid, msgs...)
	}

	return fmt.Errorf("no upstream available for non-local node %s", pid.Node)
}

// Attach delegates the receiver attachment using the same routing logic as Send.
func (n *Node) Attach(pid PID, ch chan []*Message) (error, context.CancelFunc) {
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
