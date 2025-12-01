package relay_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	api "github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/system/relay"
)

// mockNode is a mock implementation of the relay.Node interface.
type mockNode struct {
	id           api.NodeID
	sendCalled   int32
	attachCalled int32
	sendErr      error
}

func (m *mockNode) ID() api.NodeID                          { return m.id }
func (m *mockNode) RegisterHost(api.HostID, api.Host) error { return nil }
func (m *mockNode) UnregisterHost(api.HostID)               {}
func (m *mockNode) GetHost(api.HostID) (api.Host, bool)     { return nil, false }
func (m *mockNode) Send(_ *api.Package) error {
	atomic.AddInt32(&m.sendCalled, 1)
	return m.sendErr
}
func (m *mockNode) Attach(api.PID, chan *api.Package) (context.CancelFunc, error) {
	atomic.AddInt32(&m.attachCalled, 1)
	return func() {}, nil
}
func (m *mockNode) Detach(api.PID) {}

// mockReceiver is a mock implementation of the relay.Receiver interface.
type mockReceiver struct {
	sendCalled int32
	sendErr    error
}

func (m *mockReceiver) Send(_ *api.Package) error {
	atomic.AddInt32(&m.sendCalled, 1)
	return m.sendErr
}

func TestRouter_Send(t *testing.T) {
	localNode := &mockNode{id: "local"}
	internode := &mockReceiver{}

	pkgToLocal := &api.Package{Target: api.PID{Node: "local", Host: "h1"}}
	pkgToLocalImplicit := &api.Package{Target: api.PID{Node: "", Host: "h1"}}
	pkgToRemote := &api.Package{Target: api.PID{Node: "remote", Host: "h2"}}

	t.Run("Route to local node", func(t *testing.T) {
		localNode.sendCalled = 0
		internode.sendCalled = 0
		router := relay.NewRouter(localNode, internode)

		err := router.Send(pkgToLocal)
		require.NoError(t, err)

		assert.Equal(t, int32(1), localNode.sendCalled, "localNode.Send should be called")
		assert.Equal(t, int32(0), internode.sendCalled, "internode.Send should not be called")
	})

	t.Run("Route to local node (implicit)", func(t *testing.T) {
		localNode.sendCalled = 0
		internode.sendCalled = 0
		router := relay.NewRouter(localNode, internode)

		err := router.Send(pkgToLocalImplicit)
		require.NoError(t, err)

		assert.Equal(t, int32(1), localNode.sendCalled, "localNode.Send should be called")
		assert.Equal(t, int32(0), internode.sendCalled, "internode.Send should not be called")
	})

	t.Run("Route to remote node with internode", func(t *testing.T) {
		localNode.sendCalled = 0
		internode.sendCalled = 0
		router := relay.NewRouter(localNode, internode)

		err := router.Send(pkgToRemote)
		require.NoError(t, err)

		assert.Equal(t, int32(0), localNode.sendCalled, "localNode.Send should not be called")
		assert.Equal(t, int32(1), internode.sendCalled, "internode.Send should be called")
	})

	t.Run("Error when routing to remote node without internode", func(t *testing.T) {
		localNode.sendCalled = 0
		router := relay.NewRouter(localNode, nil) // No internode receiver

		err := router.Send(pkgToRemote)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.Equal(t, int32(0), localNode.sendCalled, "localNode.Send should not be called")
	})

	t.Run("Error on nil package", func(t *testing.T) {
		router := relay.NewRouter(localNode, internode)
		err := router.Send(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot send nil package")
	})

	t.Run("Propagate error from local node", func(t *testing.T) {
		errToSend := errors.New("local send failed")
		errNode := &mockNode{id: "local", sendErr: errToSend}
		router := relay.NewRouter(errNode, internode)

		err := router.Send(pkgToLocal)
		require.Error(t, err)
		assert.Equal(t, errToSend, err)
	})

	t.Run("Propagate error from internode", func(t *testing.T) {
		errToSend := errors.New("internode send failed")
		errInternode := &mockReceiver{sendErr: errToSend}
		router := relay.NewRouter(localNode, errInternode)

		err := router.Send(pkgToRemote)
		require.Error(t, err)
		assert.Equal(t, errToSend, err)
	})
}

func TestRouter_VirtualNodes(t *testing.T) {
	localNode := &mockNode{id: "local"}

	t.Run("RegisterVirtualNode", func(t *testing.T) {
		router := relay.NewRouter(localNode, nil)
		virtualReceiver := &mockReceiver{}

		err := router.RegisterVirtualNode("virtual1", virtualReceiver)
		require.NoError(t, err)
	})

	t.Run("RegisterVirtualNode with empty nodeID", func(t *testing.T) {
		router := relay.NewRouter(localNode, nil)
		virtualReceiver := &mockReceiver{}

		err := router.RegisterVirtualNode("", virtualReceiver)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nodeID cannot be empty")
	})

	t.Run("RegisterVirtualNode conflicts with local node", func(t *testing.T) {
		router := relay.NewRouter(localNode, nil)
		virtualReceiver := &mockReceiver{}

		err := router.RegisterVirtualNode("local", virtualReceiver)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "conflicts with local node")
	})

	t.Run("RegisterVirtualNode duplicate", func(t *testing.T) {
		router := relay.NewRouter(localNode, nil)
		virtualReceiver1 := &mockReceiver{}
		virtualReceiver2 := &mockReceiver{}

		err := router.RegisterVirtualNode("virtual1", virtualReceiver1)
		require.NoError(t, err)

		err = router.RegisterVirtualNode("virtual1", virtualReceiver2)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already registered")
	})

	t.Run("UnregisterVirtualNode", func(t *testing.T) {
		router := relay.NewRouter(localNode, nil)
		virtualReceiver := &mockReceiver{}

		err := router.RegisterVirtualNode("virtual1", virtualReceiver)
		require.NoError(t, err)

		existed := router.UnregisterVirtualNode("virtual1")
		assert.True(t, existed)
	})

	t.Run("UnregisterVirtualNode not found", func(t *testing.T) {
		router := relay.NewRouter(localNode, nil)

		existed := router.UnregisterVirtualNode("nonexistent")
		assert.False(t, existed)
	})

	t.Run("Send to virtual node", func(t *testing.T) {
		localNode.sendCalled = 0
		router := relay.NewRouter(localNode, nil)
		virtualReceiver := &mockReceiver{}

		err := router.RegisterVirtualNode("virtual1", virtualReceiver)
		require.NoError(t, err)

		pkgToVirtual := &api.Package{Target: api.PID{Node: "virtual1", Host: "queue", UniqID: "wf-123"}}
		err = router.Send(pkgToVirtual)
		require.NoError(t, err)

		assert.Equal(t, int32(0), localNode.sendCalled, "localNode.Send should not be called")
		assert.Equal(t, int32(1), virtualReceiver.sendCalled, "virtualReceiver.Send should be called")
	})

	t.Run("Send to unregistered virtual node", func(t *testing.T) {
		localNode.sendCalled = 0
		router := relay.NewRouter(localNode, nil)

		pkgToVirtual := &api.Package{Target: api.PID{Node: "nonexistent", Host: "queue", UniqID: "wf-123"}}
		err := router.Send(pkgToVirtual)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("Virtual node takes priority over internode", func(t *testing.T) {
		router := relay.NewRouter(localNode, &mockReceiver{})
		virtualReceiver := &mockReceiver{}

		err := router.RegisterVirtualNode("virtual1", virtualReceiver)
		require.NoError(t, err)

		pkgToVirtual := &api.Package{Target: api.PID{Node: "virtual1", Host: "queue", UniqID: "wf-123"}}
		err = router.Send(pkgToVirtual)
		require.NoError(t, err)

		assert.Equal(t, int32(1), virtualReceiver.sendCalled, "virtualReceiver.Send should be called")
	})

	t.Run("Propagate error from virtual node", func(t *testing.T) {
		router := relay.NewRouter(localNode, nil)
		errToSend := errors.New("virtual send failed")
		virtualReceiver := &mockReceiver{sendErr: errToSend}

		err := router.RegisterVirtualNode("virtual1", virtualReceiver)
		require.NoError(t, err)

		pkgToVirtual := &api.Package{Target: api.PID{Node: "virtual1", Host: "queue", UniqID: "wf-123"}}
		err = router.Send(pkgToVirtual)
		require.Error(t, err)
		assert.Equal(t, errToSend, err)
	})
}
