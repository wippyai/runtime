// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/relay"
	"go.uber.org/zap"
)

func TestContextSendWaitsForDiscoveredPeerRegistration(t *testing.T) {
	cfg := insecureManagerConfig()
	cfg.Logger = zap.NewNop()
	m := NewConnectionManager(cfg, nil).(*manager)
	membership := &mockMembership{nodes: []cluster.NodeInfo{{ID: "peer"}}}
	service := NewService(zap.NewNop(), m, &mockCodec{encoded: []byte("once")}, nil, nil, membership)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	pkg := relay.NewServicePackage("local", "host", "peer", "remote", "request")
	done := make(chan error, 1)
	go func() { done <- service.SendContext(ctx, pkg) }()
	select {
	case err := <-done:
		t.Fatalf("send returned before transport registration: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	// Simulate the transport subscriber running after a discovery consumer.
	m.AddManagedNode("peer")
	require.NoError(t, <-done)
	require.Equal(t, [][]byte{[]byte("once")}, drainAllData(m.nodeStates, "peer"))
	m.RemoveManagedNode("peer")
}

func TestContextSendDiscoveryWaitPreservesCallerOwnership(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "shutdown"}[stop], func(t *testing.T) {
			cfg := insecureManagerConfig()
			cfg.Logger = zap.NewNop()
			m := NewConnectionManager(cfg, nil).(*manager)
			m.ctx, m.cancel = context.WithCancel(t.Context())
			defer m.cancel()
			service := NewService(zap.NewNop(), m, &mockCodec{}, nil, nil, &mockMembership{nodes: []cluster.NodeInfo{{ID: "peer"}}})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			pkg := relay.NewServicePackage("local", "host", "peer", "remote", "request")
			defer relay.ReleasePackage(pkg)
			done := make(chan error, 1)
			go func() { done <- service.SendContext(ctx, pkg) }()
			require.Eventually(t, func() bool {
				m.managedMu.Lock()
				defer m.managedMu.Unlock()
				return m.managedChanged != nil
			}, time.Second, time.Millisecond)
			if stop {
				m.cancel()
			} else {
				cancel()
			}
			select {
			case err := <-done:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(time.Second):
				t.Fatal("discovery wait did not cancel")
			}
			require.Equal(t, cluster.NodeID("peer"), pkg.Target.Node)
			require.False(t, m.IsManaged("peer"), "send must not resurrect peer state")
		})
	}
}

func TestContextSendUnknownPeerDoesNotWaitOrCreateState(t *testing.T) {
	cfg := insecureManagerConfig()
	cfg.Logger = zap.NewNop()
	m := NewConnectionManager(cfg, nil).(*manager)
	service := NewService(zap.NewNop(), m, &mockCodec{}, nil, nil, &mockMembership{})
	pkg := relay.NewServicePackage("local", "host", "unknown", "remote", "request")
	defer relay.ReleasePackage(pkg)
	require.ErrorIs(t, service.SendContext(t.Context(), pkg), ErrNodeNotManaged)
	require.False(t, m.IsManaged("unknown"))
}

func TestManagedWaitCannotResurrectDepartedPeer(t *testing.T) {
	cfg := insecureManagerConfig()
	cfg.Logger = zap.NewNop()
	m := NewConnectionManager(cfg, nil).(*manager)
	m.AddManagedNode("departed")
	m.RemoveManagedNode("departed")
	// Membership can still contain the departed peer; only its subscriber may
	// restore transport state. The send must expire without creating it.
	service := NewService(zap.NewNop(), m, &mockCodec{}, nil, nil, &mockMembership{nodes: []cluster.NodeInfo{{ID: "departed"}}})
	pkg := relay.NewServicePackage("local", "host", "departed", "remote", "request")
	defer relay.ReleasePackage(pkg)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, service.SendContext(ctx, pkg), context.DeadlineExceeded)
	require.False(t, m.IsManaged("departed"))
}

func TestManagedWaitConcurrentRegistrationDoesNotLoseWakeups(t *testing.T) {
	cfg := insecureManagerConfig()
	cfg.Logger = zap.NewNop()
	m := NewConnectionManager(cfg, nil).(*manager)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	const peers = 100
	ready := make(chan struct{})
	done := make(chan error, peers)
	for i := range peers {
		node := fmt.Sprintf("peer-%d", i)
		go func() { <-ready; done <- m.WaitManaged(ctx, node) }()
	}
	close(ready)
	for i := range peers {
		m.AddManagedNode(fmt.Sprintf("peer-%d", i))
	}
	for range peers {
		require.NoError(t, <-done)
	}
	for i := range peers {
		m.RemoveManagedNode(fmt.Sprintf("peer-%d", i))
	}
}
