// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	clusterapi "github.com/wippyai/runtime/api/cluster"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	logsapi "github.com/wippyai/runtime/api/logs"
	metricsapi "github.com/wippyai/runtime/api/metrics"
	payloadapi "github.com/wippyai/runtime/api/payload"
	relayapi "github.com/wippyai/runtime/api/relay"
	metricscfg "github.com/wippyai/runtime/api/service/metrics"
	"github.com/wippyai/runtime/cluster/internode"
	"github.com/wippyai/runtime/service/metrics"
	"github.com/wippyai/runtime/system/eventbus"
	"github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/relay"
	"go.uber.org/zap"
)

func TestClusterBootPublishesOnlyRetainedListener(t *testing.T) {
	t.Run("normal_shutdown", func(t *testing.T) { checkClusterBootListener(t, false, nil, false) })
	t.Run("failed_join_shutdown", func(t *testing.T) { checkClusterBootListener(t, true, nil, false) })
}

func TestClusterBootRequiresNativePeerKeySource(t *testing.T) {
	t.Run("native", func(t *testing.T) {
		checkClusterBootListener(t, false, clusterapi.PeerKeySource(func(string) (ed25519.PublicKey, bool) {
			return nil, false
		}), false)
	})
	t.Run("yaml_value", func(t *testing.T) {
		checkClusterBootListener(t, false, "allow-all", true)
	})
	t.Run("nil_function", func(t *testing.T) {
		checkClusterBootListener(t, false, clusterapi.PeerKeySource(nil), true)
	})
}

func checkClusterBootListener(t *testing.T, failJoin bool, source any, wantLoadError bool) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	secret := make([]byte, 32)
	_, err = rand.Read(secret)
	require.NoError(t, err)
	settings := map[string]any{
		"enabled": true, ClusterNodeName: "listener-proof", "raft.enabled": false,
		"membership.bind_addr": "127.0.0.1", "membership.bind_port": 0,
		ClusterMembershipSecret: base64.StdEncoding.EncodeToString(secret),
		"internode.bind_addr":   "127.0.0.1", "internode.bind_port": 0,
		"internode.auto_port":                        true,
		"internode.identity_key":                     base64.RawStdEncoding.EncodeToString(key),
		"internode.trusted_peer_keys.listener-proof": base64.RawStdEncoding.EncodeToString(pub),
	}
	if source != nil {
		settings[ClusterInternodePeerKeySource] = source
	}
	if failJoin {
		settings[ClusterMembershipJoin] = "127.0.0.1:1"
	}
	cfg := boot.NewConfig(boot.WithSection("cluster", settings))
	base, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx := ctxapi.WithAppContext(base, ctxapi.NewAppContext())
	ctx = boot.WithConfig(ctx, cfg)
	ctx = logsapi.WithLogger(ctx, zap.NewNop())
	ctx = event.WithBus(ctx, eventbus.NewBus())
	ctx = payloadapi.WithTranscoder(ctx, payload.NewTranscoder())
	relayNode := relay.NewNode("listener-proof")
	ctx = relayapi.WithNode(ctx, relayNode)
	ctx = relayapi.WithRouter(ctx, relay.NewRouter(relayNode, nil))
	collector := metrics.NewCollector(metricscfg.Config{})
	defer collector.Close()
	ctx = metricsapi.WithCollector(ctx, collector)
	component := Cluster()
	ctx, err = component.Load(ctx)
	if wantLoadError {
		require.ErrorContains(t, err, "requires a native PeerKeySource")
		return
	}
	require.NoError(t, err)
	membership := clusterapi.GetMembership(ctx)
	require.NotNil(t, membership)
	require.Empty(t, membership.LocalNode().Meta[internode.MetadataPort], "Load cannot advertise a temporary probe")
	stop := component.(boot.Stopper)
	defer stop.Stop(ctx)
	startErr := component.(boot.Starter).Start(ctx)
	if failJoin {
		require.Error(t, startErr)
	} else {
		require.NoError(t, startErr)
	}
	node := membership.LocalNode()
	port, err := strconv.Atoi(node.Meta[internode.MetadataPort])
	require.NoError(t, err)
	require.Positive(t, port)
	endpoint := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	if !failJoin {
		probe, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", endpoint)
		if probe != nil {
			_ = probe.Close()
		}
		require.Error(t, err, "advertised endpoint must remain reserved")
	}
	require.NoError(t, stop.Stop(ctx))
	probe, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", endpoint)
	require.NoError(t, err, "shutdown must release the retained socket")
	require.NoError(t, probe.Close())
}
