// SPDX-License-Identifier: MPL-2.0

package memory

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/system/eventbus"
	payloadSystem "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/payload/json"
	systemresource "github.com/wippyai/runtime/system/resource"
	"go.uber.org/zap"
)

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

func newResourceTestManager(t *testing.T) (*Manager, *systemresource.Registry) {
	t.Helper()

	transcoder := payloadSystem.GlobalTranscoder()
	json.Register(transcoder)

	bus := eventbus.NewBus()
	reg := systemresource.NewRegistry(bus, zap.NewNop())
	require.NoError(t, reg.Start(context.Background()))
	t.Cleanup(func() {
		require.NoError(t, reg.Stop())
	})

	return NewManager(bus, transcoder, zap.NewNop(), nil), reg
}

// TestManager_UpdateRepointsResourceRegistry covers the store lifecycle a hot
// module install drives: the entry is updated, the manager recreates the store,
// and every later acquisition must reach the live store rather than the stopped
// one.
func TestManager_UpdateRepointsResourceRegistry(t *testing.T) {
	mgr, reg := newResourceTestManager(t)
	ctx := context.Background()

	storeID := registry.NewID("test", "cache")
	require.NoError(t, mgr.Add(ctx, makeStoreEntry(storeID, 1000)))

	mgr.mu.RLock()
	original := mgr.stores[storeID]
	mgr.mu.RUnlock()
	require.NotNil(t, original)

	got, err := awaitProvider(t, reg, storeID, any(original))
	require.NoError(t, err)
	require.Same(t, original, got)

	require.NoError(t, mgr.Update(ctx, makeStoreEntry(storeID, 2000)))

	mgr.mu.RLock()
	updated := mgr.stores[storeID]
	mgr.mu.RUnlock()
	require.NotNil(t, updated)
	require.NotSame(t, original, updated)

	got, err = awaitProvider(t, reg, storeID, any(updated))
	require.NoError(t, err)
	require.Same(t, updated, got)
}
