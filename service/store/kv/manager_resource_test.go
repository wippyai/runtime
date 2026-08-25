// SPDX-License-Identifier: MPL-2.0

package kv

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
	kvcfg "github.com/wippyai/runtime/api/service/store/kv"
	supervisorapi "github.com/wippyai/runtime/api/supervisor"
	storesvc "github.com/wippyai/runtime/service/store"
	"github.com/wippyai/runtime/system/eventbus"
	systemkv "github.com/wippyai/runtime/system/kv"
	payloadSystem "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/payload/json"
	systemresource "github.com/wippyai/runtime/system/resource"
	systemsupervisor "github.com/wippyai/runtime/system/supervisor"
	"go.uber.org/zap"
)

// kvEntryManager is the registry.EntryListener surface both kv store managers
// expose.
type kvEntryManager interface {
	Add(ctx context.Context, entry registry.Entry) error
	Update(ctx context.Context, entry registry.Entry) error
}

// storeHarness wires a manager to the live supervisor and resource registry
// over a shared bus, so entry changes travel the same path they take at
// runtime.
type storeHarness struct {
	ctx     context.Context
	bus     event.Bus
	reg     *systemresource.Registry
	sup     *systemsupervisor.Supervisor
	mgr     kvEntryManager
	current func(registry.ID) *Store
}

func newStoreHarness(
	t *testing.T,
	build func(*systemkv.CRDTEngine, event.Bus, payload.Transcoder) (kvEntryManager, func(registry.ID) *Store),
) *storeHarness {
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

	mgr, current := build(systemkv.NewCRDTEngine("test-node", bus, zap.NewNop()), bus, transcoder)

	return &storeHarness{ctx: ctx, bus: bus, reg: reg, sup: sup, mgr: mgr, current: current}
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
		store.mu.Lock()
		got := store.closed
		store.mu.Unlock()
		if got == want || time.Now().After(deadline) {
			return got
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func makeKVEntry(id registry.ID, kind registry.Kind, namespace string, meta attrs.Bag) registry.Entry {
	return registry.Entry{
		ID:   id,
		Kind: kind,
		Meta: meta,
		Data: payload.New(map[string]any{
			"namespace": namespace,
			"lifecycle": map[string]any{
				"auto_start": true,
			},
		}),
	}
}

func kvManagerCases() []struct {
	name  string
	kind  registry.Kind
	build func(*systemkv.CRDTEngine, event.Bus, payload.Transcoder) (kvEntryManager, func(registry.ID) *Store)
} {
	return []struct {
		name  string
		kind  registry.Kind
		build func(*systemkv.CRDTEngine, event.Bus, payload.Transcoder) (kvEntryManager, func(registry.ID) *Store)
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
}

// TestManagers_UpdateHandOverToReplacement covers the store lifecycle a hot
// module install drives: the entry is updated, the manager installs a
// replacement view, and both the supervisor and the resource registry must end
// up on that replacement while the superseded view is retired.
func TestManagers_UpdateHandOverToReplacement(t *testing.T) {
	for _, tc := range kvManagerCases() {
		t.Run(tc.name, func(t *testing.T) {
			h := newStoreHarness(t, tc.build)

			storeID := registry.NewID("test", "kvstore")
			h.commit(t, func(ctx context.Context) error {
				return h.mgr.Add(ctx, makeKVEntry(storeID, tc.kind, "before", attrs.NewBagFrom(map[string]any{
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
				return h.mgr.Update(ctx, makeKVEntry(storeID, tc.kind, "after", attrs.NewBagFrom(map[string]any{
					"module_version": "1.1.0",
				})))
			})

			updated := h.current(storeID)
			require.NotNil(t, updated)
			require.NotSame(t, original, updated)

			got, err = h.awaitProvider(t, storeID, any(updated))
			require.NoError(t, err)
			require.Same(t, updated, got)

			require.True(t, h.awaitClosed(t, original, true), "superseded view must be stopped")
			require.False(t, h.awaitClosed(t, updated, false), "replacement view must be running")
			require.Equal(t, supervisorapi.StatusRunning, h.awaitStatus(t, storeID, supervisorapi.StatusRunning))

			// Stopping the service through the supervisor must reach the
			// replacement, which proves the supervisor adopted it rather than
			// the superseded view.
			h.bus.Send(context.Background(), event.Event{
				System: supervisorapi.System,
				Kind:   supervisorapi.ServiceStop,
				Path:   storeID.String(),
			})
			require.True(t, h.awaitClosed(t, updated, true), "supervisor must supervise the replacement view")
		})
	}
}

// TestManagers_UpdateEmitResourceEntryShape pins the payload the resource
// registry routes on, including the metadata carried from the entry.
func TestManagers_UpdateEmitResourceEntryShape(t *testing.T) {
	for _, tc := range kvManagerCases() {
		t.Run(tc.name, func(t *testing.T) {
			transcoder := payloadSystem.GlobalTranscoder()
			json.Register(transcoder)

			ctx := ackCtx()
			bus := &recordingBus{}
			mgr, current := tc.build(systemkv.NewCRDTEngine("test-node", bus, zap.NewNop()), bus, transcoder)

			storeID := registry.NewID("test", "kvstore")
			meta := attrs.NewBagFrom(map[string]any{"module_version": "1.1.0"})

			require.NoError(t, mgr.Add(ctx, makeKVEntry(storeID, tc.kind, "before", nil)))
			bus.clear()

			require.NoError(t, mgr.Update(ctx, makeKVEntry(storeID, tc.kind, "after", meta)))

			events := bus.recorded()
			require.Len(t, events, 2)

			// The confirmed resource repoint is published before the supervisor
			// is told to adopt the replacement, so the registry is never behind
			// the retirement.
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
			assert.Same(t, current(storeID), entry.Provider)
		})
	}
}

// TestManagers_UpdateFailWhenHandoverIsNotConfirmed covers an update whose
// resource repoint never lands: the manager must report the failure and keep
// serving the view it had, rather than claim a replacement nobody is serving.
func TestManagers_UpdateFailWhenHandoverIsNotConfirmed(t *testing.T) {
	for _, tc := range kvManagerCases() {
		t.Run(tc.name, func(t *testing.T) {
			transcoder := payloadSystem.GlobalTranscoder()
			json.Register(transcoder)

			bus := &recordingBus{}
			mgr, current := tc.build(systemkv.NewCRDTEngine("test-node", bus, zap.NewNop()), bus, transcoder)

			storeID := registry.NewID("test", "kvstore")
			require.NoError(t, mgr.Add(ackCtx(), makeKVEntry(storeID, tc.kind, "before", nil)))
			original := current(storeID)

			awaitSvc := eventbus.NewAwaitService(eventbus.NewBus())
			base := event.WithAwaitService(ctxapi.NewRootContext(), awaitSvc)
			require.NoError(t, awaitSvc.Start(base))
			t.Cleanup(func() {
				require.NoError(t, awaitSvc.Stop())
			})

			ctx, cancel := context.WithCancel(base)
			cancel()

			err := mgr.Update(ctx, makeKVEntry(storeID, tc.kind, "after", nil))
			require.Error(t, err)
			require.ErrorIs(t, err, context.Canceled)
			assert.Same(t, original, current(storeID), "an unconfirmed update must not swap the store")
		})
	}
}

// TestManagers_UpdateFailWithoutCoordination keeps an update from silently
// half-applying where the handover cannot be confirmed at all.
func TestManagers_UpdateFailWithoutCoordination(t *testing.T) {
	for _, tc := range kvManagerCases() {
		t.Run(tc.name, func(t *testing.T) {
			transcoder := payloadSystem.GlobalTranscoder()
			json.Register(transcoder)

			bus := &recordingBus{}
			mgr, current := tc.build(systemkv.NewCRDTEngine("test-node", bus, zap.NewNop()), bus, transcoder)

			storeID := registry.NewID("test", "kvstore")
			require.NoError(t, mgr.Add(ackCtx(), makeKVEntry(storeID, tc.kind, "before", nil)))
			original := current(storeID)

			err := mgr.Update(context.Background(), makeKVEntry(storeID, tc.kind, "after", nil))
			require.ErrorIs(t, err, storesvc.ErrResourceCoordinationUnavailable)
			assert.Same(t, original, current(storeID), "an unconfirmed update must not swap the store")
		})
	}
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
