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
	upstream atomic.Pointer[api.Receiver] // Parent plane
}

func NewNode(nodeID api.NodeID, upstream *api.Receiver) *Node {
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

func (n *Node) RegisterHost(hostID api.HostID, host api.Host) error {
	_, loaded := n.hosts.LoadOrStore(hostID, host)
	if loaded {
		return fmt.Errorf("host %s already exists in node %s", hostID, n.nodeID)
	}
	return nil
}

func (n *Node) UnregisterHost(hostID api.HostID) {
	n.hosts.Delete(hostID)
}

func (n *Node) Send(ctx context.Context, pkg *api.Package) error {
	// Handle local messages
	if pkg.PID.Node == "" || pkg.PID.Node == n.nodeID {
		if h, ok := n.hosts.Load(pkg.PID.Host); ok {
			host, ok := h.(api.Host)
			if !ok {
				return fmt.Errorf("host %s has invalid type", pkg.PID.Host)
			}
			return host.Send(ctx, pkg)
		}
		return fmt.Errorf("host %s not found in node", pkg.PID.Host)
	}

	// Handle upstream messages if we have an upstream configured
	if upstream := n.upstream.Load(); upstream != nil {
		return (*upstream).Send(ctx, pkg)
	}

	return fmt.Errorf("no upstream available for non-local node %s", pkg.PID.Node)
}

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

func (n *Node) Detach(pid api.PID) {
	if pid.Node == "" || pid.Node == n.nodeID {
		if h, ok := n.hosts.Load(pid.Host); ok {
			if host, ok := h.(api.Host); ok {
				host.Detach(pid)
			}
		}
	}
}
