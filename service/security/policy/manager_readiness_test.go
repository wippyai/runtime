// SPDX-License-Identifier: MPL-2.0

package policy

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
	policyapi "github.com/wippyai/runtime/api/service/security/policy"
	"github.com/wippyai/runtime/system/eventbus"
	policyregistry "github.com/wippyai/runtime/system/security"
	"go.uber.org/zap"
)

type policyDeliveryBus struct {
	event.Bus
	beforeSend func()
}

func (b *policyDeliveryBus) Send(ctx context.Context, e event.Event) {
	if e.System == security.System && b.beforeSend != nil {
		b.beforeSend()
	}
	b.Bus.Send(ctx, e)
}

func readinessEntry() registry.Entry {
	return registry.Entry{ID: registry.NewID("test", "first_request"), Kind: policyapi.Policy}
}

func startPolicyOwner(t *testing.T, bus event.Bus) *policyregistry.PolicyRegistry {
	t.Helper()
	owner := policyregistry.NewPolicyRegistry(bus, zap.NewNop())
	require.NoError(t, owner.Start(context.Background()))
	t.Cleanup(func() { require.NoError(t, owner.Stop()) })
	return owner
}

func awaitPolicyResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(time.Second):
		t.Fatal("policy operation did not resolve")
		return nil
	}
}

func TestManagerNeverAcknowledgesUndeliveredPolicy(t *testing.T) {
	for _, operation := range []string{"add", "update", "delete"} {
		t.Run(operation, func(t *testing.T) {
			bus := eventbus.NewBus()
			t.Cleanup(bus.Stop)
			wrapped := &policyDeliveryBus{Bus: bus}
			owner := startPolicyOwner(t, wrapped)
			manager := NewManager(owner, &mockFactory{}, zap.NewNop())
			entry := readinessEntry()
			if operation != "add" {
				require.NoError(t, manager.Add(context.Background(), entry))
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wrapped.beforeSend = cancel
			var err error
			switch operation {
			case "add":
				err = manager.Add(ctx, entry)
			case "update":
				err = manager.Update(ctx, entry)
			case "delete":
				err = manager.Delete(ctx, entry)
			}
			require.ErrorIs(t, err, context.Canceled)
			_, err = owner.GetPolicy(entry.ID)
			if operation == "add" {
				require.ErrorIs(t, err, security.ErrPolicyNotFound)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestManagerCompletionIncludesPolicyVisibility(t *testing.T) {
	bus := eventbus.NewBus()
	t.Cleanup(bus.Stop)
	owner := startPolicyOwner(t, bus)
	group := registry.NewID("test", "old_group")
	factory := &mockFactory{createFunc: func(_ context.Context, entry registry.Entry) (*security.PolicyEntry, error) {
		return &security.PolicyEntry{Policy: &mockPolicy{id: entry.ID}, Groups: []registry.ID{group}}, nil
	}}
	manager := NewManager(owner, factory, zap.NewNop())
	entry := readinessEntry()
	require.NoError(t, manager.Add(context.Background(), entry))
	_, err := owner.GetPolicy(entry.ID)
	require.NoError(t, err)
	scope, err := owner.GetPolicyGroup(group)
	require.NoError(t, err)
	require.True(t, scope.Contains(entry.ID))
	oldGroup := group
	group = registry.NewID("test", "new_group")
	require.NoError(t, manager.Update(context.Background(), entry))
	_, err = owner.GetPolicyGroup(oldGroup)
	require.ErrorIs(t, err, security.ErrGroupNotFound)
	scope, err = owner.GetPolicyGroup(group)
	require.NoError(t, err)
	require.True(t, scope.Contains(entry.ID))
	require.NoError(t, manager.Delete(context.Background(), entry))
	_, err = owner.GetPolicy(entry.ID)
	require.ErrorIs(t, err, security.ErrPolicyNotFound)
	_, err = owner.GetPolicyGroup(group)
	require.ErrorIs(t, err, security.ErrGroupNotFound)
	require.ErrorIs(t, manager.Update(context.Background(), entry), security.ErrPolicyNotFound)
	require.ErrorIs(t, manager.Delete(context.Background(), entry), security.ErrPolicyNotFound)
}

func TestManagerRejectsMissingOwnerEvenWithObserver(t *testing.T) {
	bus := eventbus.NewBus()
	t.Cleanup(bus.Stop)
	observed := make(chan event.Event, 1)
	id, err := bus.Subscribe(context.Background(), security.System, observed)
	require.NoError(t, err)
	t.Cleanup(func() { bus.Unsubscribe(context.Background(), id) })
	manager := NewManager(nil, &mockFactory{}, zap.NewNop())
	done := make(chan error, 1)
	go func() { done <- manager.Add(context.Background(), readinessEntry()) }()
	require.ErrorIs(t, awaitPolicyResult(t, done), security.ErrRegistryNotFound)
	require.Empty(t, observed)
}

func TestManagerRejectsStoppedOwnerAtSend(t *testing.T) {
	bus := eventbus.NewBus()
	t.Cleanup(bus.Stop)
	wrapped := &policyDeliveryBus{Bus: bus}
	owner := startPolicyOwner(t, wrapped)
	wrapped.beforeSend = func() { _ = owner.Stop() }
	manager := NewManager(owner, &mockFactory{}, zap.NewNop())
	done := make(chan error, 1)
	go func() { done <- manager.Add(context.Background(), readinessEntry()) }()
	require.ErrorIs(t, awaitPolicyResult(t, done), policyregistry.ErrRegistryStopped)
	_, err := owner.GetPolicy(readinessEntry().ID)
	require.ErrorIs(t, err, security.ErrPolicyNotFound)
}

func TestManagerCancellationDoesNotWaitForUnrelatedDelivery(t *testing.T) {
	bus := eventbus.NewBus()
	t.Cleanup(bus.Stop)
	entered := make(chan struct{})
	wrapped := &policyDeliveryBus{Bus: bus, beforeSend: func() { close(entered) }}
	owner := startPolicyOwner(t, wrapped)
	observerCtx, releaseObserver := context.WithCancel(context.Background())
	defer releaseObserver()
	unread := make(chan event.Event)
	_, err := bus.Subscribe(observerCtx, "blocked", unread)
	require.NoError(t, err)
	bus.Send(observerCtx, event.Event{System: "blocked"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := NewManager(owner, &mockFactory{}, zap.NewNop())
	done := make(chan error, 1)
	go func() { done <- manager.Add(ctx, readinessEntry()) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("did not reach policy publication")
	}
	cancel()
	require.ErrorIs(t, awaitPolicyResult(t, done), context.Canceled)
	_, err = owner.GetPolicy(readinessEntry().ID)
	require.ErrorIs(t, err, security.ErrPolicyNotFound)
}

func TestManagerRejectsStoppedBus(t *testing.T) {
	bus := eventbus.NewBus()
	owner := startPolicyOwner(t, bus)
	bus.Stop()
	manager := NewManager(owner, &mockFactory{}, zap.NewNop())
	done := make(chan error, 1)
	go func() { done <- manager.Add(context.Background(), readinessEntry()) }()
	require.ErrorIs(t, awaitPolicyResult(t, done), policyregistry.ErrRegistryStopped)
}
