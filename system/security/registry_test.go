package security

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
	"go.uber.org/zap"
)

type mockEventBus struct {
	events []event.Event
}

func (m *mockEventBus) Publish(e event.Event) error {
	m.events = append(m.events, e)
	return nil
}

func (m *mockEventBus) Subscribe(_ context.Context, _ event.System, _ chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}

func (m *mockEventBus) SubscribeP(_ context.Context, _ event.System, _ event.Kind, _ chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}

func (m *mockEventBus) Unsubscribe(_ context.Context, _ event.SubscriberID) {
}

func (m *mockEventBus) Send(_ context.Context, e event.Event) {
	m.events = append(m.events, e)
}

func newTestRegistry(*testing.T) *PolicyRegistry {
	bus := &mockEventBus{}
	logger := zap.NewNop()
	reg := NewPolicyRegistry(bus, logger)
	return reg
}

func TestPolicyRegistry_StartStop(t *testing.T) {
	reg := newTestRegistry(t)

	err := reg.Start(context.Background())
	assert.NoError(t, err)
	assert.NotNil(t, reg.subscriber)

	err = reg.Stop()
	assert.NoError(t, err)
}

func TestPolicyRegistry_ListGroupsAndPolicies(t *testing.T) {
	reg := newTestRegistry(t)

	policy1 := newMockPolicy("test", "policy1", security.Allow)
	policy2 := newMockPolicy("test", "policy2", security.Deny)
	groupID := registry.NewID("test", "group1")

	reg.handleEvent(event.Event{
		Kind: security.PolicyRegister,
		Path: policy1.ID().String(),
		Data: &security.PolicyEntry{
			Policy: policy1,
			Groups: []registry.ID{groupID},
		},
	})

	reg.handleEvent(event.Event{
		Kind: security.PolicyRegister,
		Path: policy2.ID().String(),
		Data: &security.PolicyEntry{
			Policy: policy2,
			Groups: []registry.ID{groupID},
		},
	})

	groups := reg.ListGroups()
	assert.Len(t, groups, 1)
	assert.Equal(t, groupID, groups[0])

	policies := reg.ListPolicies()
	assert.Len(t, policies, 2)
	policyIDs := map[registry.ID]bool{
		policy1.ID(): true,
		policy2.ID(): true,
	}
	for _, id := range policies {
		assert.True(t, policyIDs[id])
	}
}

func TestPolicyRegistry_GetPolicyAndGroup(t *testing.T) {
	reg := newTestRegistry(t)

	policy := newMockPolicy("test", "policy1", security.Allow)
	groupID := registry.NewID("test", "group1")

	reg.handleEvent(event.Event{
		Kind: security.PolicyRegister,
		Path: policy.ID().String(),
		Data: &security.PolicyEntry{
			Policy: policy,
			Groups: []registry.ID{groupID},
		},
	})

	_, err := reg.GetPolicy(policy.ID())
	assert.NoError(t, err)

	nonExistentID := registry.NewID("test", "nonexistent")
	_, err = reg.GetPolicy(nonExistentID)
	assert.Error(t, err)
	assert.Equal(t, security.ErrPolicyNotFound, err)

	scope, err := reg.GetPolicyGroup(groupID)
	assert.NoError(t, err)
	assert.NotNil(t, scope)
	policies := scope.Policies()
	assert.Len(t, policies, 1)

	nonExistentGroup := registry.NewID("test", "nonexistent")
	_, err = reg.GetPolicyGroup(nonExistentGroup)
	assert.Error(t, err)
	assert.Equal(t, security.ErrGroupNotFound, err)
}

func TestPolicyRegistry_EventHandling(t *testing.T) {
	reg := newTestRegistry(t)

	policy := newMockPolicy("test", "policy1", security.Allow)
	groupID := registry.NewID("test", "group1")

	reg.handleEvent(event.Event{
		Kind: security.PolicyRegister,
		Path: policy.ID().String(),
		Data: &security.PolicyEntry{
			Policy: policy,
			Groups: []registry.ID{groupID},
		},
	})

	retrievedPolicy, err := reg.GetPolicy(policy.ID())
	assert.NoError(t, err)
	initialResult := retrievedPolicy.Evaluate(security.Actor{}, "", "", nil)
	assert.Equal(t, security.Allow, initialResult)

	updatedPolicy := newMockPolicy("test", "policy1", security.Deny)
	reg.handleEvent(event.Event{
		Kind: security.PolicyUpdate,
		Path: policy.ID().String(),
		Data: &security.PolicyEntry{
			Policy: updatedPolicy,
			Groups: []registry.ID{groupID},
		},
	})

	retrievedPolicy, err = reg.GetPolicy(policy.ID())
	assert.NoError(t, err)
	result := retrievedPolicy.Evaluate(security.Actor{}, "", "", nil)
	assert.Equal(t, security.Deny, result)

	reg.handleEvent(event.Event{
		Kind: security.PolicyDelete,
		Path: policy.ID().String(),
	})

	_, err = reg.GetPolicy(policy.ID())
	assert.Error(t, err)
	assert.Equal(t, security.ErrPolicyNotFound, err)

	_, err = reg.GetPolicyGroup(groupID)
	assert.Error(t, err)
	assert.Equal(t, security.ErrGroupNotFound, err)
}
