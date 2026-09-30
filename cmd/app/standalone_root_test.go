// SPDX-License-Identifier: MPL-2.0

package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	moduleapi "github.com/wippyai/runtime/api/modules"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/runtime/internal/version"
	luapayload "github.com/wippyai/runtime/runtime/lua/engine/payload"
	luaregistry "github.com/wippyai/runtime/runtime/lua/modules/registry"
	transcoder "github.com/wippyai/runtime/system/payload"
	jsonpayload "github.com/wippyai/runtime/system/payload/json"
	registryimpl "github.com/wippyai/runtime/system/registry"
	expansion "github.com/wippyai/runtime/system/registry/expansion"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/topology"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

type standaloneRootRunner struct{}

func (*standaloneRootRunner) Transition(_ context.Context, state regapi.State, changes regapi.ChangeSet, _ func(context.Context)) (regapi.State, error) {
	entries := topology.NewStateMap(state)
	for _, change := range changes {
		if change.Kind == regapi.EntryDelete {
			delete(entries, change.Entry.ID)
		} else {
			entries[change.Entry.ID] = change.Entry
		}
	}
	return topology.StateMapToSlice(entries), nil
}

func TestSeededStandaloneRootVisibleInLuaSnapshot(t *testing.T) {
	bundle := Bundle{Root: "acme/app"}
	for _, name := range []string{"app", "worker"} {
		entries := []wapp.Entry{{ID: wapp.NewID("acme."+name, "definition"), Kind: regapi.NamespaceDefinition}}
		if name == "app" {
			entries = append(entries, wapp.Entry{ID: wapp.NewID("acme.app", "worker"), Kind: regapi.NamespaceDependency,
				Data: map[string]any{"component": "acme/worker", "version": "1.0.0"}})
		}
		var data bytes.Buffer
		require.NoError(t, wapp.NewWriter().PackEntries(wapp.Metadata{"namespace": "acme." + name, "name": name, "version": "1.0.0"}, entries, &data))
		bundle.Packs = append(bundle.Packs, Pack{Module: "acme/" + name, Version: "1.0.0", Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(data.Bytes())), Data: data.Bytes()})
	}
	deployment := filepath.Join(t.TempDir(), "deployment")
	lockPath, err := bundle.Seed(deployment)
	require.NoError(t, err)
	locked, err := lock.New(lockPath)
	require.NoError(t, err)
	require.Equal(t, []string{"acme/app"}, locked.GetRootModules())

	ctx := moduleapi.WithSourceRegistry(ctxapi.NewRootContext(), moduleapi.NewSourceRegistry())
	dtt := transcoder.GlobalTranscoder()
	jsonpayload.Register(dtt)
	luapayload.Register(dtt)
	ctx = payload.WithTranscoder(ctx, dtt)
	ctx = security.SetStrictMode(ctx, false)
	ctx = regapi.WithDependencyAccess(ctx, regapi.DependencyAccessVerifiedOffline)
	resolver := topology.NewResolver()
	history := historymem.New()
	client := &offlineHub{}
	handler, err := hub.NewDependencyHandler(hub.DependencyHandlerOptions{
		Hub: client, Resolver: resolver, LockPath: lockPath,
		VendorDir: filepath.Join(deployment, locked.GetVendorPath()), Logger: zap.NewNop(),
	})
	require.NoError(t, err)
	require.NoError(t, handler.PrepareRestore(ctx, history))
	reg := registryimpl.NewRegistry(history, &standaloneRootRunner{},
		topology.NewStateBuilder(zap.NewNop(), resolver), resolver, zap.NewNop(),
		registryimpl.WithKindDirective(regapi.NamespaceDependency,
			expansion.NewDependencyDirective(handler.Expand).WithResolutionTransition(handler.ReconcileResolution)))
	// Bundle.Seed selects the application solely in wippy.lock. Its pack has
	// no dependency declaration pointing back to itself.
	baseline := regapi.State{
		{ID: regapi.NewID("acme.app", "definition"), Kind: regapi.NamespaceDefinition, Registry: regapi.EntryMetadata{Owner: "acme/app"}},
		{ID: regapi.NewID("acme.app", "worker"), Kind: regapi.NamespaceDependency, Registry: regapi.EntryMetadata{Owner: "acme/app", Root: true},
			Data: payload.New(map[string]any{"component": "acme/worker", "version": "1.0.0"})},
		{ID: regapi.NewID("acme.worker", "definition"), Kind: regapi.NamespaceDefinition, Registry: regapi.EntryMetadata{Owner: "acme/worker"}},
	}
	require.NoError(t, reg.LoadState(ctx, baseline, version.FromParent(nil, regapi.RootVersion)))
	require.Zero(t, client.requests)
	captured := reg.Snapshot()
	require.NotNil(t, captured.Registry.Resolution)
	require.Len(t, captured.Registry.Resolution.Roots, 1)
	require.Equal(t, "acme/app", captured.Registry.Resolution.Deployment.Root)

	l := lua.NewState()
	defer l.Close()
	l.SetContext(regapi.WithRegistry(ctx, reg))
	lua.OpenErrors(l)
	table, _ := luaregistry.Module.Build()
	l.SetGlobal("registry", table)
	require.NoError(t, l.DoString(`
        local snapshot = assert(registry.snapshot())
        local first = assert(snapshot:state())
        assert(#first.resolution.roots == 1)

        assert(first.resolution.deployment ~= nil, "lock-selected standalone root is absent from Lua inventory")
        assert(first.resolution.deployment.root == "acme/app")
        assert(first.resolution.deployment.modules[1].name == "acme/app")
        assert(first.resolution.deployment.modules[1].version == "1.0.0")
        assert(first.resolution.deployment.modules[1].digest == first.resolution.modules[1].digest)
        first.resolution.deployment.root = "forged/root"
        first.resolution.deployment.modules[1].version = "999.0.0"
        local second = assert(snapshot:state())
        assert(second.resolution.deployment.root == "acme/app")
        assert(second.resolution.deployment.modules[1].version == "1.0.0")
    `))
	require.Equal(t, "acme/app", captured.Registry.Resolution.Deployment.Root)
}
