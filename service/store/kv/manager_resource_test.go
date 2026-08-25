// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	kvcfg "github.com/wippyai/runtime/api/service/store/kv"
	"github.com/wippyai/runtime/system/eventbus"
	systemkv "github.com/wippyai/runtime/system/kv"
	payloadSystem "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/payload/json"
	systemresource "github.com/wippyai/runtime/system/resource"
	"go.uber.org/zap"
)

// kvEntryManager is the registry.EntryListener surface both kv store managers
// expose.
type kvEntryManager interface {
	Add(ctx context.Context, entry registry.Entry) error
	Update(ctx context.Context, entry registry.Entry) error
}

// awaitProvider polls the resource registry until the acquired provider is want,
// then reports the last observed value and error.
func awaitProvider(t *testing.T, reg *systemresource.Registry, id registry.ID, want any) (any, error) {
	t.Helper()

	ctx := context.Background()
	deadline := time.Now().Add(2 * time.Second)
	for {
		var (
			val any
			err error
		)
		res, acquireErr := reg.Acquire(ctx, id, resource.ModeNormal)
		if acquireErr != nil {
			err = acquireErr
		} else {
			val, err = res.Get()
			res.Release()
			if err == nil && val == want {
				return val, nil
			}
		}
		if time.Now().After(deadline) {
			return val, err
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func makeKVEntry(id registry.ID, kind registry.Kind, namespace string) registry.Entry {
	return registry.Entry{
		ID:   id,
		Kind: kind,
		Data: payload.New(map[string]any{
			"namespace": namespace,
		}),
	}
}

// TestManagers_UpdateRepointResourceRegistry covers the store lifecycle a hot
// module install drives: the entry is updated, the manager recreates the store
// view, and every later acquisition must reach the live view rather than the
// superseded one.
func TestManagers_UpdateRepointResourceRegistry(t *testing.T) {
	cases := []struct {
		name  string
		kind  registry.Kind
		build func(engine *systemkv.CRDTEngine, bus event.Bus, dtt payload.Transcoder) (kvEntryManager, func(registry.ID) *Store)
	}{
		{
			name: "raft",
			kind: kvcfg.KindRaft,
			build: func(engine *systemkv.CRDTEngine, bus event.Bus, dtt payload.Transcoder) (kvEntryManager, func(registry.ID) *Store) {
				m := NewRaftManager(engine, bus, dtt, zap.NewNop())
				return m, func(id registry.ID) *Store {
					m.mu.RLock()
					defer m.mu.RUnlock()
					return m.stores[id]
				}
			},
		},
		{
			name: "crdt",
			kind: kvcfg.KindCRDT,
			build: func(engine *systemkv.CRDTEngine, bus event.Bus, dtt payload.Transcoder) (kvEntryManager, func(registry.ID) *Store) {
				m := NewCRDTManager(engine, bus, dtt, zap.NewNop())
				return m, func(id registry.ID) *Store {
					m.mu.RLock()
					defer m.mu.RUnlock()
					return m.stores[id]
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			transcoder := payloadSystem.GlobalTranscoder()
			json.Register(transcoder)

			bus := eventbus.NewBus()
			reg := systemresource.NewRegistry(bus, zap.NewNop())
			require.NoError(t, reg.Start(ctx))
			t.Cleanup(func() {
				require.NoError(t, reg.Stop())
			})

			mgr, current := tc.build(systemkv.NewCRDTEngine("test-node", bus, zap.NewNop()), bus, transcoder)

			storeID := registry.NewID("test", "kvstore")
			require.NoError(t, mgr.Add(ctx, makeKVEntry(storeID, tc.kind, "before")))

			original := current(storeID)
			require.NotNil(t, original)

			got, err := awaitProvider(t, reg, storeID, any(original))
			require.NoError(t, err)
			require.Same(t, original, got)

			require.NoError(t, mgr.Update(ctx, makeKVEntry(storeID, tc.kind, "after")))

			updated := current(storeID)
			require.NotNil(t, updated)
			require.NotSame(t, original, updated)

			got, err = awaitProvider(t, reg, storeID, any(updated))
			require.NoError(t, err)
			require.Same(t, updated, got)
		})
	}
}
