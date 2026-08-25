// SPDX-License-Identifier: MPL-2.0

package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	storesvc "github.com/wippyai/runtime/service/store"
	"github.com/wippyai/runtime/system/eventbus"
	systemresource "github.com/wippyai/runtime/system/resource"
	"go.uber.org/zap"
)

type stubProvider struct{}

func (stubProvider) Acquire(_ context.Context, _ registry.ID, _ resource.AccessMode) (resource.Resource[any], error) {
	return nil, nil
}

func awaitTestContext(t *testing.T) (context.Context, event.Bus) {
	t.Helper()

	bus := eventbus.NewBus()
	awaitSvc := eventbus.NewAwaitService(bus)
	ctx := event.WithAwaitService(ctxapi.NewRootContext(), awaitSvc)
	require.NoError(t, awaitSvc.Start(ctx))
	t.Cleanup(func() {
		require.NoError(t, awaitSvc.Stop())
	})

	return ctx, bus
}

// TestAwaitResourceUpdate_IgnoresOutcomesOfOtherOperations covers the
// correlation the wait depends on. Registrations and updates for one resource
// all travel under the same path, so an outcome published for the resource
// rather than for this operation must never be taken as this operation's
// confirmation.
func TestAwaitResourceUpdate_IgnoresOutcomesOfOtherOperations(t *testing.T) {
	ctx, bus := awaitTestContext(t)

	storeID := registry.NewID("test", "cache")

	// Nothing applies the update: the only traffic is a stream of outcomes for
	// other operations on the same resource, exactly what ordinary registration
	// produces.
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				bus.Send(ctx, event.Event{
					System: resource.System,
					Kind:   resource.Accept,
					Path:   storeID.String(),
				})
				time.Sleep(time.Millisecond)
			}
		}
	}()

	waitCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()

	err := storesvc.AwaitResourceUpdate(waitCtx, bus, resource.Entry{
		ID:       storeID,
		Provider: stubProvider{},
	}, "test update")

	require.Error(t, err, "an unrelated outcome must not confirm this operation")
}

// TestAwaitResourceUpdate_ConfirmsAppliedUpdate covers the confirming path
// against the real registry.
func TestAwaitResourceUpdate_ConfirmsAppliedUpdate(t *testing.T) {
	ctx, bus := awaitTestContext(t)

	reg := systemresource.NewRegistry(bus, zap.NewNop())
	require.NoError(t, reg.Start(ctx))
	t.Cleanup(func() {
		require.NoError(t, reg.Stop())
	})

	storeID := registry.NewID("test", "cache")
	bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Register,
		Path:   storeID.String(),
		Data:   resource.Entry{ID: storeID, Provider: stubProvider{}},
	})
	require.Eventually(t, func() bool {
		return reg.Exists(storeID)
	}, 2*time.Second, 5*time.Millisecond)

	require.NoError(t, storesvc.AwaitResourceUpdate(ctx, bus, resource.Entry{
		ID:       storeID,
		Provider: stubProvider{},
	}, "test update"))
}

// TestAwaitResourceUpdate_ReportsRejection covers an update the registry
// declines, so the caller fails instead of waiting out the budget.
func TestAwaitResourceUpdate_ReportsRejection(t *testing.T) {
	ctx, bus := awaitTestContext(t)

	reg := systemresource.NewRegistry(bus, zap.NewNop())
	require.NoError(t, reg.Start(ctx))
	t.Cleanup(func() {
		require.NoError(t, reg.Stop())
	})

	// Never registered, so the update has nothing to repoint.
	err := storesvc.AwaitResourceUpdate(ctx, bus, resource.Entry{
		ID:       registry.NewID("test", "missing"),
		Provider: stubProvider{},
	}, "test update")

	require.Error(t, err)
	require.Contains(t, err.Error(), "handover")
}
