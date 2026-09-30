// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/boot"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	relayapi "github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/api/topology"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	processmodule "github.com/wippyai/runtime/runtime/lua/modules/process"
	systemrelay "github.com/wippyai/runtime/system/relay"
	systemtopology "github.com/wippyai/runtime/system/topology"
)

func standaloneNamesContext(t *testing.T, cfg boot.Config) (context.Context, *systemrelay.Node) {
	t.Helper()
	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	if cfg != nil {
		ctx = boot.WithConfig(ctx, cfg)
	}
	node := systemrelay.NewNode("standalone")
	router := systemrelay.NewRouter(node, nil)
	ctx = relayapi.WithNode(ctx, node)
	ctx = relayapi.WithRouter(ctx, router)
	ctx = topology.WithTopology(ctx, systemtopology.NewTopology(router, node.ID()))
	ctx = topology.WithRegistry(ctx, systemtopology.NewPIDRegistry())
	return ctx, node
}

func startStandaloneNames(t *testing.T, cfg boot.Config) (context.Context, *systemrelay.Node) {
	t.Helper()
	ctx, node := standaloneNamesContext(t, cfg)
	for _, component := range []boot.Component{Raft(), EventualReg()} {
		var err error
		ctx, err = component.Load(ctx)
		require.NoError(t, err)
		stopper := component.(boot.Stopper)
		t.Cleanup(func() { require.NoError(t, stopper.Stop(ctx)) })
		require.NoError(t, component.(boot.Starter).Start(ctx))
	}
	return ctx, node
}

func TestStandaloneNames_AllScopes(t *testing.T) {
	for _, configured := range []bool{false, true} {
		name := "no config"
		var cfg boot.Config
		if configured {
			name = "cluster explicitly disabled"
			// Cluster-only knobs must not change standalone naming.
			cfg = boot.NewConfig(boot.WithSection(ClusterName, map[string]any{
				"enabled": false, "raft.enabled": false, "raft.role": "client", "raft.registry_backend": "fsm",
			}))
		}
		t.Run(name, func(t *testing.T) {
			ctx, node := startStandaloneNames(t, cfg)
			local := topology.GetRegistry(ctx)
			eventual := topology.GetEventualRegistry(ctx)
			global := globalapi.GetRegistry(ctx)
			require.NotNil(t, eventual)
			require.NotNil(t, global)
			require.Nil(t, GetKVRaftEngine(ctx), "standalone names must not expose a Raft store")
			p := pid.PID{Node: node.ID(), Host: "app", UniqID: "one"}
			_, err := local.Register("local", p)
			require.NoError(t, err)
			_, err = eventual.Register("eventual", p)
			require.NoError(t, err)
			for _, mode := range []globalapi.RegistrationMode{globalapi.Consistent, globalapi.Strong} {
				name := "consistent"
				if mode == globalapi.Strong {
					name = "strong"
				}
				out, err := global.RegisterScope(ctx, name, p, mode)
				require.NoError(t, err)
				require.Equal(t, globalapi.RegisterStateActive, out.State)
				require.True(t, p.Equal(out.PID))
			}
			for _, name := range []string{"local", "eventual", "consistent", "strong"} {
				got, found := local.Lookup(name)
				require.True(t, found, name)
				require.True(t, p.Equal(got), name)
			}
			reverse, err := global.Lookup(ctx, "", globalapi.ByPID(p))
			require.NoError(t, err)
			require.ElementsMatch(t, []string{"consistent", "strong"}, reverse.NamesForPID)
			require.True(t, local.Unregister("local"))
			require.True(t, eventual.Unregister("eventual"))
			removed, err := global.UnregisterScope(ctx, "consistent", globalapi.Consistent)
			require.NoError(t, err)
			require.True(t, removed)
			removed, err = global.UnregisterScope(ctx, "strong", globalapi.Strong)
			require.NoError(t, err)
			require.True(t, removed)
		})
	}
}

func TestStandaloneNames_IndependentScopesAndPrecedence(t *testing.T) {
	ctx, node := startStandaloneNames(t, boot.NewConfig())
	local := topology.GetRegistry(ctx)
	eventual := topology.GetEventualRegistry(ctx)
	global := globalapi.GetRegistry(ctx)
	p1 := pid.PID{Node: node.ID(), Host: "app", UniqID: "local"}
	p2 := pid.PID{Node: node.ID(), Host: "app", UniqID: "eventual"}
	p3 := pid.PID{Node: node.ID(), Host: "app", UniqID: "strong"}
	_, err := local.Register("same", p1)
	require.NoError(t, err)
	_, err = eventual.Register("same", p2)
	require.NoError(t, err)
	_, err = global.RegisterScope(ctx, "same", p3, globalapi.Strong)
	require.NoError(t, err)
	got, found := local.Lookup("same")
	require.True(t, found)
	require.True(t, p3.Equal(got))
	_, err = global.RegisterScope(ctx, "same", p1, globalapi.Consistent)
	require.ErrorIs(t, err, globalapi.ErrNameAlreadyRegistered)
	// Global removal exposes, rather than revokes, the weaker bindings.
	removed, err := global.UnregisterScope(ctx, "same", globalapi.Strong)
	require.NoError(t, err)
	require.True(t, removed)
	got, found = local.Lookup("same")
	require.True(t, found)
	require.True(t, p2.Equal(got))
	require.True(t, eventual.Unregister("same"))
	got, found = local.Lookup("same")
	require.True(t, found)
	require.True(t, p1.Equal(got))
}

func TestStandaloneNames_ConcurrentStrongSingleOwner(t *testing.T) {
	ctx, node := startStandaloneNames(t, boot.NewConfig())
	reg := globalapi.GetRegistry(ctx)
	for iteration := range 20 {
		var wg sync.WaitGroup
		start := make(chan struct{})
		results := make(chan error, 2)
		for _, id := range []string{"one", "two"} {
			wg.Go(func() {
				<-start
				_, err := reg.RegisterScope(ctx, "contested", pid.PID{Node: node.ID(), Host: "app", UniqID: id}, globalapi.Strong)
				results <- err
			})
		}
		close(start)
		wg.Wait()
		close(results)
		wins := 0
		for err := range results {
			if err == nil {
				wins++
			} else {
				require.True(t, errors.Is(err, globalapi.ErrNameAlreadyRegistered) || errors.Is(err, globalapi.ErrPendingConflict), "%v", err)
			}
		}
		require.Equal(t, 1, wins, "attempt %d", iteration)
		removed, err := reg.UnregisterScope(ctx, "contested", globalapi.Strong)
		require.NoError(t, err)
		require.True(t, removed)
	}
}

func TestStandaloneNames_ProcessExitReapsGlobalNames(t *testing.T) {
	ctx, node := startStandaloneNames(t, boot.NewConfig())
	topo := topology.GetTopology(ctx)
	reg := globalapi.GetRegistry(ctx)
	p := pid.PID{Node: node.ID(), Host: "app", UniqID: "owner"}
	require.NoError(t, topo.Register(p))
	_, err := reg.RegisterScope(ctx, "consistent", p, globalapi.Consistent)
	require.NoError(t, err)
	_, err = reg.RegisterScope(ctx, "strong", p, globalapi.Strong)
	require.NoError(t, err)
	topo.Complete(p, &runtime.Result{})
	require.Eventually(t, func() bool {
		res, lookupErr := reg.Lookup(ctx, "", globalapi.ByPID(p))
		return lookupErr == nil && len(res.NamesForPID) == 0
	}, time.Second, time.Millisecond)
}

func TestStandaloneNames_StopBeforeStart(t *testing.T) {
	ctx, node := standaloneNamesContext(t, boot.NewConfig())
	component := Raft()
	ctx, err := component.Load(ctx)
	require.NoError(t, err)
	require.NoError(t, component.(boot.Stopper).Stop(ctx))
	_, found := node.GetHost("sysreg")
	require.False(t, found)
}

func TestStandaloneNames_LoadFailurePreservesExistingHost(t *testing.T) {
	ctx, node := standaloneNamesContext(t, boot.NewConfig())
	require.NoError(t, node.RegisterHost("sysreg", node))
	_, err := Raft().Load(ctx)
	require.ErrorContains(t, err, "register relay host")
	host, found := node.GetHost("sysreg")
	require.True(t, found)
	require.Same(t, node, host)
	require.Nil(t, globalapi.GetRegistry(ctx))
}

func TestStandaloneNames_LuaSurfaceAndPermissions(t *testing.T) {
	for _, strict := range []bool{false, true} {
		name := "trusted context"
		if strict {
			name = "strict context without grants"
		}
		t.Run(name, func(t *testing.T) {
			ctx, node := startStandaloneNames(t, boot.NewConfig())
			security.SetStrictMode(ctx, strict)
			ctx, frame := ctxapi.OpenFrameContext(ctx)
			t.Cleanup(func() { ctxapi.ReleaseFrameContext(frame) })
			p := pid.PID{Node: node.ID(), Host: "app", UniqID: "lua"}
			p = p.Precomputed()
			require.NoError(t, topology.GetTopology(ctx).Register(p))
			require.NoError(t, runtime.SetFramePID(ctx, p))
			l := lua.NewState()
			t.Cleanup(l.Close)
			l.SetContext(ctx)
			module, _ := processmodule.Module.Build()
			l.SetGlobal("process", module)
			if !strict {
				require.NoError(t, l.DoString(`
        for _, scope in ipairs({process.registry.LOCAL, process.registry.EVENTUAL,
                                process.registry.CONSISTENT, process.registry.STRONG}) do
            local ok, err = process.registry.register("lua-scope", nil, scope)
            assert(ok, tostring(err))
            local owner, lookup_err = process.registry.lookup("lua-scope")
            assert(owner == process.pid(), tostring(lookup_err))
            ok, err = process.registry.unregister("lua-scope", scope)
            assert(ok, tostring(err))
        end
    `))
				return
			}
			// Boot wiring must not bypass the existing per-scope authorization.
			require.NoError(t, l.DoString(`
        for _, scope in ipairs({process.registry.LOCAL, process.registry.EVENTUAL,
                                process.registry.CONSISTENT, process.registry.STRONG}) do
            local ok, err = process.registry.register("denied", nil, scope)
            assert(ok == nil and err ~= nil)
        end
    `))
			_, found := topology.GetRegistry(ctx).Lookup("denied")
			require.False(t, found)
		})
	}
}

func TestStandaloneNames_EnabledClusterDoesNotFallBack(t *testing.T) {
	for _, section := range []map[string]any{
		{"enabled": true},
		{"enabled": true, "raft.enabled": false},
		{"enabled": true, "raft.role": "client"},
	} {
		ctx, _ := standaloneNamesContext(t, boot.NewConfig(boot.WithSection(ClusterName, section)))
		ctx, err := Raft().Load(ctx)
		require.NoError(t, err)
		require.Nil(t, globalapi.GetRegistry(ctx), "no standalone authority without the configured cluster")
		_, err = EventualReg().Load(ctx)
		require.ErrorContains(t, err, "cluster enabled but membership not available")
		require.Nil(t, topology.GetEventualRegistry(ctx))
	}
}
