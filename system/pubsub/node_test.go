package pubsub

import (
	"context"
	api "github.com/ponyruntime/pony/api/pubsub"
	"sync/atomic"
	"testing"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/stretchr/testify/assert"
)

// dummyHost is a stub that implements the Host interface.
type dummyHost struct {
	sendCalled   int32
	attachCalled int32
}

func (d *dummyHost) Send(ctx context.Context, pid api.PID, batch *api.Batch) error {
	atomic.AddInt32(&d.sendCalled, 1)
	return nil
}

func (d *dummyHost) Attach(pid api.PID, ch chan *api.Batch) (context.CancelFunc, error) {
	atomic.AddInt32(&d.attachCalled, 1)
	cancel := func() {}
	return cancel, nil
}

// dummyUpstream is a stub that implements the Upstream interface.
type dummyUpstream struct {
	sendCalled int32
}

func (d *dummyUpstream) Send(ctx context.Context, pid api.PID, batch *api.Batch) error {
	atomic.AddInt32(&d.sendCalled, 1)
	return nil
}

func TestNodeSendLocal(t *testing.T) {
	// Create a dummy host and register it with the node.
	dhost := &dummyHost{}
	nodeID := "node1"
	node := NewNode(nodeID, nil)
	assert.NoError(t, node.RegisterHost("host1", dhost))

	// Case 1: Local message with empty pid.Node.
	pidLocalEmpty := api.PID{
		Node:   "",
		Host:   "host1",
		ID:     registry.ID{NS: "ns", Name: "proc"},
		UniqID: "uniq",
	}
	batch := &api.Batch{{Topic: "local"}}
	err := node.Send(context.Background(), pidLocalEmpty, batch)
	assert.NoError(t, err)
	assert.Equal(t, int32(1), dhost.sendCalled)

	// Case 2: Local message with pid.Node equal to node's nodeID.
	pidLocal := api.PID{
		Node:   nodeID,
		Host:   "host1",
		ID:     registry.ID{NS: "ns", Name: "proc"},
		UniqID: "uniq",
	}
	err = node.Send(context.Background(), pidLocal, batch)
	assert.NoError(t, err)
	assert.Equal(t, int32(2), dhost.sendCalled)
}

func TestNodeSendHostNotFound(t *testing.T) {
	node := NewNode("node1", nil)
	pid := api.PID{
		Node:   "",
		Host:   "nonexistent",
		ID:     registry.ID{NS: "ns", Name: "proc"},
		UniqID: "uniq",
	}
	batch := &api.Batch{{Topic: "notfound"}}
	err := node.Send(context.Background(), pid, batch)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "host nonexistent not found")
}

func TestNodeSendInvalidHostType(t *testing.T) {
	node := NewNode("node1", nil)
	// Store an invalid type under a host id.
	node.hosts.Store("host1", "not a host")
	pid := api.PID{
		Node:   "",
		Host:   "host1",
		ID:     registry.ID{NS: "ns", Name: "proc"},
		UniqID: "uniq",
	}
	batch := &api.Batch{{Topic: "invalid"}}
	err := node.Send(context.Background(), pid, batch)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid type")
}

func TestNodeSendNonLocalNoUpstream(t *testing.T) {
	node := NewNode("node1", nil)
	pid := api.PID{
		Node:   "remoteNode",
		Host:   "host1",
		ID:     registry.ID{NS: "ns", Name: "proc"},
		UniqID: "uniq",
	}
	batch := &api.Batch{{Topic: "nonlocal"}}
	err := node.Send(context.Background(), pid, batch)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no upstream available")
}

func TestNodeSendNonLocalWithUpstream(t *testing.T) {
	dUp := &dummyUpstream{}
	// Upstream is provided via a pointer to an Upstream interface.
	var up api.Upstream = dUp
	node := NewNode("node1", &up)
	pid := api.PID{
		Node:   "remoteNode",
		Host:   "host1",
		ID:     registry.ID{NS: "ns", Name: "proc"},
		UniqID: "uniq",
	}
	batch := &api.Batch{{Topic: "nonlocal"}}
	err := node.Send(context.Background(), pid, batch)
	assert.NoError(t, err)
	assert.Equal(t, int32(1), dUp.sendCalled)
}

func TestNodeAttachLocal(t *testing.T) {
	dhost := &dummyHost{}
	nodeID := "node1"
	node := NewNode(nodeID, nil)
	assert.NoError(t, node.RegisterHost("host1", dhost))

	// Use a local PID.
	pidLocal := api.PID{
		Node:   "",
		Host:   "host1",
		ID:     registry.ID{NS: "ns", Name: "proc"},
		UniqID: "uniq",
	}
	ch := make(chan *api.Batch, 1)
	cancel, err := node.Attach(pidLocal, ch)
	assert.NoError(t, err)
	assert.NotNil(t, cancel)
	assert.Equal(t, int32(1), dhost.attachCalled)
}

func TestNodeAttachNonLocal(t *testing.T) {
	node := NewNode("node1", nil)
	pid := api.PID{
		Node:   "remoteNode",
		Host:   "host1",
		ID:     registry.ID{NS: "ns", Name: "proc"},
		UniqID: "uniq",
	}
	ch := make(chan *api.Batch, 1)
	cancel, err := node.Attach(pid, ch)
	assert.Error(t, err)
	assert.Nil(t, cancel)
	assert.Contains(t, err.Error(), "no upstream available")
}

func TestNodeAttachInvalidHostType(t *testing.T) {
	node := NewNode("node1", nil)
	// Store an invalid type under a host id.
	node.hosts.Store("host1", "not a host")
	pid := api.PID{
		Node:   "",
		Host:   "host1",
		ID:     registry.ID{NS: "ns", Name: "proc"},
		UniqID: "uniq",
	}
	ch := make(chan *api.Batch, 1)
	cancel, err := node.Attach(pid, ch)
	assert.Error(t, err)
	assert.Nil(t, cancel)
	assert.Contains(t, err.Error(), "invalid type")
}
