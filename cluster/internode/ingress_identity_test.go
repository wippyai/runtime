// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

type authenticatingMockManager struct {
	*mockConnectionManager
	authenticated bool
	protected     bool
}

func (m authenticatingMockManager) AuthenticatesPeers() bool { return m.authenticated }

func (m authenticatingMockManager) ProtectsPayloads() bool { return m.protected }

type forgedIngressCodec struct{ mockCodec }

func (c *forgedIngressCodec) Decode(data []byte) (*relay.Package, error) {
	pkg, err := c.mockCodec.Decode(data)
	if err == nil {
		pkg.Ingress = relay.IngressIdentity{Node: "forged-authority", Authenticated: true, IntegrityProtected: true}
	}
	return pkg, err
}

func TestServiceIngressIdentityComesFromConnection(t *testing.T) {
	for _, mode := range []string{"authenticated", "unauthenticated", "unknown_policy", "empty_peer", "tls_authenticated", "tls_unauthenticated"} {
		t.Run(mode, func(t *testing.T) {
			manager := newMockConnectionManager()
			var transport ConnectionManager = manager
			if mode != "unknown_policy" {
				transport = authenticatingMockManager{mockConnectionManager: manager, authenticated: mode != "unauthenticated" && mode != "tls_unauthenticated", protected: mode == "tls_authenticated" || mode == "tls_unauthenticated"}
			}
			peer := cluster.NodeID("actual-peer")
			if mode == "empty_peer" {
				peer = ""
			}
			called := false
			svc := NewService(zap.NewNop(), transport, &forgedIngressCodec{}, func(pkg *relay.Package) error {
				defer relay.ReleasePackage(pkg)
				called = true
				require.Equal(t, peer, pkg.Ingress.Node)
				require.Equal(t, mode == "authenticated" || mode == "tls_authenticated", pkg.Ingress.Authenticated)
				require.Equal(t, mode == "tls_authenticated" || mode == "tls_unauthenticated", pkg.Ingress.IntegrityProtected)
				require.Equal(t, pid.NodeID("remote-node"), pkg.Source.Node, "transitive source semantics remain unchanged")
				return nil
			}, eventbus.NewBus(), &mockMembership{localNode: cluster.NodeInfo{ID: "local"}})
			require.NoError(t, svc.Start(t.Context()))
			defer func() { require.NoError(t, svc.Stop()) }()
			manager.onMessage(peer, []byte("frame"))
			require.True(t, called)
		})
	}
}

func TestMessageCodecNeverTransfersIngressIdentity(t *testing.T) {
	codec := NewMessageCodec(&mockTranscoder{})
	pkg := relay.NewServicePackage("sender", "host", "receiver", "host", "topic")
	defer relay.ReleasePackage(pkg)
	pkg.Ingress = relay.IngressIdentity{Node: "upstream-authority", Authenticated: true, IntegrityProtected: true}
	data, err := codec.Encode(pkg)
	require.NoError(t, err)
	decoded, err := codec.Decode(data)
	require.NoError(t, err)
	defer relay.ReleasePackage(decoded)
	require.Equal(t, relay.IngressIdentity{}, decoded.Ingress)
	require.Equal(t, pkg.Source, decoded.Source)
}
