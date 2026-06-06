// SPDX-License-Identifier: MPL-2.0

// Package clustertest provides an in-process multi-node harness that wires the
// real raft, relay, and kv code paths so store.kv.* behavior can be proven
// end-to-end under failure injection (partition, kill, restart) without TCP.
package clustertest

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/cluster/internode"
)

// errBlocked models a partitioned or downed link.
var errBlocked = errors.New("clustertest: link blocked")

// mesh ties N in-process internode.ConnectionManagers together. SendToNode
// dispatches synchronously into the target's registered class receiver unless
// either endpoint is down or the directed link is partitioned.
type mesh struct {
	conns   map[cluster.NodeID]*meshConn
	down    map[cluster.NodeID]bool
	blocked map[string]bool
	mu      sync.Mutex
}

func newMesh() *mesh {
	return &mesh{
		conns:   map[cluster.NodeID]*meshConn{},
		down:    map[cluster.NodeID]bool{},
		blocked: map[string]bool{},
	}
}

func linkKey(src, dst cluster.NodeID) string { return src + ">" + dst }

func (m *mesh) connect(id cluster.NodeID) *meshConn {
	c := &meshConn{
		mesh:      m,
		self:      id,
		receivers: map[internode.Class]func(cluster.NodeID, []byte){},
		inbox:     make(chan inboundMsg, 8192),
		done:      make(chan struct{}),
	}
	m.mu.Lock()
	if old := m.conns[id]; old != nil {
		close(old.done)
	}
	m.conns[id] = c
	m.mu.Unlock()
	go c.deliverLoop()
	return c
}

// reachable reports whether src may deliver to dst right now.
func (m *mesh) reachable(src, dst cluster.NodeID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.down[src] || m.down[dst] {
		return false
	}
	return !m.blocked[linkKey(src, dst)] && !m.blocked[linkKey(dst, src)]
}

// partition isolates id from every other node (both directions).
func (m *mesh) partition(id cluster.NodeID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for other := range m.conns {
		if other == id {
			continue
		}
		m.blocked[linkKey(id, other)] = true
		m.blocked[linkKey(other, id)] = true
	}
}

// heal restores all links to id.
func (m *mesh) heal(id cluster.NodeID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for other := range m.conns {
		delete(m.blocked, linkKey(id, other))
		delete(m.blocked, linkKey(other, id))
	}
}

func (m *mesh) setDown(id cluster.NodeID, v bool) {
	m.mu.Lock()
	m.down[id] = v
	m.mu.Unlock()
}

// stopAll closes every conn's deliver goroutine (test teardown).
func (m *mesh) stopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.conns {
		select {
		case <-c.done:
		default:
			close(c.done)
		}
	}
}

// inboundMsg is one delivered frame, dispatched on the receiver's own goroutine
// to decouple sender and receiver (a real network is asynchronous; inline
// dispatch caused reentrancy/election instability under partition+heal).
type inboundMsg struct {
	recv func(cluster.NodeID, []byte)
	from cluster.NodeID
	data []byte
}

// meshConn implements internode.ConnectionManager for one endpoint.
type meshConn struct {
	mesh      *mesh
	receivers map[internode.Class]func(cluster.NodeID, []byte)
	inbox     chan inboundMsg
	done      chan struct{}
	self      cluster.NodeID
	mu        sync.Mutex
}

func (c *meshConn) Start(_ context.Context, _ func(cluster.NodeID, []byte)) error { return nil }
func (c *meshConn) Stop() error                                                   { return nil }

// deliverLoop drains the inbox FIFO so all inbound frames to this node are
// processed in order on a single dedicated goroutine.
func (c *meshConn) deliverLoop() {
	for {
		select {
		case <-c.done:
			return
		case m := <-c.inbox:
			m.recv(m.from, m.data)
		}
	}
}

func (c *meshConn) SendToNode(target cluster.NodeID, data []byte, class internode.Class) error {
	if !c.mesh.reachable(c.self, target) {
		return errBlocked
	}
	c.mesh.mu.Lock()
	peer := c.mesh.conns[target]
	c.mesh.mu.Unlock()
	if peer == nil {
		return fmt.Errorf("clustertest: unknown peer %q", target)
	}
	peer.mu.Lock()
	r := peer.receivers[class]
	peer.mu.Unlock()
	if r == nil {
		return errBlocked
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	select {
	case peer.inbox <- inboundMsg{from: c.self, data: cp, recv: r}:
		return nil
	case <-peer.done:
		return errBlocked
	}
}

func (c *meshConn) EnsureConnection(_ cluster.NodeID, _ string, _ int) {}
func (c *meshConn) DisconnectFromNode(_ cluster.NodeID)                {}
func (c *meshConn) ConnectedNodes() []cluster.NodeID                   { return nil }
func (c *meshConn) GetListenPort() int                                 { return 0 }
func (c *meshConn) AddManagedNode(_ cluster.NodeID)                    {}
func (c *meshConn) RemoveManagedNode(_ cluster.NodeID)                 {}
func (c *meshConn) IsManaged(_ cluster.NodeID) bool                    { return true }
func (c *meshConn) EvictOrphanNodes(_ map[cluster.NodeID]struct{}) int { return 0 }
func (c *meshConn) RecordDropReason(_ string)                          {}

func (c *meshConn) RegisterClassReceiver(class internode.Class, recv func(cluster.NodeID, []byte)) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if recv != nil && c.receivers[class] != nil {
		return false
	}
	c.receivers[class] = recv
	return true
}

var _ internode.ConnectionManager = (*meshConn)(nil)
