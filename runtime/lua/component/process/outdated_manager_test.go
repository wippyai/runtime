// SPDX-License-Identifier: MPL-2.0

package process

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/runtime/lua/code"
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
