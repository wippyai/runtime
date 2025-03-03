package pubsub

import (
	"context"
	"fmt"
	api "github.com/ponyruntime/pony/api/pubsub"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// / ---- todo: tem
type stats struct {
	send  atomic.Int64
	send2 atomic.Int64
	send3 atomic.Int64
	send4 atomic.Int64
	send5 atomic.Int64
	send6 atomic.Int64
}

var s stats

func init() {
	s = stats{}
	go func() {
		for {
			time.Sleep(5 * time.Second)
			log.Printf("STATS %v %v %v %v %v %v",
				s.send.Load(),
				s.send2.Load(),
				s.send3.Load(),
				s.send4.Load(),
				s.send5.Load(),
				s.send6.Load())
		}
	}()
}

// Node represents a messaging node in the pub/sub system that manages multiple hosts
// and routes messages between them. Nodes can also forward messages to upstream
// receivers for inter-node communication in distributed setups.
type Node struct {
	nodeID   api.NodeID
	hosts    sync.Map                     // stores mapping: HostID -> api.Host
	upstream atomic.Pointer[api.Receiver] // Parent plane
}

// NewNode creates a new messaging node with the specified node ID and optional
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

var ix = 0

// Send delivers a package to its destination. If the destination is in this node,
// it routes to the appropriate host. Otherwise, it forwards to the upstream
// receiver if one is configured.
// Returns an error if the destination host is not found or upstream is not configured
// for external nodes.
func (n *Node) Send(pkg *api.Package) error {
	s.send.Add(1)

	// Handle local messages
	if pkg.PID.Node == "" || pkg.PID.Node == n.nodeID {
		if h, ok := n.hosts.Load(pkg.PID.Host); ok {
			host, ok := h.(api.Host)
			if !ok {
				return fmt.Errorf("host %s has invalid type", pkg.PID.Host)
			}

			s.send2.Add(1)
			return host.Send(pkg)
		}
		return fmt.Errorf("host %s not found in node", pkg.PID.Host)
	}

	// Handle upstream messages if we have an upstream configured
	if upstream := n.upstream.Load(); upstream != nil {
		return (*upstream).Send(pkg)
	}

	return fmt.Errorf("no upstream available for non-local node %s", pkg.PID.Node)
}

// Attach connects a process ID to a channel for receiving packages.
// Returns a cancel function to detach the channel and an error if the host
// is not found or the PID refers to a non-local node.
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
// If the PID refers to a non-local node or the host is not found, this is a no-op.
func (n *Node) Detach(pid api.PID) {
	if pid.Node == "" || pid.Node == n.nodeID {
		if h, ok := n.hosts.Load(pid.Host); ok {
			if host, ok := h.(api.Host); ok {
				host.Detach(pid)
			}
		}
	}
}
