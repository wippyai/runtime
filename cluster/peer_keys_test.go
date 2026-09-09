// SPDX-License-Identifier: MPL-2.0

package cluster

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	clusterapi "github.com/wippyai/runtime/api/cluster"
	metricscfg "github.com/wippyai/runtime/api/service/metrics"
	"github.com/wippyai/runtime/service/metrics"
	"github.com/wippyai/runtime/system/eventbus"
	"github.com/wippyai/runtime/system/payload"
	"go.uber.org/zap"
)

func TestRunningStackAdmitsNewApprovedKeyWithoutRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ownerPub, ownerKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	clientPub, clientKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	secret := make([]byte, 32)
	_, err = rand.Read(secret)
	require.NoError(t, err)
	collector := metrics.NewCollector(metricscfg.Config{})
	defer collector.Close()
	config := func(name string, key ed25519.PrivateKey) StackConfig {
		return StackConfig{
			NodeName: name, Logger: zap.NewNop(), Bus: eventbus.NewBus(),
			Collector: collector, Transcoder: payload.NewTranscoder(),
			MembershipBindAddr: "127.0.0.1", InternodeBindAddr: "127.0.0.1",
			SecretKey:            base64.StdEncoding.EncodeToString(secret),
			InternodeIdentityKey: base64.RawStdEncoding.EncodeToString(key),
			InternodeTrustedPeerKeys: map[string]string{
				name: base64.RawStdEncoding.EncodeToString(key.Public().(ed25519.PublicKey)),
			},
		}
	}
	var approved atomic.Bool
	denied := make(chan struct{})
	var deniedOnce sync.Once
	ownerConfig := config("owner", ownerKey)
	ownerConfig.InternodePeerKeySource = clusterapi.PeerKeySource(func(id string) (ed25519.PublicKey, bool) {
		if id != "client" {
			return nil, false
		}
		if !approved.Load() {
			deniedOnce.Do(func() { close(denied) })
			return nil, false
		}
		return bytes.Clone(clientPub), true
	})
	owner, err := AssembleStack(ownerConfig)
	require.NoError(t, err)
	defer owner.Stop()
	require.NoError(t, owner.Start(ctx))
	port := owner.ConnMgr.GetListenPort()
	clientConfig := config("client", clientKey)
	clientConfig.InternodeTrustedPeerKeys["owner"] = base64.RawStdEncoding.EncodeToString(ownerPub)
	clientConfig.JoinAddrs = []string{owner.Membership.LocalNode().Addr}
	client, err := AssembleStack(clientConfig)
	require.NoError(t, err)
	defer client.Stop()
	require.NoError(t, client.Start(ctx))
	select {
	case <-denied:
	case <-ctx.Done():
		t.Fatal("new peer did not reach host authorization")
	}
	require.Empty(t, owner.ConnMgr.ConnectedNodes(), "unapproved peer must not connect")
	approved.Store(true)
	require.Eventually(t, func() bool {
		return len(owner.ConnMgr.ConnectedNodes()) == 1 && len(client.ConnMgr.ConnectedNodes()) == 1
	}, 12*time.Second, 20*time.Millisecond, "approved identity must connect to the running owner")
	require.Equal(t, port, owner.ConnMgr.GetListenPort(), "owner did not restart")
}
