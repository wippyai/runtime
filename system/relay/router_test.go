// SPDX-License-Identifier: MPL-2.0

package relay_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	relayapi "github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/system/relay"
)

// mockNode is a mock implementation of the relay.Node interface.
type mockNode struct {
	sendErr      error
	id           pid.NodeID
	sendCalled   int32
	attachCalled int32
}

func (m *mockNode) ID() pid.NodeID                                   { return m.id }
func (m *mockNode) RegisterHost(pid.HostID, relayapi.Receiver) error { return nil }
func (m *mockNode) UnregisterHost(pid.HostID)                        {}
func (m *mockNode) GetHost(pid.HostID) (relayapi.Receiver, bool)     { return nil, false }
func (m *mockNode) Send(_ *relayapi.Package) error {
	atomic.AddInt32(&m.sendCalled, 1)
	return m.sendErr
}
func (m *mockNode) Attach(pid.PID, chan *relayapi.Package) (context.CancelFunc, error) {
	atomic.AddInt32(&m.attachCalled, 1)
	return func() {}, nil
}
func (m *mockNode) Detach(pid.PID) {}

// mockReceiver is a mock implementation of the relay.Receiver interface.
type mockReceiver struct {
	sendErr    error
	sendCalled int32
}

func (m *mockReceiver) Send(_ *relayapi.Package) error {
	atomic.AddInt32(&m.sendCalled, 1)
	return m.sendErr
}

func TestRouter_Send(t *testing.T) {
	localNode := &mockNode{id: "local"}
	internode := &mockReceiver{}

	pkgToLocal := &relayapi.Package{Target: pid.PID{Node: "local", Host: "h1"}}
	pkgToLocalImplicit := &relayapi.Package{Target: pid.PID{Node: "", Host: "h1"}}
	pkgToRemote := &relayapi.Package{Target: pid.PID{Node: "remote", Host: "h2"}}

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
}

func TestRouter_PeerNodes(t *testing.T) {
	localNode := &mockNode{id: "local"}

	t.Run("RegisterPeer", func(t *testing.T) {
		router := relay.NewRouter(localNode, nil)
		peerReceiver := &mockReceiver{}

		err := router.RegisterPeer("peer1", peerReceiver)
		require.NoError(t, err)
	})

	t.Run("RegisterPeer with empty nodeID", func(t *testing.T) {
		router := relay.NewRouter(localNode, nil)
		peerReceiver := &mockReceiver{}

		err := router.RegisterPeer("", peerReceiver)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nodeID cannot be empty")
	})

	t.Run("RegisterPeer conflicts with local node", func(t *testing.T) {
		router := relay.NewRouter(localNode, nil)
		peerReceiver := &mockReceiver{}

		err := router.RegisterPeer("local", peerReceiver)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "conflicts with local node")
	})

	t.Run("RegisterPeer duplicate", func(t *testing.T) {
		router := relay.NewRouter(localNode, nil)
		peerReceiver1 := &mockReceiver{}
		peerReceiver2 := &mockReceiver{}

		err := router.RegisterPeer("peer1", peerReceiver1)
		require.NoError(t, err)

		err = router.RegisterPeer("peer1", peerReceiver2)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already registered")
	})

	t.Run("UnregisterPeer", func(t *testing.T) {
		router := relay.NewRouter(localNode, nil)
		peerReceiver := &mockReceiver{}

		err := router.RegisterPeer("peer1", peerReceiver)
		require.NoError(t, err)

		existed := router.UnregisterPeer("peer1")
		assert.True(t, existed)
	})

	t.Run("UnregisterPeer not found", func(t *testing.T) {
		router := relay.NewRouter(localNode, nil)

		existed := router.UnregisterPeer("nonexistent")
		assert.False(t, existed)
	})

	t.Run("Send to peer node", func(t *testing.T) {
		localNode.sendCalled = 0
		router := relay.NewRouter(localNode, nil)
		peerReceiver := &mockReceiver{}

		err := router.RegisterPeer("peer1", peerReceiver)
		require.NoError(t, err)

		pkgToPeer := &relayapi.Package{Target: pid.PID{Node: "peer1", Host: "queue", UniqID: "wf-123"}}
		err = router.Send(pkgToPeer)
		require.NoError(t, err)

		assert.Equal(t, int32(0), localNode.sendCalled, "localNode.Send should not be called")
		assert.Equal(t, int32(1), peerReceiver.sendCalled, "peerReceiver.Send should be called")
	})

	t.Run("Send to unregistered peer node", func(t *testing.T) {
		localNode.sendCalled = 0
		router := relay.NewRouter(localNode, nil)

		pkgToPeer := &relayapi.Package{Target: pid.PID{Node: "nonexistent", Host: "queue", UniqID: "wf-123"}}
		err := router.Send(pkgToPeer)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("Peer node takes priority over internode", func(t *testing.T) {
		router := relay.NewRouter(localNode, &mockReceiver{})
		peerReceiver := &mockReceiver{}

		err := router.RegisterPeer("peer1", peerReceiver)
		require.NoError(t, err)

		pkgToPeer := &relayapi.Package{Target: pid.PID{Node: "peer1", Host: "queue", UniqID: "wf-123"}}
		err = router.Send(pkgToPeer)
		require.NoError(t, err)

		assert.Equal(t, int32(1), peerReceiver.sendCalled, "peerReceiver.Send should be called")
	})

	t.Run("Propagate error from peer node", func(t *testing.T) {
		router := relay.NewRouter(localNode, nil)
		errToSend := errors.New("peer send failed")
		peerReceiver := &mockReceiver{sendErr: errToSend}

		err := router.RegisterPeer("peer1", peerReceiver)
		require.NoError(t, err)

		pkgToPeer := &relayapi.Package{Target: pid.PID{Node: "peer1", Host: "queue", UniqID: "wf-123"}}
		err = router.Send(pkgToPeer)
		require.Error(t, err)
		assert.Equal(t, errToSend, err)
	})
}
