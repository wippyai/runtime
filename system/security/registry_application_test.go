// SPDX-License-Identifier: MPL-2.0

package security

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	securityapi "github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// heldPolicyBus captures the exact request without dispatching it. Replaying
// it after cancellation or restart exercises stale requests deterministically.
type heldPolicyBus struct {
	event.Bus
	requests chan event.Event
}

func (b *heldPolicyBus) Send(_ context.Context, e event.Event) {
	b.requests <- e
}

func receivePolicyRequest(t *testing.T, requests <-chan event.Event) event.Event {
	t.Helper()
	select {
	case e := <-requests:
		return e
	case <-time.After(time.Second):
		t.Fatal("policy request was not published")
		return event.Event{}
	}
}

func receiveApplicationResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(time.Second):
		t.Fatal("policy application did not resolve")
		return nil
	}
}

func TestPolicyRegistryRejectsCanceledAndOldOwnerRequests(t *testing.T) {
	for _, restart := range []bool{false, true} {
		t.Run(map[bool]string{false: "caller canceled", true: "owner restarted"}[restart], func(t *testing.T) {
			bus := eventbus.NewBus()
			t.Cleanup(bus.Stop)
			held := &heldPolicyBus{Bus: bus, requests: make(chan event.Event, 1)}
			owner := NewPolicyRegistry(held, zap.NewNop())
			require.NoError(t, owner.Start(context.Background()))
			t.Cleanup(func() { require.NoError(t, owner.Stop()) })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			policy := newMockPolicy("pending", securityapi.Allow)
			group := registry.NewID("test", "group")
			entry := &securityapi.PolicyEntry{Policy: policy, Groups: []registry.ID{group}}
			done := make(chan error, 1)
			go func() { done <- owner.ApplyPolicy(ctx, policy.ID(), securityapi.PolicyRegister, entry) }()
			request := receivePolicyRequest(t, held.requests)
			if restart {
				require.NoError(t, owner.Stop())
				require.NoError(t, owner.Start(context.Background()))
				require.ErrorIs(t, receiveApplicationResult(t, done), ErrRegistryStopped)
			} else {
				cancel()
				require.ErrorIs(t, receiveApplicationResult(t, done), context.Canceled)
			}
			owner.handleEvent(request)
			_, err := owner.GetPolicy(policy.ID())
			require.ErrorIs(t, err, securityapi.ErrPolicyNotFound)
			_, err = owner.GetPolicyGroup(group)
			require.ErrorIs(t, err, securityapi.ErrGroupNotFound)
			require.Empty(t, owner.applications)
		})
	}
}

func TestPolicyRegistryApplicationRejectsInvalidRequests(t *testing.T) {
	bus := eventbus.NewBus()
	t.Cleanup(bus.Stop)
	owner := NewPolicyRegistry(bus, zap.NewNop())
	policy := newMockPolicy("test", securityapi.Allow)
	entry := &securityapi.PolicyEntry{Policy: policy}
	require.ErrorIs(t, owner.ApplyPolicy(context.Background(), policy.ID(), securityapi.PolicyRegister, entry), ErrRegistryStopped)
	require.NoError(t, owner.Start(context.Background()))
	t.Cleanup(func() { require.NoError(t, owner.Stop()) })
	require.ErrorIs(t, owner.Start(context.Background()), ErrRegistryStarted)
	for _, invalid := range []*securityapi.PolicyEntry{nil, {}, {Policy: newMockPolicy("other", securityapi.Allow)}} {
		require.ErrorIs(t, owner.ApplyPolicy(context.Background(), policy.ID(), securityapi.PolicyRegister, invalid), ErrInvalidPolicyPayload)
	}
	require.ErrorIs(t, owner.ApplyPolicy(context.Background(), policy.ID(), "unknown", entry), ErrInvalidPolicyPayload)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, owner.ApplyPolicy(ctx, policy.ID(), securityapi.PolicyRegister, entry), context.Canceled)
	require.Empty(t, owner.applications)
}

func TestPolicyRegistryStoresDetachedPayloadWithoutWaiter(t *testing.T) {
	bus := eventbus.NewBus()
	t.Cleanup(bus.Stop)
	owner := NewPolicyRegistry(bus, zap.NewNop())
	require.NoError(t, owner.Start(context.Background()))
	t.Cleanup(func() { require.NoError(t, owner.Stop()) })
	policy := newMockPolicy("test", securityapi.Allow)
	group := registry.NewID("test", "group")
	entry := &securityapi.PolicyEntry{Policy: policy, Groups: []registry.ID{group}}
	require.NoError(t, owner.ApplyPolicy(context.Background(), policy.ID(), securityapi.PolicyRegister, entry))
	stored, ok := owner.policies.Load(policy.ID())
	require.True(t, ok)
	require.Nil(t, stored.(*securityapi.PolicyEntry).Applied)
	require.Nil(t, entry.Applied)
	entry.Groups[0] = registry.NewID("test", "changed")
	require.Equal(t, []registry.ID{group}, stored.(*securityapi.PolicyEntry).Groups)
	require.Empty(t, owner.applications)
}

func TestPolicyRegistryUnreadAcknowledgementAndNilPayloadDoNotStall(t *testing.T) {
	owner := newTestRegistry(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		owner.handleEvent(event.Event{Kind: securityapi.PolicyRegister, Data: (*securityapi.PolicyEntry)(nil)})
		owner.handleEvent(event.Event{Kind: securityapi.PolicyUpdate, Data: &securityapi.PolicyEntry{}})
		owner.handleEvent(event.Event{Kind: securityapi.PolicyRegister, Data: &securityapi.PolicyEntry{Applied: make(chan error)}})
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("malformed policy event blocked the owner")
	}
}

func TestPolicyRegistryConcurrentShutdownResolvesAllApplications(t *testing.T) {
	bus := eventbus.NewBus()
	t.Cleanup(bus.Stop)
	held := &heldPolicyBus{Bus: bus, requests: make(chan event.Event, 32)}
	owner := NewPolicyRegistry(held, zap.NewNop())
	require.NoError(t, owner.Start(context.Background()))
	policy := newMockPolicy("pending", securityapi.Allow)
	results := make(chan error, 32)
	for range 32 {
		go func() {
			results <- owner.ApplyPolicy(context.Background(), policy.ID(), securityapi.PolicyRegister, &securityapi.PolicyEntry{Policy: policy})
		}()
	}
	for range 32 {
		receivePolicyRequest(t, held.requests)
	}
	var stops sync.WaitGroup
	for range 4 {
		stops.Go(func() { _ = owner.Stop() })
	}
	for range 32 {
		require.ErrorIs(t, receiveApplicationResult(t, results), ErrRegistryStopped)
	}
	stops.Wait()
	require.Empty(t, owner.applications)
}

func TestPolicyRegistryOnlyAdmittingOwnerAnswers(t *testing.T) {
	bus := eventbus.NewBus()
	t.Cleanup(bus.Stop)
	owner := NewPolicyRegistry(bus, zap.NewNop())
	other := NewPolicyRegistry(bus, zap.NewNop())
	require.NoError(t, owner.Start(context.Background()))
	t.Cleanup(func() { require.NoError(t, owner.Stop()) })
	require.NoError(t, other.Start(context.Background()))
	t.Cleanup(func() { require.NoError(t, other.Stop()) })
	policy := newMockPolicy("selected", securityapi.Allow)
	require.NoError(t, owner.ApplyPolicy(context.Background(), policy.ID(), securityapi.PolicyRegister, &securityapi.PolicyEntry{Policy: policy}))
	_, err := owner.GetPolicy(policy.ID())
	require.NoError(t, err)
	_, err = other.GetPolicy(policy.ID())
	require.ErrorIs(t, err, securityapi.ErrPolicyNotFound)
}
