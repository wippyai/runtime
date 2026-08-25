// SPDX-License-Identifier: MPL-2.0

package memory

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	memstore "github.com/wippyai/runtime/api/service/store/memory"
	supervisorapi "github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/system/eventbus"
	payloadSystem "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/payload/json"
	systemresource "github.com/wippyai/runtime/system/resource"
	systemsupervisor "github.com/wippyai/runtime/system/supervisor"
	"go.uber.org/zap"
)

// storeHarness wires a manager to the live supervisor and resource registry
// over a shared bus, so entry changes travel the same path they take at
// runtime.
type storeHarness struct {
	bus event.Bus
	reg *systemresource.Registry
	sup *systemsupervisor.Supervisor
	mgr *Manager
}

func newStoreHarness(t *testing.T) *storeHarness {
	t.Helper()

	transcoder := payloadSystem.GlobalTranscoder()
	json.Register(transcoder)

	ctx := context.Background()
	bus := eventbus.NewBus()

	reg := systemresource.NewRegistry(bus, zap.NewNop())
	require.NoError(t, reg.Start(ctx))

	sup := systemsupervisor.NewSupervisor(bus, zap.NewNop())
	require.NoError(t, sup.Start(ctx))

	t.Cleanup(func() {
		require.NoError(t, sup.Stop())
		require.NoError(t, reg.Stop())
	})

	return &storeHarness{
		bus: bus,
		reg: reg,
		sup: sup,
		mgr: NewManager(bus, transcoder, zap.NewNop(), nil),
	}
}

// commit applies an entry change inside a registry transaction, the way the
// registry runner delivers one.
func (h *storeHarness) commit(t *testing.T, apply func(ctx context.Context) error) {
	t.Helper()

	ctx := context.Background()
	h.bus.Send(ctx, event.Event{System: registry.System, Kind: registry.TxBegin})
	require.NoError(t, apply(ctx))
	h.bus.Send(ctx, event.Event{System: registry.System, Kind: registry.TxCommit})
}

// awaitProvider polls the resource registry until the acquired provider is want,
// then reports the last observed value and error.
func (h *storeHarness) awaitProvider(t *testing.T, id registry.ID, want any) (any, error) {
	t.Helper()

	ctx := context.Background()
	deadline := time.Now().Add(2 * time.Second)
	for {
		var (
			val any
			err error
		)
		res, acquireErr := h.reg.Acquire(ctx, id, resource.ModeNormal)
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

func (h *storeHarness) awaitStatus(t *testing.T, id registry.ID, want supervisorapi.Status) supervisorapi.Status {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for {
		state, err := h.sup.GetState(id.String())
		if err == nil {
			if state.Status == want {
				return state.Status
			}
			if time.Now().After(deadline) {
				return state.Status
			}
		} else if time.Now().After(deadline) {
			return supervisorapi.StatusUnknown
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (h *storeHarness) awaitClosed(t *testing.T, store *Store, want bool) bool {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for {
		store.mu.RLock()
		got := store.closed
		store.mu.RUnlock()
		if got == want || time.Now().After(deadline) {
			return got
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (h *storeHarness) current(id registry.ID) *Store {
	h.mgr.mu.RLock()
	defer h.mgr.mu.RUnlock()
	return h.mgr.stores[id]
}

func makeSupervisedEntry(id registry.ID, maxSize int, meta attrs.Bag) registry.Entry {
	return registry.Entry{
		ID:   id,
		Kind: memstore.KV,
		Meta: meta,
		Data: payload.New(map[string]any{
			"max_size":         maxSize,
			"cleanup_interval": "1m",
			"lifecycle": map[string]any{
				"auto_start": true,
			},
		}),
	}
}

// TestManager_UpdateHandsOverToReplacement covers the store lifecycle a hot
// module install drives: the entry is updated, the manager installs a
// replacement store, and both the supervisor and the resource registry must end
// up on that replacement while the superseded instance is retired.
func TestManager_UpdateHandsOverToReplacement(t *testing.T) {
	h := newStoreHarness(t)

	storeID := registry.NewID("test", "cache")
	h.commit(t, func(ctx context.Context) error {
		return h.mgr.Add(ctx, makeSupervisedEntry(storeID, 1000, attrs.NewBagFrom(map[string]any{
			"module_version": "1.0.0",
		})))
	})

	original := h.current(storeID)
	require.NotNil(t, original)

	got, err := h.awaitProvider(t, storeID, any(original))
	require.NoError(t, err)
	require.Same(t, original, got)
	require.Equal(t, supervisorapi.StatusRunning, h.awaitStatus(t, storeID, supervisorapi.StatusRunning))

	h.commit(t, func(ctx context.Context) error {
		return h.mgr.Update(ctx, makeSupervisedEntry(storeID, 2000, attrs.NewBagFrom(map[string]any{
			"module_version": "1.1.0",
		})))
	})

	updated := h.current(storeID)
	require.NotNil(t, updated)
	require.NotSame(t, original, updated)

	// The resource registry serves the replacement.
	got, err = h.awaitProvider(t, storeID, any(updated))
	require.NoError(t, err)
	require.Same(t, updated, got)

	// The superseded store is retired and the replacement is live.
	require.True(t, h.awaitClosed(t, original, true), "superseded store must be stopped")
	require.False(t, h.awaitClosed(t, updated, false), "replacement store must be running")
	require.Equal(t, supervisorapi.StatusRunning, h.awaitStatus(t, storeID, supervisorapi.StatusRunning))

	// Stopping the service through the supervisor must reach the replacement,
	// which proves the supervisor adopted it rather than the superseded store.
	h.bus.Send(context.Background(), event.Event{
		System: supervisorapi.System,
		Kind:   supervisorapi.ServiceStop,
		Path:   storeID.String(),
	})
	require.True(t, h.awaitClosed(t, updated, true), "supervisor must supervise the replacement store")
}

// TestManager_UpdateEmitsResourceEntryShape pins the payload the resource
// registry routes on, including the metadata carried from the entry.
func TestManager_UpdateEmitsResourceEntryShape(t *testing.T) {
	mgr, bus := newTestManager(t)
	ctx := context.Background()

	storeID := registry.NewID("test", "cache")
	meta := attrs.NewBagFrom(map[string]any{"module_version": "1.1.0"})

	require.NoError(t, mgr.Add(ctx, makeStoreEntry(storeID, 1000)))
	bus.clearEvents()

	require.NoError(t, mgr.Update(ctx, makeSupervisedEntry(storeID, 2000, meta)))

	events := bus.getEvents()
	require.Len(t, events, 2)

	assert.Equal(t, supervisorapi.System, events[0].System)
	assert.Equal(t, supervisorapi.ServiceRegister, events[0].Kind)
	assert.Equal(t, storeID.String(), events[0].Path)

	assert.Equal(t, resource.System, events[1].System)
	assert.Equal(t, resource.Update, events[1].Kind)
	assert.Equal(t, storeID.String(), events[1].Path)

	entry, ok := events[1].Data.(resource.Entry)
	require.True(t, ok, "resource event must carry a resource.Entry")
	assert.Equal(t, storeID, entry.ID)
	assert.Equal(t, meta, entry.Meta)
	assert.Same(t, mgr.stores[storeID], entry.Provider)
}

// TestManager_UpdateRejectsCancelledContext keeps the manager from installing a
// replacement whose handover events the bus would drop.
func TestManager_UpdateRejectsCancelledContext(t *testing.T) {
	mgr, _ := newTestManager(t)

	storeID := registry.NewID("test", "cache")
	require.NoError(t, mgr.Add(context.Background(), makeStoreEntry(storeID, 1000)))
	original := mgr.stores[storeID]

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.ErrorIs(t, mgr.Update(ctx, makeStoreEntry(storeID, 2000)), context.Canceled)
	assert.Same(t, original, mgr.stores[storeID], "a rejected update must not swap the store")
}
