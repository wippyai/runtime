// SPDX-License-Identifier: MPL-2.0

package cluster

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metricscfg "github.com/wippyai/runtime/api/service/metrics"
	"github.com/wippyai/runtime/cluster/internode"
	"github.com/wippyai/runtime/service/metrics"
	"github.com/wippyai/runtime/system/eventbus"
	"github.com/wippyai/runtime/system/payload"
	"go.uber.org/zap"
)

// Real authenticated stacks share one address without assigning any port.
// Every advertised internode endpoint must remain owned by its listener.
func TestConcurrentAutomaticPorts(t *testing.T) {
	const count = 20
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	collector := metrics.NewCollector(metricscfg.Config{})
	defer collector.Close()
	keys := make([]ed25519.PrivateKey, count)
	trusted := make(map[string]string, count)
	for i := range keys {
		pub, key, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		keys[i] = key
		trusted[fmt.Sprintf("project-%d", i)] = base64.RawStdEncoding.EncodeToString(pub)
	}
	secret := make([]byte, 32)
	_, err := rand.Read(secret)
	require.NoError(t, err)
	stacks := make([]*Stack, count)
	defer func() {
		var stopping sync.WaitGroup
		for _, stack := range stacks {
			if stack != nil {
				stopping.Go(func() { _ = stack.Stop() })
			}
		}
		stopping.Wait()
	}()
	defer func() {
		if t.Failed() {
			for _, stack := range stacks {
				if stack != nil {
					t.Logf("node=%s members=%d connected=%v", stack.Membership.LocalNode().ID, len(stack.Membership.Nodes()), stack.ConnMgr.ConnectedNodes())
				}
			}
		}
	}()
	config := func(i int) StackConfig {
		return StackConfig{
			NodeName: fmt.Sprintf("project-%d", i), Logger: zap.NewNop(),
			Bus: eventbus.NewBus(), Collector: collector, Transcoder: payload.NewTranscoder(),
			MembershipBindAddr: "127.0.0.1", InternodeBindAddr: "127.0.0.1",
			MembershipBindPort: 0, InternodeBindPort: 0, InternodeAutoPort: true,
			SecretKey:                base64.StdEncoding.EncodeToString(secret),
			InternodeIdentityKey:     base64.RawStdEncoding.EncodeToString(keys[i]),
			InternodeTrustedPeerKeys: trusted,
		}
	}
	stacks[0], err = AssembleStack(config(0))
	require.NoError(t, err)
	require.Zero(t, stacks[0].ConnMgr.GetListenPort(), "construction must not bind")
	require.NoError(t, stacks[0].Start(ctx))
	seed := stacks[0].Membership.LocalNode().Addr
	errors := make(chan error, count-1)
	var starting sync.WaitGroup
	for i := 1; i < count; i++ {
		starting.Go(func() {
			cfg := config(i)
			cfg.JoinAddrs = []string{seed}
			stack, err := AssembleStack(cfg)
			if err == nil {
				stacks[i] = stack
				err = stack.Start(ctx)
			}
			errors <- err
		})
	}
	starting.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	ports := make(map[int]bool)
	for _, stack := range stacks {
		port := stack.ConnMgr.GetListenPort()
		require.Positive(t, port)
		require.False(t, ports[port], "duplicate listener port")
		ports[port] = true
		require.Equal(t, strconv.Itoa(port), stack.Membership.LocalNode().Meta[internode.MetadataPort])
		listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if listener != nil {
			_ = listener.Close()
		}
		require.Error(t, err, "advertised listener must remain bound")
	}
	require.Eventually(t, func() bool {
		for _, stack := range stacks {
			if len(stack.ConnMgr.ConnectedNodes()) != count-1 {
				return false
			}
		}
		return true
	}, 20*time.Second, 50*time.Millisecond, "all authenticated mesh connections must form")

	// A new execution keeps its logical name and key, but must not depend on
	// its previous endpoint being available after shutdown.
	oldPort := stacks[count-1].ConnMgr.GetListenPort()
	require.NoError(t, stacks[count-1].Stop())
	occupied, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(oldPort)))
	require.NoError(t, err)
	defer occupied.Close()
	cfg := config(count - 1)
	cfg.JoinAddrs = []string{seed}
	restarted, err := AssembleStack(cfg)
	require.NoError(t, err)
	stacks[count-1] = restarted
	require.NoError(t, restarted.Start(ctx))
	require.NotEqual(t, oldPort, restarted.ConnMgr.GetListenPort())
	joined := false
	defer func() {
		if !joined {
			for _, stack := range stacks {
				t.Logf("node=%s members=%v connected=%v", stack.Membership.LocalNode().ID, stack.Membership.Nodes(), stack.ConnMgr.ConnectedNodes())
			}
		}
	}()
	// A missed leave requires dead-node detection, the 30s reclaim delay,
	// and anti-entropy before the same name can move to a new address.
	require.Eventually(t, func() bool {
		return len(restarted.ConnMgr.ConnectedNodes()) == count-1
	}, 60*time.Second, 50*time.Millisecond, "same identity must rejoin at a new endpoint")
	joined = true
}

func TestFailedJoinReleasesAutomaticPorts(t *testing.T) {
	collector := metrics.NewCollector(metricscfg.Config{})
	defer collector.Close()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	secret := make([]byte, 32)
	_, err = rand.Read(secret)
	require.NoError(t, err)
	stack, err := AssembleStack(StackConfig{
		NodeName: "failed-join", Logger: zap.NewNop(), Bus: eventbus.NewBus(),
		Transcoder: payload.NewTranscoder(), Collector: collector,
		MembershipBindAddr: "127.0.0.1", InternodeBindAddr: "127.0.0.1",
		JoinAddrs:                []string{"127.0.0.1:1"},
		SecretKey:                base64.StdEncoding.EncodeToString(secret),
		InternodeIdentityKey:     base64.RawStdEncoding.EncodeToString(key),
		InternodeTrustedPeerKeys: map[string]string{"failed-join": base64.RawStdEncoding.EncodeToString(pub)},
	})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	require.Error(t, stack.Start(ctx))
	port := stack.ConnMgr.GetListenPort()
	require.Positive(t, port, "internode must have started before the failed join")
	for _, address := range []string{
		net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), stack.Membership.LocalNode().Addr,
	} {
		listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", address)
		require.NoError(t, err, "failed startup must release %s", address)
		require.NoError(t, listener.Close())
	}
	packet, err := (&net.ListenConfig{}).ListenPacket(t.Context(), "udp", stack.Membership.LocalNode().Addr)
	require.NoError(t, err, "failed startup must release gossip UDP")
	require.NoError(t, packet.Close())
	require.NoError(t, stack.Stop())
}
