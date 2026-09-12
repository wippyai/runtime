// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/cluster/internode"
	"github.com/wippyai/runtime/system/eventbus"
	systempayload "github.com/wippyai/runtime/system/payload"
	localrelay "github.com/wippyai/runtime/system/relay"
	"go.uber.org/zap"
)

type participantNativeMembership struct {
	local cluster.NodeInfo
}

func (m *participantNativeMembership) Nodes() []cluster.NodeInfo   { return []cluster.NodeInfo{m.local} }
func (m *participantNativeMembership) LocalNode() cluster.NodeInfo { return m.local }
func (m *participantNativeMembership) UpdateMeta(meta map[string]string) {
	if m.local.Meta == nil {
		m.local.Meta = make(cluster.NodeMeta)
	}
	for key, value := range meta {
		m.local.Meta[key] = value
	}
}

// participantNativeObserver records the peer stamped by internode.Service
// before routing the package to the local participant host.
type participantNativeObserver struct {
	host     relay.ContextSender
	received chan pid.NodeID
}

func (o *participantNativeObserver) Send(pkg *relay.Package) error {
	return o.SendContext(context.Background(), pkg)
}

func (o *participantNativeObserver) SendContext(ctx context.Context, pkg *relay.Package) error {
	select {
	case o.received <- pkg.ReceivedFrom:
	default:
	}
	return o.host.SendContext(ctx, pkg)
}

func participantNativeTLSConfigs(t *testing.T) (internode.ManagerTLSConfig, internode.ManagerTLSConfig) {
	t.Helper()
	rootPublic, rootPrivate, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	now := time.Now()
	root := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, root, root, rootPublic, rootPrivate)
	require.NoError(t, err)
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER}), 0600))

	leaf := func(name string, serial int64) internode.ManagerTLSConfig {
		public, private, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		certificate := &x509.Certificate{
			SerialNumber: big.NewInt(serial),
			NotBefore:    now.Add(-time.Minute),
			NotAfter:     now.Add(time.Hour),
			KeyUsage:     x509.KeyUsageDigitalSignature,
			ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
			IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		}
		leafDER, err := x509.CreateCertificate(rand.Reader, certificate, root, public, rootPrivate)
		require.NoError(t, err)
		keyDER, err := x509.MarshalPKCS8PrivateKey(private)
		require.NoError(t, err)
		cfg := internode.ManagerTLSConfig{
			Enabled:  true,
			CAFile:   caPath,
			CertFile: filepath.Join(dir, name+".pem"),
			KeyFile:  filepath.Join(dir, name+".key"),
		}
		require.NoError(t, os.WriteFile(cfg.CertFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}), 0600))
		require.NoError(t, os.WriteFile(cfg.KeyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0600))
		return cfg
	}
	return leaf("authority", 2), leaf("client", 3)
}

func TestParticipantNativeTLSLoopbackSnapshot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, authorityEngine := newParticipantTestInventory(t, 4)
	_, clientEngine := newParticipantTestInventory(t, 4)

	authorityNode := localrelay.NewNode("authority")
	clientNode := localrelay.NewNode("client")
	authorityBus := eventbus.NewBus()
	clientBus := eventbus.NewBus()
	authorityMembership := &participantNativeMembership{local: cluster.NodeInfo{ID: "authority", Addr: "127.0.0.1"}}
	clientMembership := &participantNativeMembership{local: cluster.NodeInfo{ID: "client", Addr: "127.0.0.1"}}
	authorityTLS, clientTLS := participantNativeTLSConfigs(t)
	authorityPublic, authoritySigning, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	clientPublic, clientSigning, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	sharedKey := []byte("participant-native-shared-authentication-key")

	authorityConfig := internode.DefaultManagerConfig()
	authorityConfig.Logger = zap.NewNop()
	authorityConfig.LocalNodeID = "authority"
	authorityConfig.BindAddr = "127.0.0.1"
	authorityConfig.AutoPort = true
	authorityConfig.TLS = authorityTLS
	authorityConfig.AuthenticationKey = sharedKey
	authorityConfig.SigningKey = authoritySigning
	authorityConfig.RequireAuthentication = true
	authorityConfig.InitialRetryDelay = 5 * time.Millisecond
	authorityConfig.MaxRetryDelay = 50 * time.Millisecond
	authorityConfig.ResolvePeerKey = func(nodeID cluster.NodeID) (ed25519.PublicKey, bool) {
		return clientPublic, nodeID == "client"
	}
	authorityConfig.AuthorizePeer = func(nodeID cluster.NodeID, _ net.Addr) bool { return nodeID == "client" }

	clientConfig := internode.DefaultManagerConfig()
	clientConfig.Logger = zap.NewNop()
	clientConfig.LocalNodeID = "client"
	clientConfig.BindAddr = "127.0.0.1"
	clientConfig.AutoPort = true
	clientConfig.TLS = clientTLS
	clientConfig.AuthenticationKey = sharedKey
	clientConfig.SigningKey = clientSigning
	clientConfig.RequireAuthentication = true
	clientConfig.InitialRetryDelay = 5 * time.Millisecond
	clientConfig.MaxRetryDelay = 50 * time.Millisecond
	clientConfig.ResolvePeerKey = func(nodeID cluster.NodeID) (ed25519.PublicKey, bool) {
		return authorityPublic, nodeID == "authority"
	}
	clientConfig.AuthorizePeer = func(nodeID cluster.NodeID, _ net.Addr) bool { return nodeID == "authority" }

	authorityManager := internode.NewConnectionManager(authorityConfig, nil)
	clientManager := internode.NewConnectionManager(clientConfig, nil)
	codec := internode.NewMessageCodec(systempayload.NewTranscoder())
	authorityService := internode.NewService(zap.NewNop(), authorityManager, codec,
		func(pkg *relay.Package) error { return authorityNode.Send(pkg) }, authorityBus, authorityMembership)
	clientService := internode.NewService(zap.NewNop(), clientManager, codec,
		func(pkg *relay.Package) error { return clientNode.Send(pkg) }, clientBus, clientMembership)

	limits := participantSnapshotLimits{MaxEntries: 4, MaxValueBytes: 4096}
	authorityRouter := localrelay.NewRouter(authorityNode, authorityService)
	clientRouter := localrelay.NewRouter(clientNode, clientService)
	authorityRegistry := NewService(authorityEngine, "authority", nil, nil)
	authorityRegistry.ConfigureStrong(StrongDeps{Incarnation: "authority-incarnation"})
	require.NoError(t, authorityRegistry.ConfigureParticipation(4))
	clientRegistry := NewService(clientEngine, "client", nil, nil)
	clientGuard := &topology.NameGuard{}
	clientRegistry.ConfigureStrong(StrongDeps{NameGuard: clientGuard, Incarnation: "client-incarnation", IsLeader: func() bool { return false }})
	clientRegistry.SetNonMember(func() bool { return true })
	require.NoError(t, clientRegistry.ConfigureParticipation(4))
	config := ParticipantEndpointConfig{MaxEntries: 4, MaxValueBytes: 4096, MaxWireBytes: 8192, MaxConcurrentRequests: 1, MaxRedirects: 2, RequestTimeout: 2 * time.Second, RefreshInterval: time.Hour}
	resolve := func(context.Context) (pid.NodeID, error) { return "authority", nil }
	authorityEndpoint, err := NewParticipantEndpoint(ctx, authorityRegistry, authorityRouter, resolve, config)
	require.NoError(t, err)
	clientEndpoint, err := NewParticipantEndpoint(ctx, clientRegistry, clientRouter, resolve, config)
	require.NoError(t, err)
	observer := &participantNativeObserver{host: clientEndpoint, received: make(chan pid.NodeID, 1)}
	require.NoError(t, authorityNode.RegisterHost(RegistryHostID, authorityEndpoint))
	require.NoError(t, clientNode.RegisterHost(RegistryHostID, observer))

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cleanupCancel()
		for _, err := range []error{clientEndpoint.Stop(cleanupCtx), authorityEndpoint.Stop(cleanupCtx), clientService.Stop(), authorityService.Stop()} {
			if err != nil {
				t.Errorf("native participant cleanup: %v", err)
			}
		}
		authorityBus.Stop()
		clientBus.Stop()
	})

	require.NoError(t, authorityService.Start(ctx))
	require.NoError(t, clientService.Start(ctx))

	authorityManager.AddManagedNode("client")
	clientManager.AddManagedNode("authority")
	authorityManager.EnsureConnection("client", "127.0.0.1", clientManager.GetListenPort())
	clientManager.EnsureConnection("authority", "127.0.0.1", authorityManager.GetListenPort())
	require.Eventually(t, func() bool {
		return len(authorityManager.ConnectedNodes()) == 1 && len(clientManager.ConnectedNodes()) == 1
	}, 3*time.Second, 10*time.Millisecond)

	require.NoError(t, authorityEndpoint.Start(ctx))
	enrolled, _, err := authorityRegistry.strong.participants.readSnapshot()
	require.NoError(t, err)
	require.NotContains(t, enrolled, "client", "client must not be pre-enrolled by fixture")
	require.NoError(t, clientEndpoint.Start(ctx))
	require.True(t, clientRegistry.NameReady())
	snapshot, err := authorityRegistry.strong.participants.captureSnapshot(ctx, clientEndpoint.source, "client", "client-incarnation", limits)
	require.NoError(t, err)
	require.NotZero(t, snapshot.Revision)
	entry, ok := snapshot.Entries[participantsKey]
	require.True(t, ok)
	require.NotZero(t, entry.Version)
	members, err := decodeParticipantRecord(entry.Value)
	require.NoError(t, err)
	require.Equal(t, "client-incarnation", members["client"])
	select {
	case receivedFrom := <-observer.received:
		require.Equal(t, pid.NodeID("authority"), receivedFrom)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	// This fixture has a standalone client KV engine, not the KV forwarding
	// stack. Commit retirement directly at the authority only after local seals;
	// the subsequent refused snapshot must travel over authenticated native TLS.
	require.NoError(t, clientRegistry.sealParticipantMutations(ctx))
	require.NoError(t, clientGuard.Close(ctx))
	require.NoError(t, authorityRegistry.strong.participants.retire(ctx, "client", "client-incarnation"))
	_, err = authorityRegistry.strong.participants.captureSnapshot(ctx, clientEndpoint.source, "client", "client-incarnation", limits)
	require.ErrorIs(t, err, ErrParticipantRetired, "require the correlated authority refusal, not just any network error")
	require.False(t, clientRegistry.NameReady())
	enrolled, _, err = authorityRegistry.strong.participants.readSnapshot()
	require.NoError(t, err)
	require.NotContains(t, enrolled, "client", "delayed authenticated snapshot must not re-enroll retired incarnation")
	require.NoError(t, clientEndpoint.Stop(ctx))
	require.False(t, clientRegistry.NameReady())
	require.ErrorIs(t, clientEndpoint.Start(ctx), context.Canceled)

	// The client receives the authority view through the participant source;
	// its independent local engine remains untouched.
	require.NoError(t, clientEngine.Scan(registryPrefix, func(kvapi.Entry) bool {
		t.Fatal("native snapshot must not materialize authority records locally")
		return false
	}))
}
