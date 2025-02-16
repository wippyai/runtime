package pubsub

import (
	"context"
	"fmt"
	api "github.com/ponyruntime/pony/api/pubsub"
	"sync"
	"sync/atomic"
)

type Node struct {
	nodeID   api.NodeID
	hosts    sync.Map                     // stores mapping: HostID -> api.Host
	upstream atomic.Pointer[api.Upstream] // Parent plane
}

func NewNode(nodeID api.NodeID, upstream *api.Upstream) *Node {
	n := &Node{
		nodeID: nodeID,
	}
	if upstream != nil {
		n.upstream.Store(upstream)
	}
	return n
}

func (n *Node) ID() api.NodeID {
	return n.nodeID
}

func (n *Node) RegisterHost(hostID api.HostID, host api.Host) {
	n.hosts.Store(hostID, host)
}

func (n *Node) UnregisterHost(hostID api.HostID) {
	n.hosts.Delete(hostID)
}

func (n *Node) Send(ctx context.Context, pid api.PID, batch *api.Batch) error {
	// Handle local messages
	if pid.Node == "" || pid.Node == n.nodeID {
		if h, ok := n.hosts.Load(pid.Host); ok {
			host, ok := h.(api.Host)
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

func (n *Node) Attach(pid api.PID, ch chan *api.Batch) (error, context.CancelFunc) {
	if pid.Node == "" || pid.Node == n.nodeID {
		if h, ok := n.hosts.Load(pid.Host); ok {
			host, ok := h.(api.Host)
			if !ok {
				return fmt.Errorf("host %s has invalid type", pid.Host), nil
			}
			return host.Attach(pid, ch)
		}
		return fmt.Errorf("host %s not found in node", pid.Host), nil
	}

	return fmt.Errorf("no upstream available for non-local node %s", pid.Node), nil
}
