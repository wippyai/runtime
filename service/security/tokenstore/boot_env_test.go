// SPDX-License-Identifier: MPL-2.0

package tokenstore_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	envapi "github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/security"
	memapi "github.com/wippyai/runtime/api/service/store/memory"
	envsvc "github.com/wippyai/runtime/service/env"
	envos "github.com/wippyai/runtime/service/env/os"
	tokenimpl "github.com/wippyai/runtime/service/security/tokenstore"
	memorystore "github.com/wippyai/runtime/service/store/memory"
	sysenv "github.com/wippyai/runtime/system/env"
	"github.com/wippyai/runtime/system/eventbus"
	"github.com/wippyai/runtime/system/registry/topology"
	securitysys "github.com/wippyai/runtime/system/security"
	"go.uber.org/zap"
)

func TestTokenStoreBootEnvSigningMatchesUpdate(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "test-boot-signing-key")
	ctx := ctxapi.NewRootContext()
	bus := eventbus.NewBus()
	t.Cleanup(bus.Stop)
	envreg := sysenv.NewRegistry(bus, zap.NewNop())
	ctx = envapi.WithRegistry(ctx, envreg)
	dtt := &jsonTranscoder{}
	resources := newTestResourceRegistry()
	storeID := registry.NewID("app", "tokens")
	mem := memorystore.NewStore(storeID, &memapi.Config{MaxSize: 100, CleanupInterval: time.Second}, zap.NewNop())
	started, err := mem.Start(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, mem.Stop(ctx)) })
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("store does not start")
	}
	resources.Register(storeID, mem)
	manager := tokenimpl.NewManager(bus, dtt, resources, newTestSecurityRegistry(), zap.NewNop())
	osm := envos.NewManager(bus, dtt, zap.NewNop())
	variables := envsvc.NewVariableManager(bus, dtt, zap.NewNop())
	tokenEntry := registry.Entry{ID: registry.NewID("app", "auth"), Kind: "security.token_store", Data: payload.New(map[string]any{
		"store": storeID.String(), "token_key": "", "token_key_env": "ENCRYPTION_KEY", "token_length": 32,
	})}
	entries := registry.State{
		tokenEntry,
		{ID: registry.NewID("settings", "signing"), Kind: "env.variable", Data: payload.New(map[string]any{"storage": "settings:os", "variable": "ENCRYPTION_KEY"})},
		{ID: registry.NewID("settings", "os"), Kind: "env.storage.os", Data: payload.New(map[string]any{})},
	}
	resolver := topology.NewResolver()
	require.NoError(t, resolver.RegisterPattern(registry.DependencyPattern{Path: "data.storage"}))
	require.NoError(t, resolver.RegisterPattern(registry.DependencyPattern{Path: "data.*_env", AllowWildcard: true}))
	sorted, err := topology.SortEntriesByDependency(entries, resolver)
	require.NoError(t, err)
	for _, entry := range sorted {
		switch entry.Kind {
		case "env.storage.os":
			require.NoError(t, osm.Add(ctx, entry))
		case "env.variable":
			require.NoError(t, variables.Add(ctx, entry))
		case "security.token_store":
			require.NoError(t, manager.Add(ctx, entry))
		}
	}
	acquire := func() *tokenimpl.TokenStore {
		res, err := manager.Acquire(ctx, tokenEntry.ID, resource.ModeNormal)
		require.NoError(t, err)
		t.Cleanup(res.Release)
		value, err := res.Get()
		require.NoError(t, err)
		ts, ok := value.(*tokenimpl.TokenStore)
		require.True(t, ok)
		return ts
	}
	boot := acquire()
	actor := security.Actor{ID: "member"}
	scope := securitysys.NewScope(nil)
	saved, err := boot.Create(ctx, actor, scope, security.TokenDetails{})
	require.NoError(t, err)
	require.Equal(t, 2, len(strings.Split(string(saved), ".")), "boot must sign with the registered environment key")
	require.NoError(t, manager.Update(ctx, tokenEntry))
	updated := acquire()
	validated, _, err := updated.Validate(ctx, saved)
	require.NoError(t, err, "boot tokens must survive the same configuration update")
	require.Equal(t, actor.ID, validated.ID)
	fresh, err := updated.Create(ctx, actor, scope, security.TokenDetails{})
	require.NoError(t, err)
	require.Equal(t, 2, len(strings.Split(string(fresh), ".")))
	_, _, err = boot.Validate(ctx, fresh)
	require.NoError(t, err)
}
