// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/component"
	"go.uber.org/zap"
)

type fakeOutdatedNotifier struct {
	affected map[registry.ID]bool
	calls    int
}

func (f *fakeOutdatedNotifier) NotifyOutdated(affected map[registry.ID]bool) {
	f.calls++
	f.affected = affected
}

func newInvalidateManager(ids ...registry.ID) *Manager {
	m := NewManager(zap.NewNop(), &code.Manager{}, &mockEventBus{}, &mockFSRegistry{}, &mockCompiledFactory{})
	for _, id := range ids {
		m.configs.Store(id, &configEntry{method: "main"})
	}
	return m
}

func TestManager_Invalidate_NotifiesReloaded(t *testing.T) {
	ids := []registry.ID{registry.NewID("app", "w1"), registry.NewID("app", "w2")}
	manager := newInvalidateManager(ids...)
	fake := &fakeOutdatedNotifier{}
	awaitSvc := &mockPrepareAwaitService{result: event.AwaitResult{Accepted: true}}
	ctx := processapi.WithOutdatedNotifier(event.WithAwaitService(ctxapi.NewRootContext(), awaitSvc), fake)

	require.NoError(t, manager.Invalidate(ctx, ids))

	require.Equal(t, 1, fake.calls)
	require.Len(t, fake.affected, 2)
	assert.True(t, fake.affected[ids[0]])
	assert.True(t, fake.affected[ids[1]])
}

// TestManager_Invalidate_FailedSwapNotNotified proves a node whose factory swap
// failed never signals OUTDATED.
func TestManager_Invalidate_FailedSwapNotNotified(t *testing.T) {
	id := registry.NewID("app", "w1")
	manager := newInvalidateManager(id)
	fake := &fakeOutdatedNotifier{}
	awaitSvc := &mockPrepareAwaitService{result: event.AwaitResult{Accepted: false, Error: fmt.Errorf("rejected")}}
	ctx := processapi.WithOutdatedNotifier(event.WithAwaitService(ctxapi.NewRootContext(), awaitSvc), fake)

	err := manager.Invalidate(ctx, []registry.ID{id})
	require.Error(t, err)
	assert.Equal(t, 0, fake.calls)
}

func TestManager_Invalidate_EmptyIDsNoNotify(t *testing.T) {
	manager := newInvalidateManager()
	fake := &fakeOutdatedNotifier{}
	ctx := processapi.WithOutdatedNotifier(ctxapi.NewRootContext(), fake)

	require.NoError(t, manager.Invalidate(ctx, nil))
	assert.Equal(t, 0, fake.calls)
}

func TestManager_Invalidate_NoNotifierNoPanic(t *testing.T) {
	manager := newInvalidateManager()
	// Root context without a registered notifier: safe no-op.
	require.NoError(t, manager.Invalidate(ctxapi.NewRootContext(), []registry.ID{registry.NewID("app", "w1")}))
}

// A process source update, or a library update affecting that process, must
// reach the process manager's notifier after the code graph is committed.
func TestManager_CodeUpdateNotifiesOutdatedProcess(t *testing.T) {
	for _, update := range []string{"process", "library"} {
		t.Run(update, func(t *testing.T) {
			bus := &mockEventBus{}
			cm, err := code.NewCodeManager(zap.NewNop(), bus, code.Config{})
			require.NoError(t, err)
			libraryID := registry.NewID("app", "lib")
			processID := registry.NewID("app", "worker")
			require.NoError(t, cm.AddNode(context.Background(), code.Node{
				ID: libraryID, Kind: luaapi.Library, Source: "return { value = 1 }",
			}, nil))
			require.NoError(t, cm.AddNode(context.Background(), code.Node{
				ID: processID, Kind: luaapi.Process, Source: "return lib.value",
			}, []code.Import{{ID: libraryID, Alias: "lib"}}))

			// The initial graph is already installed. Only the following update
			// should contribute to this transaction's invalidation event.
			require.NoError(t, cm.Begin(context.Background()))
			if update == "process" {
				require.NoError(t, cm.UpdateNode(context.Background(), code.Node{
					ID: processID, Source: "return lib.value + 1",
				}, []code.Import{{ID: libraryID, Alias: "lib"}}))
			} else {
				require.NoError(t, cm.UpdateNode(context.Background(), code.Node{
					ID: libraryID, Source: "return { value = 2 }",
				}, nil))
			}

			notifier := &fakeOutdatedNotifier{}
			awaitSvc := &mockPrepareAwaitService{result: event.AwaitResult{Accepted: true}}
			ctx := processapi.WithOutdatedNotifier(event.WithAwaitService(ctxapi.NewRootContext(), awaitSvc), notifier)
			require.NoError(t, cm.Commit(ctx))

			var invalidation event.Event
			for _, evt := range bus.events {
				if evt.System == luaapi.System && evt.Kind == luaapi.InvalidateNodes {
					invalidation = evt
				}
			}
			require.Equal(t, luaapi.InvalidateNodes, invalidation.Kind)
			req, ok := invalidation.Data.(luaapi.InvalidateNodesRequest)
			require.True(t, ok)
			require.Contains(t, req.Nodes, luaapi.InvalidateNode{ID: processID, Kind: luaapi.Process})

			manager := NewManager(zap.NewNop(), cm, bus, nil, &mockCompiledFactory{})
			manager.configs.Store(processID, &configEntry{method: "main"})
			handler := component.NewHandler(luaapi.Process, manager)
			require.NoError(t, handler.Handle(ctx, invalidation))
			require.Equal(t, 1, notifier.calls)
			assert.Equal(t, map[registry.ID]bool{processID: true}, notifier.affected)
		})
	}
}
