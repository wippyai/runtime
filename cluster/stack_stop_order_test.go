// SPDX-License-Identifier: MPL-2.0

package cluster

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metricscfg "github.com/wippyai/runtime/api/service/metrics"
	"github.com/wippyai/runtime/service/metrics"
	"github.com/wippyai/runtime/system/eventbus"
	"github.com/wippyai/runtime/system/payload"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// A node leaves membership while its internode sessions still reach live
// peers. A peer that leaves the cluster as soon as its session to the stopping
// node ends must not prevent the stopping node from completing its leave.
func TestStackStopLeavesMembershipBeforeInternodeCloses(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	collector := metrics.NewCollector(metricscfg.Config{})
	defer collector.Close()

	pubA, keyA, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	pubB, keyB, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	trusted := map[string]string{
		"stop-order-a": base64.RawStdEncoding.EncodeToString(pubA),
		"stop-order-b": base64.RawStdEncoding.EncodeToString(pubB),
	}
	secret := make([]byte, 32)
	_, err = rand.Read(secret)
	require.NoError(t, err)

	core, logs := observer.New(zapcore.DebugLevel)
	config := func(name string, key ed25519.PrivateKey, logger *zap.Logger) StackConfig {
		return StackConfig{
			NodeName: name, Logger: logger, Bus: eventbus.NewBus(),
			Collector: collector, Transcoder: payload.NewTranscoder(),
			MembershipBindAddr: "127.0.0.1", InternodeBindAddr: "127.0.0.1",
			InternodeAutoPort:        true,
			SecretKey:                base64.StdEncoding.EncodeToString(secret),
			InternodeIdentityKey:     base64.RawStdEncoding.EncodeToString(key),
			InternodeTrustedPeerKeys: trusted,
		}
	}

	// The stopping node gossips slower than its peer, so the peer's own leave
	// reaches it before its leave broadcast is gossiped.
	aCfg := config("stop-order-a", keyA, zap.New(core))
	aCfg.MembershipGossipInterval = time.Second
	a, err := AssembleStack(aCfg)
	require.NoError(t, err)
	require.NoError(t, a.Start(ctx))
	bCfg := config("stop-order-b", keyB, zap.NewNop())
	bCfg.JoinAddrs = []string{a.Membership.LocalNode().Addr}
	b, err := AssembleStack(bCfg)
	require.NoError(t, err)
	require.NoError(t, b.Start(ctx))
	defer func() { _ = b.Internode.Stop() }()

	require.Eventually(t, func() bool {
		return len(a.ConnMgr.ConnectedNodes()) == 1 && len(b.ConnMgr.ConnectedNodes()) == 1
	}, 20*time.Second, 20*time.Millisecond, "internode session must form")

	// The peer behaves like a client: it leaves the cluster once its session
	// to the stopping node ends.
	peerLeft := make(chan struct{})
	go func() {
		defer close(peerLeft)
		for len(b.ConnMgr.ConnectedNodes()) > 0 {
			time.Sleep(time.Millisecond)
		}
		_ = b.Membership.Stop()
	}()

	require.NoError(t, a.Stop())

	require.Empty(t, logs.FilterMessage("failed to leave cluster gracefully").All(),
		"leave must complete while a live peer receives the broadcast")
	require.Len(t, logs.FilterMessage("left cluster successfully").All(), 1)
	select {
	case <-peerLeft:
	case <-time.After(10 * time.Second):
		t.Fatal("peer did not observe the end of its session")
	}
}
