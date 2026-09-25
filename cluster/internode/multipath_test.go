// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"go.uber.org/zap"
)

func TestCandidatesFilterOrderAndVersion(t *testing.T) {
	in := []Candidate{
		{Host: "127.0.0.1", Port: 9000},
		{Host: "fe80::1", Port: 9000},
		{Host: "100.70.0.69", Port: 9000},
		{Host: "fd7a:115c:a1e0::1", Port: 9000},
		{Host: "100.70.0.69", Port: 9000},
		{Host: "bee.tailnet.ts.net", Port: 9000},
	}
	got := NormalizeCandidates(in)
	require.Len(t, got, 3)
	require.Equal(t, "tailnet", got[0].Scope)
	require.Equal(t, "ipv4", got[0].Family)
	require.Equal(t, "ipv6", got[1].Family)
	require.Equal(t, "dns", got[2].Family)
	ordered := OrderCandidates(got, Candidate{Host: "bee.tailnet.ts.net", Port: 9000})
	require.Equal(t, "bee.tailnet.ts.net", ordered[0].Host)
	encoded, err := EncodeCandidates(got)
	require.NoError(t, err)
	require.LessOrEqual(t, len(encoded), 320)
	decoded, err := DecodeCandidates(encoded)
	require.NoError(t, err)
	require.Equal(t, got, decoded)
	_, err = DecodeCandidates(`{"v":4,"c":[]}`)
	require.Error(t, err)
}

func TestCandidateMetadataFitsAlongsideIdentityAndGossip(t *testing.T) {
	meta := map[string]string{
		MetadataPublicKey: "1234567890123456789012345678901234567890123",
		MetadataPort:      "7950", "raft_eligible": "true", "raft_priority": "100",
		"failure_domain": "", "version": "1.0.0", "role": "wippy",
	}
	internode := []Candidate{{Host: "100.70.0.69", Port: 7950}, {Host: "192.168.1.20", Port: 7950}}
	encoded, err := EncodeCandidatesForMeta(meta, internode)
	require.NoError(t, err)
	require.NotEmpty(t, encoded)
	meta[MetadataCandidatesV3] = encoded
	gossip := []Candidate{{Host: "100.70.0.69", Port: 7946}, {Host: "192.168.1.20", Port: 7946}}
	encoded, err = EncodeCandidatesForMetaKey(meta, MetadataGossipCandidatesV3, gossip)
	require.NoError(t, err)
	require.NotEmpty(t, encoded)
	meta[MetadataGossipCandidatesV3] = encoded
	data, err := json.Marshal(meta)
	require.NoError(t, err)
	require.LessOrEqual(t, len(data), 512)
}

func TestConfiguredCandidatesAcceptIPv6AndMagicDNS(t *testing.T) {
	got, err := ParseCandidateList("[fd7a:115c:a1e0::1]:9001,bee.tailnet.ts.net,127.0.0.1", 9000)
	require.Error(t, err, "loopback must not be advertised")
	got, err = ParseCandidateList("[fd7a:115c:a1e0::1]:9001,bee.tailnet.ts.net", 9000)
	require.NoError(t, err)
	require.Equal(t, "ipv6", got[0].Family)
	require.Equal(t, 9001, got[0].Port)
	require.Equal(t, "tailnet", got[1].Scope)
}

type multipathMock struct {
	*mockConnectionManager
	candidates []Candidate
}

func (m *multipathMock) EnsureCandidates(_ cluster.NodeID, candidates []Candidate) {
	m.candidates = append([]Candidate(nil), candidates...)
}
func (m *multipathMock) RequestReverseConnect(cluster.NodeID, []Candidate) error { return nil }
func (m *multipathMock) PeerStatus(cluster.NodeID) (PeerPathStatus, bool) {
	return PeerPathStatus{}, false
}

func TestServiceConsumesV3AndPreservesLegacyEndpoint(t *testing.T) {
	svc, _, _, _, ctx, cancel := setupService(t)
	defer cancel()
	multi := &multipathMock{mockConnectionManager: newMockConnectionManager()}
	svc.connMan = multi
	require.NoError(t, svc.Start(ctx))
	defer svc.Stop()
	multi.AddManagedNode("peer")
	encoded, err := EncodeCandidates([]Candidate{{Host: "100.70.0.69", Port: 9001}})
	require.NoError(t, err)
	svc.connectToNode(cluster.NodeInfo{ID: "peer", Addr: "192.168.1.10:7946", Meta: cluster.NodeMeta{
		MetadataPort: "9000", MetadataCandidatesV3: encoded,
	}})
	require.Len(t, multi.candidates, 2)
	require.Equal(t, "100.70.0.69:9001", multi.candidates[0].Address())
	require.Equal(t, "192.168.1.10:9000", multi.candidates[1].Address())
}

func authenticatedMultipathManagers(t *testing.T) (*manager, *manager) {
	t.Helper()
	pubA, keyA, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	pubB, keyB, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	newManager := func(id string, key ed25519.PrivateKey, peer string, peerKey ed25519.PublicKey) *manager {
		cfg := DefaultManagerConfig()
		cfg.Logger = zap.NewNop()
		cfg.LocalNodeID = id
		cfg.BindAddr = "127.0.0.1"
		cfg.BindPort = 0
		cfg.AutoPort = true
		cfg.AuthenticationKey = []byte("multipath-test-secret")
		cfg.SigningKey = key
		cfg.ResolvePeerKey = func(id cluster.NodeID) (ed25519.PublicKey, bool) { return peerKey, id == peer }
		cfg.AuthorizePeer = func(id cluster.NodeID, _ net.Addr) bool { return id == peer }
		cfg.HandshakeTimeout = 200 * time.Millisecond
		cfg.InitialRetryDelay = 10 * time.Millisecond
		cfg.MaxRetryDelay = 40 * time.Millisecond
		return NewConnectionManager(cfg, nil).(*manager)
	}
	return newManager("a", keyA, "b", pubB), newManager("b", keyB, "a", pubA)
}

func TestMultipathSelectsVerifiedListenerAndReportsPath(t *testing.T) {
	a, b := authenticatedMultipathManagers(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	require.NoError(t, a.Start(ctx, func(cluster.NodeID, []byte) {}))
	defer a.Stop()
	require.NoError(t, b.Start(ctx, func(cluster.NodeID, []byte) {}))
	defer b.Stop()
	a.AddManagedNode("b")
	b.AddManagedNode("a")
	// The first endpoint is unreachable. The second is the live listener.
	a.EnsureCandidates("b", []Candidate{{Host: "192.0.2.1", Port: 1}, {Host: "127.0.0.1", Port: b.GetListenPort()}})
	require.Eventually(t, func() bool { return len(a.ConnectedNodes()) == 1 }, 2*time.Second, 10*time.Millisecond)
	status, ok := a.PeerStatus("b")
	require.True(t, ok)
	require.Equal(t, "CONNECTED", status.State)
	require.Equal(t, "127.0.0.1", status.Path.Host)
	require.Equal(t, b.GetListenPort(), status.Path.Port)
	require.Len(t, status.Candidates, 2)
	state := a.nodeStates.GetNodeState("b")
	require.Equal(t, b.GetListenPort(), a.nodeStates.candidatesForState("b", state)[0].Port,
		"the identity-verified winner must lead the next reconnect race")
	require.Equal(t, status.RemoteAddress, status.Observed.Address())
}

func TestMultipathReconnectKeepsCandidatePool(t *testing.T) {
	a, b := authenticatedMultipathManagers(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	require.NoError(t, a.Start(ctx, func(cluster.NodeID, []byte) {}))
	defer a.Stop()
	require.NoError(t, b.Start(ctx, func(cluster.NodeID, []byte) {}))
	defer b.Stop()
	a.AddManagedNode("b")
	b.AddManagedNode("a")
	a.EnsureCandidates("b", []Candidate{{Host: "192.0.2.1", Port: 1}, {Host: "127.0.0.1", Port: b.GetListenPort()}})
	require.Eventually(t, func() bool { return len(a.ConnectedNodes()) == 1 }, 2*time.Second, 10*time.Millisecond)
	before, _ := a.nodeStates.GetNodeConnection("b")
	require.NotNil(t, before)
	// Close the socket as a network failure, leaving the managed peer and
	// advertised pool in place for the control loop's backoff reconnect.
	require.NoError(t, before.conn.Close())
	require.Eventually(t, func() bool {
		after, state := a.nodeStates.GetNodeConnection("b")
		return state == StateConnected && after != nil && after != before
	}, 3*time.Second, 10*time.Millisecond)
	status, ok := a.PeerStatus("b")
	require.True(t, ok)
	require.Equal(t, b.GetListenPort(), status.Path.Port)
	require.Len(t, status.Candidates, 2)
}

func TestReverseConnectUsesAuthenticatedInboundPath(t *testing.T) {
	a, b := authenticatedMultipathManagers(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	require.NoError(t, a.Start(ctx, func(cluster.NodeID, []byte) {}))
	defer a.Stop()
	require.NoError(t, b.Start(ctx, func(cluster.NodeID, []byte) {}))
	defer b.Stop()
	a.AddManagedNode("b")
	b.AddManagedNode("a")
	// A cannot reach B's advertised address. A rendezvous hands B a verified
	// request with A's candidate, so B must initiate despite node-ID ordering.
	a.EnsureCandidates("b", []Candidate{{Host: "192.0.2.1", Port: 1}})
	require.NoError(t, b.RequestReverseConnect("a", []Candidate{{Host: "127.0.0.1", Port: a.GetListenPort()}}))
	require.Eventually(t, func() bool { return len(a.ConnectedNodes()) == 1 && len(b.ConnectedNodes()) == 1 }, 2*time.Second, 10*time.Millisecond)
	status, ok := b.PeerStatus("a")
	require.True(t, ok)
	require.Equal(t, "outbound", status.Direction)
	require.Equal(t, a.GetListenPort(), status.Path.Port)
	inbound, ok := a.PeerStatus("b")
	require.True(t, ok)
	require.Equal(t, "inbound", inbound.Direction)
	require.Equal(t, inbound.RemoteAddress, inbound.Observed.Address())
}

func TestMultipathTLSRejectsWrongNodeBeforeSelectingPath(t *testing.T) {
	a, b := authenticatedMultipathManagers(t)
	aTLS, bTLS := managerTestTLSConfigs(t)
	a.config.TLS, b.config.TLS = aTLS, bTLS
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	require.NoError(t, a.Start(ctx, func(cluster.NodeID, []byte) {}))
	defer a.Stop()
	require.NoError(t, b.Start(ctx, func(cluster.NodeID, []byte) {}))
	defer b.Stop()
	a.AddManagedNode("b")
	b.AddManagedNode("a")

	serverTLS, err := loadTLSConfig(bTLS)
	require.NoError(t, err)
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	wrong := tls.NewListener(raw, serverTLS)
	defer wrong.Close()
	accepted := make(chan struct{})
	go func() {
		conn, err := wrong.Accept()
		if err != nil {
			return
		}
		close(accepted)
		_, _ = PerformServerHandshake(conn, b.config.NodeConnectionConfig(), zap.NewNop(), "wrong-node")
	}()
	a.EnsureCandidates("b", []Candidate{
		{Host: "127.0.0.1", Port: raw.Addr().(*net.TCPAddr).Port},
		{Host: "127.0.0.1", Port: b.GetListenPort()},
	})
	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Fatal("wrong TLS endpoint was not attempted")
	}
	require.Eventually(t, func() bool { return len(a.ConnectedNodes()) == 1 }, 2*time.Second, 10*time.Millisecond)
	status, ok := a.PeerStatus("b")
	require.True(t, ok)
	require.Equal(t, b.GetListenPort(), status.Path.Port)
}
