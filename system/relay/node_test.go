package relay

import (
	"context"
	"sync/atomic"
	"testing"

	api "github.com/wippyai/runtime/api/relay"

	"github.com/stretchr/testify/assert"
)

// dummyHost is a stub that implements the Host interface.
type dummyHost struct {
	sendCalled   int32
	attachCalled int32
}

func (d *dummyHost) Send(_ *api.Package) error {
	atomic.AddInt32(&d.sendCalled, 1)
	return nil
}

func (d *dummyHost) Attach(_ api.PID, _ chan *api.Package) (context.CancelFunc, error) {
	atomic.AddInt32(&d.attachCalled, 1)
	cancel := func() {}
	return cancel, nil
}

func (d *dummyHost) Detach(_ api.PID) {
	// No-op for testing
}

func TestNodeSendLocal(t *testing.T) {
	// Create a dummy host and register it with the node.
	dhost := &dummyHost{}
	nodeID := "node1"
	node := NewNode(nodeID)
	assert.NoError(t, node.RegisterHost("host1", dhost))

	// Case 1: Local message with empty pid.Node.
	pidLocalEmpty := api.PID{
		Node:   "",
		Host:   "host1",
		UniqID: "uniq",
	}
	pkg := &api.Package{
		Target: pidLocalEmpty,
		Messages: []*api.Message{
			{Topic: "local"},
		},
	}
	err := node.Send(pkg)
	assert.NoError(t, err)
	assert.Equal(t, int32(1), dhost.sendCalled)

	// Case 2: Local message with pid.Node equal to node's nodeID.
	pidLocal := api.PID{
		Node:   nodeID,
		Host:   "host1",
		UniqID: "uniq",
	}
	pkg.Target = pidLocal
	err = node.Send(pkg)
	assert.NoError(t, err)
	assert.Equal(t, int32(2), dhost.sendCalled)
}

func TestNodeSendHostNotFound(t *testing.T) {
	node := NewNode("node1")
	pid := api.PID{
		Node:   "",
		Host:   "nonexistent",
		UniqID: "uniq",
	}
	pkg := &api.Package{
		Target: pid,
		Messages: []*api.Message{
			{Topic: "notfound"},
		},
	}
	err := node.Send(pkg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "host nonexistent not found")
}

func TestNodeSendInvalidHostType(t *testing.T) {
	node := NewNode("node1")
	// Store an invalid type under a host id.
	node.hosts.Store("host1", "not a host")
	pid := api.PID{
		Node:   "",
		Host:   "host1",
		UniqID: "uniq",
	}
	pkg := &api.Package{
		Target: pid,
		Messages: []*api.Message{
			{Topic: "invalid"},
		},
	}
	err := node.Send(pkg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid type")
}

func TestNodeSendNonLocalNoUpstream(t *testing.T) {
	node := NewNode("node1")
	pid := api.PID{
		Node:   "remoteNode",
		Host:   "host1",
		UniqID: "uniq",
	}
	pkg := &api.Package{
		Target: pid,
		Messages: []*api.Message{
			{Topic: "nonlocal"},
		},
	}
	err := node.Send(pkg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot route to external node remoteNode")
}

func TestNodeAttachLocal(t *testing.T) {
	dhost := &dummyHost{}
	nodeID := "node1"
	node := NewNode(nodeID)
	assert.NoError(t, node.RegisterHost("host1", dhost))

	// Use a local pid.
	pidLocal := api.PID{
		Node:   "",
		Host:   "host1",
		UniqID: "uniq",
	}
	ch := make(chan *api.Package, 1)
	cancel, err := node.Attach(pidLocal, ch)
	assert.NoError(t, err)
	assert.NotNil(t, cancel)
	assert.Equal(t, int32(1), dhost.attachCalled)
}

func TestNodeAttachNonLocal(t *testing.T) {
	node := NewNode("node1")
	pid := api.PID{
		Node:   "remoteNode",
		Host:   "host1",
		UniqID: "uniq",
	}
	ch := make(chan *api.Package, 1)
	cancel, err := node.Attach(pid, ch)
	assert.Error(t, err)
	assert.Nil(t, cancel)
	assert.Contains(t, err.Error(), "cannot attach to external node remoteNode")
}

func TestNodeAttachInvalidHostType(t *testing.T) {
	node := NewNode("node1")
	// Store an invalid type under a host id.
	node.hosts.Store("host1", "not a host")
	pid := api.PID{
		Node:   "",
		Host:   "host1",
		UniqID: "uniq",
	}
	ch := make(chan *api.Package, 1)
	cancel, err := node.Attach(pid, ch)
	assert.Error(t, err)
	assert.Nil(t, cancel)
	assert.Contains(t, err.Error(), "invalid type")
}

func TestNodeDetach(t *testing.T) {
	dhost := &dummyHost{}
	nodeID := "node1"
	node := NewNode(nodeID)
	assert.NoError(t, node.RegisterHost("host1", dhost))

	// Test detach with local pid
	pidLocal := api.PID{
		Node:   "",
		Host:   "host1",
		UniqID: "uniq",
	}
	node.Detach(pidLocal) // Should not panic

	// Test detach with non-local pid
	pidNonLocal := api.PID{
		Node:   "remoteNode",
		Host:   "host1",
		UniqID: "uniq",
	}
	node.Detach(pidNonLocal) // Should not panic

	// Test detach with invalid host
	pidInvalidHost := api.PID{
		Node:   "",
		Host:   "nonexistent",
		UniqID: "uniq",
	}
	node.Detach(pidInvalidHost) // Should not panic
}

func TestNodeRegisterHostDuplicate(t *testing.T) {
	node := NewNode("node1")
	dhost := &dummyHost{}

	// First registration should succeed
	err := node.RegisterHost("host1", dhost)
	assert.NoError(t, err)

	// Second registration should fail
	err = node.RegisterHost("host1", dhost)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestNodeRegisterHostInvalidType(t *testing.T) {
	node := NewNode("node1")

	// Store an invalid type directly in the hosts map
	node.hosts.Store("host1", "not a host")

	// Try to use the invalid host
	pid := api.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "test",
	}

	// Try to attach to the invalid host
	ch := make(chan *api.Package)
	_, err := node.Attach(pid, ch)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid type")

	// Try to send to the invalid host
	pkg := &api.Package{
		Target: pid,
		Messages: []*api.Message{
			{Topic: "test"},
		},
	}
	err = node.Send(pkg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid type")
}

func TestNodeUnregisterHostNonExistent(_ *testing.T) {
	node := NewNode("node1")

	// Unregister a non-existent host should not panic
	node.UnregisterHost("nonexistent")
}

func TestNodeUnregisterHostInvalidType(_ *testing.T) {
	node := NewNode("node1")

	// Store an invalid type
	node.hosts.Store("host1", "not a host")

	// Unregister should not panic
	node.UnregisterHost("host1")
}
