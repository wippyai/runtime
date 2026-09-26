// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"go.uber.org/zap"
)

type dialTestNode struct {
	manager  *manager
	received chan []byte
	id       cluster.NodeID
}

// startAuthenticatedPair starts two managers that trust each other through
// the pinned-key handshake and fast retry settings.
func startAuthenticatedPair(ctx context.Context, t *testing.T) (low, high dialTestNode) {
	t.Helper()
	publicLow, privateLow, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	publicHigh, privateHigh, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	sharedKey := []byte("0123456789abcdef0123456789abcdef")

	configure := func(self, peer cluster.NodeID, signing ed25519.PrivateKey, peerKey ed25519.PublicKey) ManagerConfig {
		cfg := DefaultManagerConfig()
		cfg.LocalNodeID = self
		cfg.BindAddr = "127.0.0.1"
		cfg.BindPort = 0
		cfg.Logger = zap.NewNop()
		cfg.AuthenticationKey = sharedKey
		cfg.SigningKey = signing
		cfg.ResolvePeerKey = func(id cluster.NodeID) (ed25519.PublicKey, bool) { return peerKey, id == peer }
		cfg.AuthorizePeer = func(id cluster.NodeID, _ net.Addr) bool { return id == peer }
		cfg.InitialRetryDelay = 5 * time.Millisecond
		cfg.MaxRetryDelay = 20 * time.Millisecond
		cfg.HandshakeTimeout = time.Second
		return cfg
	}

	start := func(self, peer cluster.NodeID, signing ed25519.PrivateKey, peerKey ed25519.PublicKey) dialTestNode {
		node := dialTestNode{id: self, received: make(chan []byte, 64)}
		node.manager = NewConnectionManager(configure(self, peer, signing, peerKey), nil).(*manager)
		require.NoError(t, node.manager.Start(ctx, func(from cluster.NodeID, data []byte) {
			if from == peer {
				node.received <- append([]byte(nil), data...)
			}
		}, ignoreSessionEnd))
		t.Cleanup(func() { require.NoError(t, node.manager.Stop()) })
		node.manager.AddManagedNode(peer)
		return node
	}

	low = start("node-1", "node-2", privateLow, publicHigh)
	high = start("node-2", "node-1", privateHigh, publicLow)
	return low, high
}

func closedLocalPort(t *testing.T) int {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	return port
}

func connectionOf(node dialTestNode, peer cluster.NodeID) (*NodeConnection, ConnectionState) {
	return node.manager.nodeStates.GetNodeConnection(peer)
}

func requireDelivered(t *testing.T, node dialTestNode, want string) {
	t.Helper()
	select {
	case got := <-node.received:
		require.Equal(t, want, string(got))
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not receive %q", node.id, want)
	}
}

// A peer that can dial out but cannot be dialed still forms the link: the
// node with the unreachable address keeps the connection the other side
// dialed, and its own failing dials do not disturb it.
func TestUnreachablePeerConnectsThroughItsOwnDial(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	low, high := startAuthenticatedPair(ctx, t)

	low.manager.EnsureConnection(high.id, "127.0.0.1", closedLocalPort(t))
	high.manager.EnsureConnection(low.id, "127.0.0.1", low.manager.GetListenPort())

	require.Eventually(t, func() bool {
		_, lowState := connectionOf(low, high.id)
		_, highState := connectionOf(high, low.id)
		return lowState == StateConnected && highState == StateConnected
	}, 3*time.Second, 5*time.Millisecond)

	established, _ := connectionOf(low, high.id)
	require.False(t, established.dialed, "low node holds the connection its peer dialed")

	// Outlast many redial periods of the low node against the unreachable
	// address.
	cfg := low.manager.config
	require.Never(t, func() bool {
		conn, state := connectionOf(low, high.id)
		return conn != established || state != StateConnected
	}, 12*cfg.MaxRetryDelay, 5*time.Millisecond)

	require.NoError(t, low.manager.SendToNode(high.id, []byte("low-to-high"), ClassRaftControl))
	require.NoError(t, high.manager.SendToNode(low.id, []byte("high-to-low"), ClassRaftControl))
	requireDelivered(t, high, "low-to-high")
	requireDelivered(t, low, "high-to-low")

	lowLink, ok := low.manager.Link(high.id)
	require.True(t, ok)
	require.False(t, lowLink.Dialed)
	highLink, ok := high.manager.Link(low.id)
	require.True(t, ok)
	require.True(t, highLink.Dialed)
	require.Equal(t, net.JoinHostPort("127.0.0.1", strconv.Itoa(low.manager.GetListenPort())), highLink.Remote)
	_, ok = low.manager.Link("node-3")
	require.False(t, ok)
}

// A link carries gossip only while it is connected.
func TestSendConnectedRequiresConnectedLink(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	low, high := startAuthenticatedPair(ctx, t)
	require.False(t, low.manager.SendConnected(high.id, []byte("early"), ClassGossip))

	low.manager.EnsureConnection(high.id, "127.0.0.1", high.manager.GetListenPort())
	require.Eventually(t, func() bool {
		_, state := connectionOf(low, high.id)
		return state == StateConnected
	}, 3*time.Second, 5*time.Millisecond)
	require.True(t, low.manager.SendConnected(high.id, []byte("gossip"), ClassGossip))
	requireDelivered(t, high, "gossip")
}

// When both sides dial at once, both ends settle on the connection the lower
// node dialed and keep it.
func TestSimultaneousDialsConvergeOnLowerNodeDial(t *testing.T) {
	for range 10 {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		low, high := startAuthenticatedPair(ctx, t)

		start := make(chan struct{})
		go func() { <-start; low.manager.EnsureConnection(high.id, "127.0.0.1", high.manager.GetListenPort()) }()
		go func() { <-start; high.manager.EnsureConnection(low.id, "127.0.0.1", low.manager.GetListenPort()) }()
		close(start)

		require.Eventually(t, func() bool {
			lowConn, lowState := connectionOf(low, high.id)
			highConn, highState := connectionOf(high, low.id)
			return lowState == StateConnected && highState == StateConnected &&
				lowConn.dialed && !highConn.dialed
		}, 3*time.Second, 5*time.Millisecond)

		lowConn, _ := connectionOf(low, high.id)
		highConn, _ := connectionOf(high, low.id)
		require.Never(t, func() bool {
			l, ls := connectionOf(low, high.id)
			h, hs := connectionOf(high, low.id)
			return l != lowConn || h != highConn || ls != StateConnected || hs != StateConnected
		}, 200*time.Millisecond, 5*time.Millisecond)

		require.NoError(t, low.manager.SendToNode(high.id, []byte("low-to-high"), ClassRaftControl))
		require.NoError(t, high.manager.SendToNode(low.id, []byte("high-to-low"), ClassRaftControl))
		requireDelivered(t, high, "low-to-high")
		requireDelivered(t, low, "high-to-low")
		cancel()
	}
}

// acceptFrom feeds m an inbound connection from peer and returns the peer's
// end. m holds the side the peer dialed.
func acceptFrom(t *testing.T, m *manager, peer cluster.NodeID) *NodeConnection {
	t.Helper()
	server, client := net.Pipe()
	go m.handleInboundConnection(server)
	remote, err := PerformClientHandshake(client, m.config.NodeConnectionConfig(), zap.NewNop(), peer, testIncarnation, m.config.LocalNodeID)
	require.NoError(t, err)
	t.Cleanup(remote.Close)
	return remote
}

// dialTo hands m a connection it dialed to peer and returns the peer's end.
func dialTo(t *testing.T, m *manager, peer cluster.NodeID) (*NodeConnection, *NodeConnection) {
	t.Helper()
	server, client := net.Pipe()
	accepted := make(chan *NodeConnection, 1)
	go func() {
		remote, err := PerformServerHandshake(server, m.config.NodeConnectionConfig(), zap.NewNop(), peer, testIncarnation)
		if err != nil {
			accepted <- nil
			return
		}
		accepted <- remote
	}()
	local, err := PerformClientHandshake(client, m.config.NodeConnectionConfig(), zap.NewNop(), m.config.LocalNodeID, m.incarnation, peer)
	require.NoError(t, err)
	remote := <-accepted
	require.NotNil(t, remote)
	t.Cleanup(remote.Close)
	m.sendCommand(peer, nodeCommand{Type: cmdConnected, Data: connectedData{Connection: local}})
	return local, remote
}

// requireClosedByPeer waits until m closes its end of the pipe behind remote.
func requireClosedByPeer(t *testing.T, remote *NodeConnection) {
	t.Helper()
	require.Eventually(t, func() bool {
		return errors.Is(remote.conn.SetReadDeadline(time.Time{}), io.ErrClosedPipe)
	}, 2*time.Second, time.Millisecond)
}

func TestPreferredConnectionReplacesCurrentWithoutDisconnect(t *testing.T) {
	cfg := insecureManagerConfig()
	cfg.LocalNodeID = "a-local"
	cfg.BindAddr = "127.0.0.1"
	cfg.BindPort = 0
	cfg.Logger = zap.NewNop()
	m := NewConnectionManager(cfg, nil).(*manager)
	require.NoError(t, m.Start(context.Background(), func(cluster.NodeID, []byte) {}, ignoreSessionEnd))
	defer func() { require.NoError(t, m.Stop()) }()
	const peer = "z-peer"
	m.AddManagedNode(peer)

	// The peer's dial is not preferred: the lower node is local.
	inboundRemote := acceptFrom(t, m, peer)
	require.Eventually(t, func() bool {
		_, state := m.nodeStates.GetNodeConnection(peer)
		return state == StateConnected
	}, 2*time.Second, time.Millisecond)
	inbound, _ := m.nodeStates.GetNodeConnection(peer)
	require.False(t, inbound.dialed)

	left := make(chan ConnectionState, 1)
	stop := make(chan struct{})
	watched := make(chan struct{})
	go func() {
		defer close(watched)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, state := m.nodeStates.GetNodeConnection(peer); state != StateConnected {
				select {
				case left <- state:
				default:
				}
			}
		}
	}()

	outbound, outboundRemote := dialTo(t, m, peer)
	require.Eventually(t, func() bool {
		conn, _ := m.nodeStates.GetNodeConnection(peer)
		return conn == outbound
	}, 2*time.Second, time.Millisecond)
	requireClosedByPeer(t, inboundRemote)
	close(stop)
	<-watched
	select {
	case state := <-left:
		t.Fatalf("node left CONNECTED during the swap: %s", state)
	default:
	}

	require.NoError(t, m.SendToNode(peer, []byte("after-swap"), ClassRaftControl))
	require.NoError(t, outboundRemote.conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	answerResume(t, outboundRemote, cfg.MaxMessageSize)
	f, err := readFrame(outboundRemote.conn, cfg.MaxMessageSize)
	require.NoError(t, err)
	require.Equal(t, []byte("after-swap"), f.data)

	// A further non-preferred connection is closed and changes nothing.
	lateRemote := acceptFrom(t, m, peer)
	requireClosedByPeer(t, lateRemote)

	// Reports about the replaced socket and about a failed dial leave the
	// serving connection in place.
	m.sendCommand(peer, nodeCommand{Type: cmdDisconnected, Data: disconnectedData{Connection: inbound, ShouldRetry: true}})
	m.sendCommand(peer, nodeCommand{Type: cmdDisconnected, Data: disconnectedData{ShouldRetry: true}})
	require.Never(t, func() bool {
		conn, state := m.nodeStates.GetNodeConnection(peer)
		return conn != outbound || state != StateConnected
	}, 100*time.Millisecond, time.Millisecond)
}
