// SPDX-License-Identifier: MPL-2.0
package policy

import (
	"context"
	"errors"
	"testing"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
	policyapi "github.com/wippyai/runtime/api/service/security/policy"
	"github.com/wippyai/runtime/system/eventbus"
	policyregistry "github.com/wippyai/runtime/system/security"
	"go.uber.org/zap"
)

type canceledDeliveryBus struct {
	event.Bus
	cancel context.CancelFunc
}

func (b *canceledDeliveryBus) Send(ctx context.Context, e event.Event) {
	if e.System == security.System {
		b.cancel()
		return
	}
	b.Bus.Send(ctx, e)
}

func TestManagerNeverAcknowledgesUndeliveredPolicy(t *testing.T) {
	for _, operation := range []string{"add", "update", "delete"} {
		t.Run(operation, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			bus := &canceledDeliveryBus{Bus: eventbus.NewBus(), cancel: cancel}
			pending := make(chan event.Event, 1)
			id, err := bus.Subscribe(ctx, security.System, pending)
			if err != nil {
				t.Fatal(err)
			}
			defer bus.Unsubscribe(context.Background(), id)
			manager := NewManager(bus, &mockFactory{}, zap.NewNop())
			entry := registry.Entry{ID: registry.NewID("test", "first_request"), Kind: policyapi.Policy}
			switch operation {
			case "add":
				err = manager.Add(ctx, entry)
			case "update":
				err = manager.Update(ctx, entry)
			case "delete":
				err = manager.Delete(ctx, entry)
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("%s acknowledged an undelivered policy: %v", operation, err)
			}
		})
	}
}

func TestManagerCompletionIncludesPolicyVisibility(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	owner := policyregistry.NewPolicyRegistry(bus, zap.NewNop())
	if err := owner.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer owner.Stop()
	manager := NewManager(bus, &mockFactory{}, zap.NewNop())
	entry := registry.Entry{ID: registry.NewID("test", "first_request"), Kind: policyapi.Policy}
	if err := manager.Add(ctx, entry); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.GetPolicy(entry.ID); err != nil {
		t.Fatalf("first lookup after add: %v", err)
	}
	if err := manager.Update(ctx, entry); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.GetPolicy(entry.ID); err != nil {
		t.Fatalf("first lookup after update: %v", err)
	}
	if err := manager.Delete(ctx, entry); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.GetPolicy(entry.ID); !errors.Is(err, security.ErrPolicyNotFound) {
		t.Fatalf("first lookup after delete: %v", err)
	}
	if err := manager.Update(ctx, entry); err == nil || err.Error() != "policy not found for update: test:first_request" {
		t.Fatalf("update owner failure was lost: %v", err)
	}
	if err := manager.Delete(ctx, entry); err == nil || err.Error() != "policy not found for deletion: test:first_request" {
		t.Fatalf("delete owner failure was lost: %v", err)
	}
}

func TestManagerRefusesAbsentPolicyOwner(t *testing.T) {
	manager := NewManager(eventbus.NewBus(), &mockFactory{}, zap.NewNop())
	entry := registry.Entry{ID: registry.NewID("test", "first_request"), Kind: policyapi.Policy}
	if err := manager.Add(context.Background(), entry); err == nil || err.Error() != "security policy registry is not subscribed" {
		t.Fatalf("missing owner failure was lost: %v", err)
	}
}
