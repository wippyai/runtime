// SPDX-License-Identifier: MPL-2.0

package env

import (
	"testing"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	envapi "github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/system/eventbus"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

func TestTopologyMatchesEnvironmentLookupPriority(t *testing.T) {
	bus := eventbus.NewBus()
	t.Cleanup(bus.Stop)
	reg := NewRegistry(bus, zap.NewNop())
	ctx, frame := ctxapi.OpenFrameContext(ctxapi.NewRootContext())
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(frame) })
	consumer := registry.Entry{ID: registry.ParseID("app:consumer")}
	require.NoError(t, frame.Set(runtime.FrameIDKey, consumer.ID))
	storageID := registry.ParseID("settings:storage")
	reg.RegisterStorage(storageID, newMockStorage(nil))
	local := envapi.Variable{ID: registry.ParseID("app:KEY"), Name: "LOCAL", StorageID: storageID}
	alias := envapi.Variable{ID: registry.ParseID("settings:key"), Name: "KEY", StorageID: storageID}
	providers := registry.State{
		{ID: local.ID, Kind: "env.variable", Data: payload.New(local)},
		{ID: alias.ID, Kind: "env.variable", Data: payload.New(&alias)},
	}
	require.NoError(t, reg.RegisterVariable(local))
	require.NoError(t, reg.RegisterVariable(alias))
	assertLookup := func(name string, state registry.State) {
		t.Helper()
		selected, err := reg.findVariable(ctx, name)
		require.NoError(t, err)
		consumer.Data = payload.New(map[string]any{"key_env": name})
		entries := append(append(registry.State(nil), state...), consumer)
		edges := topology.ResolveDependencies(topology.NewStateMap(entries), topology.NewResolver())
		require.Equal(t, []registry.ID{selected.ID}, edges[consumer.ID], "topology and actual lookup disagree for %s", name)
	}
	assertLookup("KEY", providers)
	assertLookup("app:KEY", providers)
	assertLookup("settings:key", providers)
	reg.UnregisterVariable(alias.ID)
	assertLookup("KEY", providers[:1])
}
