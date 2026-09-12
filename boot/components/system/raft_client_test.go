// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"errors"
	clusterapi "github.com/wippyai/runtime/api/cluster"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/pid"
	relayapi "github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/cluster/membership"
	"github.com/wippyai/runtime/system/eventbus"
	sysrelay "github.com/wippyai/runtime/system/relay"
	systopology "github.com/wippyai/runtime/system/topology"
	"go.uber.org/zap"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/topology"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
)

// TestLoadClientRegistry_Guards covers unavailable client compositions. Component
// lifetime is tested below; cross-node forwarding is covered in clustertest.
func TestLoadClientRegistry_Guards(t *testing.T) {
	cases := []struct {
		section map[string]any
		name    string
	}{
		{name: "fsm backend never wires a client registry", section: map[string]any{"raft.registry_backend": "fsm"}},
		{name: "kv backend no-ops without relay/membership prerequisites", section: map[string]any{"raft.registry_backend": "kv"}},
		{name: "default backend no-ops without prerequisites", section: map[string]any{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := boot.NewConfig(boot.WithSection(ClusterName, tc.section))
			out, engine, registry, endpoint, err := loadClientRegistry(context.Background(), cfg.Sub(ClusterName), nil)
			require.NoError(t, err)
			require.Nil(t, engine)
			require.Nil(t, registry)
			require.Nil(t, endpoint)
			require.Nil(t, globalapi.GetRegistry(out), "no global.Registry facade must be wired")
			require.Nil(t, topology.GetGlobalRegistry(out), "no topology.GlobalRegistry facade must be wired")
		})
	}
}

func TestClientRegistryComponentRequiresAuthorityAndOwnsFailedStartup(t *testing.T) {
	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	node := sysrelay.NewNode("client")
	router := sysrelay.NewRouter(node, nil)
	ctx = logapi.WithLogger(ctx, logger)
	ctx = event.WithBus(ctx, bus)
	ctx = relayapi.WithNode(ctx, node)
	ctx = relayapi.WithRouter(ctx, router)
	ctx = topology.WithTopology(ctx, systopology.NewTopology(router, node.ID()))
	guard := &topology.NameGuard{}
	ctx = topology.WithNameGuard(ctx, guard)
	ctx = topology.WithRegistry(ctx, systopology.NewPIDRegistry(systopology.WithNameGuard(guard)))
	ctx = clusterapi.WithMembership(ctx, membership.NewService(membership.Config{}, bus, logger, nil, nil, nil))
	ctx = boot.WithConfig(ctx, boot.NewConfig(boot.WithSection(ClusterName, map[string]any{"enabled": true, "raft.enabled": false})))
	component := Raft()
	ctx, err := component.Load(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, component.(boot.Stopper).Stop(ctx)) })
	require.ErrorContains(t, component.(boot.Starter).Start(ctx), "naming startup requires authority for predecessor recovery")
	registry := topology.GetGlobalRegistry(ctx)
	require.NotNil(t, registry)
	require.False(t, registry.NameReady(), "no authority means no naming admission")
	require.NoError(t, component.(boot.Stopper).Stop(ctx))
	require.False(t, registry.NameReady(), "client shutdown must stop its naming owner even while parent context lives")
	_, err = globalapi.GetRegistry(ctx).RegisterScope(context.Background(), "after-stop", pid.PID{Node: "client", Host: "h", UniqID: "p"}, globalapi.Consistent)
	require.True(t, errors.Is(err, globalapi.ErrNotReady), "stopped client must reject registration without forwarding: %v", err)
}
