package security

import (
	"context"
	"testing"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/security"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

type mockEventBus struct {
	events []event.Event
}

func (m *mockEventBus) Publish(e event.Event) error {
	m.events = append(m.events, e)
	return nil
}

func (m *mockEventBus) Subscribe(ctx context.Context, system event.System, ch chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}

func (m *mockEventBus) SubscribeP(ctx context.Context, system event.System, kind event.Kind, ch chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}

func (m *mockEventBus) Unsubscribe(ctx context.Context, id event.SubscriberID) {
}

func (m *mockEventBus) Send(ctx context.Context, e event.Event) {
	m.events = append(m.events, e)
}

func TestPolicyRegistry_StartStop(t *testing.T) {
	bus := &mockEventBus{}
	logger := zap.NewNop()
	reg := NewPolicyRegistry(bus, logger)

	// Test Start
	err := reg.Start(context.Background())
	assert.NoError(t, err, "Start should not return error")
	assert.NotNil(t, reg.subscriber, "Subscriber should be created")

	// Test Stop
	err = reg.Stop()
	assert.NoError(t, err, "Stop should not return error")
}

func TestPolicyRegistry_ListGroupsAndPolicies(t *testing.T) {
	bus := &mockEventBus{}
	logger := zap.NewNop()
	reg := NewPolicyRegistry(bus, logger)

	// Add some test policies and groups
	policy1 := NewMockPolicy("test", "policy1", security.Allow)
	policy2 := NewMockPolicy("test", "policy2", security.Deny)
	groupID := registry.ID{NS: "test", Name: "group1"}

	// Register policies
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

	// Test ListGroups
	groups := reg.ListGroups()
	assert.Len(t, groups, 1, "Should have one group")
	assert.Equal(t, groupID, groups[0], "Group ID should match")

	// Test ListPolicies
	policies := reg.ListPolicies()
	assert.Len(t, policies, 2, "Should have two policies")
	policyIDs := map[registry.ID]bool{
		policy1.ID(): true,
		policy2.ID(): true,
	}
	for _, id := range policies {
		assert.True(t, policyIDs[id], "Policy ID should be in list")
	}
}

func TestPolicyRegistry_GetPolicyAndGroup(t *testing.T) {
	bus := &mockEventBus{}
	logger := zap.NewNop()
	reg := NewPolicyRegistry(bus, logger)

	// Add a test policy
	policy := NewMockPolicy("test", "policy1", security.Allow)
	groupID := registry.ID{NS: "test", Name: "group1"}

	reg.handleEvent(event.Event{
		Kind: security.PolicyRegister,
		Path: policy.ID().String(),
		Data: &security.PolicyEntry{
			Policy: policy,
			Groups: []registry.ID{groupID},
		},
	})

	// Test GetPolicy success
	retrievedPolicy, err := reg.GetPolicy(policy.ID())
	assert.NoError(t, err, "GetPolicy should not return error")
	assert.Equal(t, policy.ID(), retrievedPolicy.ID(), "Policy ID should match")

	// Test GetPolicy not found
	nonExistentID := registry.ID{NS: "test", Name: "nonexistent"}
	_, err = reg.GetPolicy(nonExistentID)
	assert.Error(t, err, "GetPolicy should return error for non-existent policy")
	assert.Equal(t, security.ErrPolicyNotFound, err, "Error should be ErrPolicyNotFound")

	// Test GetPolicyGroup success
	scope, err := reg.GetPolicyGroup(groupID)
	assert.NoError(t, err, "GetPolicyGroup should not return error")
	assert.NotNil(t, scope, "Scope should not be nil")
	policies := scope.Policies()
	assert.Len(t, policies, 1, "Scope should have one policy")
	assert.Equal(t, policy.ID(), policies[0].ID(), "Policy ID should match")

	// Test GetPolicyGroup not found
	nonExistentGroup := registry.ID{NS: "test", Name: "nonexistent"}
	_, err = reg.GetPolicyGroup(nonExistentGroup)
	assert.Error(t, err, "GetPolicyGroup should return error for non-existent group")
	assert.Equal(t, security.ErrGroupNotFound, err, "Error should be ErrGroupNotFound")
}

func TestPolicyRegistry_EventHandling(t *testing.T) {
	bus := &mockEventBus{}
	logger := zap.NewNop()
	reg := NewPolicyRegistry(bus, logger)

	policy := NewMockPolicy("test", "policy1", security.Allow)
	groupID := registry.ID{NS: "test", Name: "group1"}

	// Test policy registration
	reg.handleEvent(event.Event{
		Kind: security.PolicyRegister,
		Path: policy.ID().String(),
		Data: &security.PolicyEntry{
			Policy: policy,
			Groups: []registry.ID{groupID},
		},
	})

	// Verify policy was registered
	retrievedPolicy, err := reg.GetPolicy(policy.ID())
	assert.NoError(t, err)
	assert.Equal(t, policy.ID(), retrievedPolicy.ID())
	initialResult := retrievedPolicy.Evaluate(security.Actor{}, "", "", nil)
	t.Logf("Initial policy result: %d", initialResult)

	// Test policy update
	updatedPolicy := NewMockPolicy("test", "policy1", security.Deny)
	reg.handleEvent(event.Event{
		Kind: security.PolicyUpdate,
		Path: policy.ID().String(),
		Data: &security.PolicyEntry{
			Policy: updatedPolicy,
			Groups: []registry.ID{groupID},
		},
	})

	// Verify policy was updated
	retrievedPolicy, err = reg.GetPolicy(policy.ID())
	assert.NoError(t, err)
	result := retrievedPolicy.Evaluate(security.Actor{}, "", "", nil)
	t.Logf("Updated policy result: %d (expected Deny: %d)", result, security.Deny)
	assert.Equal(t, security.Deny, result, "Policy should return Deny")

	// Test policy deletion
	reg.handleEvent(event.Event{
		Kind: security.PolicyDelete,
		Path: policy.ID().String(),
	})

	// Verify policy was deleted
	_, err = reg.GetPolicy(policy.ID())
	assert.Error(t, err)
	assert.Equal(t, security.ErrPolicyNotFound, err)

	// Verify group was also cleaned up
	_, err = reg.GetPolicyGroup(groupID)
	assert.Error(t, err)
	assert.Equal(t, security.ErrGroupNotFound, err)
}
