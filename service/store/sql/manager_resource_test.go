// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	sqlstore "github.com/wippyai/runtime/api/service/store/sql"
	supervisorapi "github.com/wippyai/runtime/api/supervisor"
	storesvc "github.com/wippyai/runtime/service/store"
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
	ctx context.Context
	bus event.Bus
	reg *systemresource.Registry
	sup *systemsupervisor.Supervisor
	mgr *Manager
}

func newStoreHarness(t *testing.T) *storeHarness {
	t.Helper()

	transcoder := payloadSystem.GlobalTranscoder()
	json.Register(transcoder)

	bus := eventbus.NewBus()

	// The handover is confirmed through the await service, exactly as it is in a
	// running instance.
	awaitSvc := eventbus.NewAwaitService(bus)
	ctx := event.WithAwaitService(ctxapi.NewRootContext(), awaitSvc)
	require.NoError(t, awaitSvc.Start(ctx))
	t.Cleanup(func() {
		require.NoError(t, awaitSvc.Stop())
	})

	reg := systemresource.NewRegistry(bus, zap.NewNop())
	require.NoError(t, reg.Start(ctx))

	sup := systemsupervisor.NewSupervisor(bus, zap.NewNop())
	require.NoError(t, sup.Start(ctx))

	t.Cleanup(func() {
		require.NoError(t, sup.Stop())
		require.NoError(t, reg.Stop())
	})

	return &storeHarness{
		ctx: ctx,
		bus: bus,
		reg: reg,
		sup: sup,
		mgr: NewManager(bus, transcoder, zap.NewNop()),
	}
}

// commit applies an entry change inside a registry transaction, the way the
// registry runner delivers one.
func (h *storeHarness) commit(t *testing.T, apply func(ctx context.Context) error) {
	t.Helper()

	ctx := h.ctx
	h.bus.Send(ctx, event.Event{System: registry.System, Kind: registry.TxBegin})
	require.NoError(t, apply(ctx))
	h.bus.Send(ctx, event.Event{System: registry.System, Kind: registry.TxCommit})
}

// awaitProvider polls the resource registry until the acquired provider is want,
// then reports the last observed value and error.
func (h *storeHarness) awaitProvider(t *testing.T, id registry.ID, want any) (any, error) {
	t.Helper()

	ctx := h.ctx
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

func makeSupervisedEntry(id registry.ID, dbID registry.ID, table string, meta attrs.Bag) registry.Entry {
	return registry.Entry{
		ID:   id,
		Kind: sqlstore.KV,
		Meta: meta,
		Data: payload.New(map[string]any{
			"database": map[string]any{
				"ns":   dbID.NS,
				"name": dbID.Name,
			},
			"table_name":          table,
			"id_column_name":      "key",
			"payload_column_name": "value",
			"expire_column_name":  "expires",
			"cleanup_interval":    "1m",
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

	storeID := registry.NewID("test", "sqlcache")
	dbID := registry.NewID("db", "primary")

	h.commit(t, func(ctx context.Context) error {
		return h.mgr.Add(ctx, makeSupervisedEntry(storeID, dbID, "kvstore", attrs.NewBagFrom(map[string]any{
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
		return h.mgr.Update(ctx, makeSupervisedEntry(storeID, dbID, "kvstore_v2", attrs.NewBagFrom(map[string]any{
			"module_version": "1.1.0",
		})))
	})

	updated := h.current(storeID)
	require.NotNil(t, updated)
	require.NotSame(t, original, updated)

	got, err = h.awaitProvider(t, storeID, any(updated))
	require.NoError(t, err)
	require.Same(t, updated, got)

	require.True(t, h.awaitClosed(t, original, true), "superseded store must be stopped")
	require.False(t, h.awaitClosed(t, updated, false), "replacement store must be running")
	require.Equal(t, supervisorapi.StatusRunning, h.awaitStatus(t, storeID, supervisorapi.StatusRunning))

	// Stopping the service through the supervisor must reach the replacement,
	// which proves the supervisor adopted it rather than the superseded store.
	h.bus.Send(h.ctx, event.Event{
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
	ctx := ackCtx()

	storeID := registry.NewID("test", "sqlcache")
	dbID := registry.NewID("db", "primary")
	meta := attrs.NewBagFrom(map[string]any{"module_version": "1.1.0"})

	require.NoError(t, mgr.Add(ctx, makeStoreEntry(storeID, dbID)))
	bus.clearEvents()

	require.NoError(t, mgr.Update(ctx, makeSupervisedEntry(storeID, dbID, "kvstore_v2", meta)))

	events := bus.getEvents()
	require.Len(t, events, 2)

	// The confirmed resource repoint is published before the supervisor is told
	// to adopt the replacement, so the registry is never behind the retirement.
	assert.Equal(t, resource.System, events[0].System)
	assert.Equal(t, resource.Update, events[0].Kind)
	assert.Equal(t, storeID.String(), events[0].Path)

	assert.Equal(t, supervisorapi.System, events[1].System)
	assert.Equal(t, supervisorapi.ServiceRegister, events[1].Kind)
	assert.Equal(t, storeID.String(), events[1].Path)

	entry, ok := events[0].Data.(resource.Entry)
	require.True(t, ok, "resource event must carry a resource.Entry")
	assert.Equal(t, storeID, entry.ID)
	assert.Equal(t, meta, entry.Meta)
	assert.Same(t, mgr.stores[storeID], entry.Provider)
}

// TestManager_UpdateFailsWhenHandoverIsNotConfirmed covers an update whose
// resource repoint never lands: the manager must report the failure and keep
// serving the store it had, rather than claim a replacement nobody is serving.
func TestManager_UpdateFailsWhenHandoverIsNotConfirmed(t *testing.T) {
	mgr, _ := newTestManager(t)

	storeID := registry.NewID("test", "sqlcache")
	dbID := registry.NewID("db", "primary")
	require.NoError(t, mgr.Add(ackCtx(), makeStoreEntry(storeID, dbID)))
	original := mgr.stores[storeID]

	awaitSvc := eventbus.NewAwaitService(eventbus.NewBus())
	base := event.WithAwaitService(ctxapi.NewRootContext(), awaitSvc)
	require.NoError(t, awaitSvc.Start(base))
	t.Cleanup(func() {
		require.NoError(t, awaitSvc.Stop())
	})

	ctx, cancel := context.WithCancel(base)
	cancel()

	err := mgr.Update(ctx, makeStoreEntry(storeID, dbID))
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	assert.Same(t, original, mgr.stores[storeID], "an unconfirmed update must not swap the store")
}

// TestManager_UpdateFailsWithoutCoordination keeps an update from silently
// half-applying where the handover cannot be confirmed at all.
func TestManager_UpdateFailsWithoutCoordination(t *testing.T) {
	mgr, _ := newTestManager(t)

	storeID := registry.NewID("test", "sqlcache")
	dbID := registry.NewID("db", "primary")
	require.NoError(t, mgr.Add(ackCtx(), makeStoreEntry(storeID, dbID)))
	original := mgr.stores[storeID]

	err := mgr.Update(context.Background(), makeStoreEntry(storeID, dbID))
	require.ErrorIs(t, err, storesvc.ErrResourceCoordinationUnavailable)
	assert.Same(t, original, mgr.stores[storeID], "an unconfirmed update must not swap the store")
}

// acceptingAwaitService stands in for the runtime await service in tests that
// use a bus which records events instead of delivering them.
type acceptingAwaitService struct{}

func (acceptingAwaitService) Prepare(
	_ context.Context,
	_ event.System,
	_ event.Kind,
	path event.Path,
	_ time.Duration,
) (event.AwaitWaiter, error) {
	return acceptedWaiter{path: path}, nil
}

func (acceptingAwaitService) Await(
	_ context.Context,
	_ event.System,
	_ event.Kind,
	path event.Path,
	_ time.Duration,
) event.AwaitResult {
	return acceptedWaiter{path: path}.Wait()
}

func (acceptingAwaitService) Start(_ context.Context) error { return nil }
func (acceptingAwaitService) Stop() error                   { return nil }

type acceptedWaiter struct {
	path event.Path
}

func (w acceptedWaiter) Wait() event.AwaitResult {
	return event.AwaitResult{
		Accepted: true,
		Event: event.Event{
			System: resource.System,
			Kind:   resource.Accept,
			Path:   w.path,
		},
	}
}

func (acceptedWaiter) Close() {}

// ackCtx returns a context whose resource handovers are confirmed immediately.
func ackCtx() context.Context {
	return event.WithAwaitService(ctxapi.NewRootContext(), acceptingAwaitService{})
}
