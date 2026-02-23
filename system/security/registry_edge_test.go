// SPDX-License-Identifier: MPL-2.0

package security

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
	"go.uber.org/zap"
)

// --- NewPolicyRegistry ---

func TestNewPolicyRegistry_NilLogger(t *testing.T) {
	reg := NewPolicyRegistry(&mockEventBus{}, nil)
	require.NotNil(t, reg)
	assert.NotNil(t, reg.logger)
}

// --- handleEvent unknown kind ---

func TestPolicyRegistry_HandleEvent_UnknownKind(t *testing.T) {
	reg := newTestRegistry(t)
	reg.handleEvent(event.Event{Kind: "unknown.event", Path: "test"})
	assert.Empty(t, reg.ListPolicies())
}

// --- registerPolicy with invalid data ---

func TestPolicyRegistry_RegisterPolicy_InvalidData(t *testing.T) {
	reg := newTestRegistry(t)
	reg.handleEvent(event.Event{
		Kind: security.PolicyRegister,
		Path: "test:policy1",
		Data: "not a PolicyEntry",
	})
	assert.Empty(t, reg.ListPolicies())
}

// --- updatePolicy with invalid data ---

func TestPolicyRegistry_UpdatePolicy_InvalidData(t *testing.T) {
	reg := newTestRegistry(t)

	policy := newMockPolicy("policy1", security.Allow)
	policyID := policy.ID()
	reg.handleEvent(event.Event{
		Kind: security.PolicyRegister,
		Path: policyID.String(),
		Data: &security.PolicyEntry{Policy: policy, Groups: nil},
	})

	reg.handleEvent(event.Event{
		Kind: security.PolicyUpdate,
		Path: policyID.String(),
		Data: "not a PolicyEntry",
	})

	// original policy should still be there
	retrieved, err := reg.GetPolicy(policyID)
	require.NoError(t, err)
	assert.Equal(t, security.Allow, retrieved.Evaluate(security.Actor{}, "", "", nil))
}

// --- updatePolicy for non-existent policy ---

func TestPolicyRegistry_UpdatePolicy_NotFound(t *testing.T) {
	reg := newTestRegistry(t)
	policy := newMockPolicy("nonexistent", security.Allow)
	policyID := policy.ID()

	reg.handleEvent(event.Event{
		Kind: security.PolicyUpdate,
		Path: policyID.String(),
		Data: &security.PolicyEntry{Policy: policy, Groups: nil},
	})

	_, err := reg.GetPolicy(policyID)
	assert.Equal(t, security.ErrPolicyNotFound, err)
}

// --- deletePolicy for non-existent policy ---

func TestPolicyRegistry_DeletePolicy_NotFound(t *testing.T) {
	reg := newTestRegistry(t)

	reg.handleEvent(event.Event{
		Kind: security.PolicyDelete,
		Path: "test:nonexistent",
	})
	assert.Empty(t, reg.ListPolicies())
}

// --- addPolicyToGroup idempotency ---

func TestPolicyRegistry_AddPolicyToGroup_Idempotent(t *testing.T) {
	reg := newTestRegistry(t)
	policy := newMockPolicy("policy1", security.Allow)
	policyID := policy.ID()
	groupID := registry.NewID("test", "group1")

	reg.handleEvent(event.Event{
		Kind: security.PolicyRegister,
		Path: policyID.String(),
		Data: &security.PolicyEntry{
			Policy: policy,
			Groups: []registry.ID{groupID},
		},
	})

	// re-register same policy in same group via update
	reg.handleEvent(event.Event{
		Kind: security.PolicyUpdate,
		Path: policyID.String(),
		Data: &security.PolicyEntry{
			Policy: policy,
			Groups: []registry.ID{groupID},
		},
	})

	scope, err := reg.GetPolicyGroup(groupID)
	require.NoError(t, err)
	assert.Len(t, scope.Policies(), 1)
}

// --- updatePolicy group migration ---

func TestPolicyRegistry_UpdatePolicy_GroupMigration(t *testing.T) {
	reg := newTestRegistry(t)
	policy := newMockPolicy("policy1", security.Allow)
	policyID := policy.ID()
	groupA := registry.NewID("test", "groupA")
	groupB := registry.NewID("test", "groupB")

	reg.handleEvent(event.Event{
		Kind: security.PolicyRegister,
		Path: policyID.String(),
		Data: &security.PolicyEntry{
			Policy: policy,
			Groups: []registry.ID{groupA},
		},
	})

	scopeA, err := reg.GetPolicyGroup(groupA)
	require.NoError(t, err)
	assert.Len(t, scopeA.Policies(), 1)

	// move from groupA to groupB
	reg.handleEvent(event.Event{
		Kind: security.PolicyUpdate,
		Path: policyID.String(),
		Data: &security.PolicyEntry{
			Policy: policy,
			Groups: []registry.ID{groupB},
		},
	})

	_, err = reg.GetPolicyGroup(groupA)
	assert.Equal(t, security.ErrGroupNotFound, err)

	scopeB, err := reg.GetPolicyGroup(groupB)
	require.NoError(t, err)
	assert.Len(t, scopeB.Policies(), 1)
}

// --- deletePolicy removes from all groups ---

func TestPolicyRegistry_DeletePolicy_RemovesFromGroups(t *testing.T) {
	reg := newTestRegistry(t)

	policy1 := newMockPolicy("policy1", security.Allow)
	policy2 := newMockPolicy("policy2", security.Deny)
	p1ID := policy1.ID()
	p2ID := policy2.ID()
	groupID := registry.NewID("test", "group1")

	reg.handleEvent(event.Event{
		Kind: security.PolicyRegister,
		Path: p1ID.String(),
		Data: &security.PolicyEntry{
			Policy: policy1,
			Groups: []registry.ID{groupID},
		},
	})

	reg.handleEvent(event.Event{
		Kind: security.PolicyRegister,
		Path: p2ID.String(),
		Data: &security.PolicyEntry{
			Policy: policy2,
			Groups: []registry.ID{groupID},
		},
	})

	scope, err := reg.GetPolicyGroup(groupID)
	require.NoError(t, err)
	assert.Len(t, scope.Policies(), 2)

	reg.handleEvent(event.Event{
		Kind: security.PolicyDelete,
		Path: p1ID.String(),
	})

	scope, err = reg.GetPolicyGroup(groupID)
	require.NoError(t, err)
	assert.Len(t, scope.Policies(), 1)
}

// --- GetPolicyGroup with missing policy reference ---

func TestPolicyRegistry_GetPolicyGroup_MissingPolicyReference(t *testing.T) {
	reg := newTestRegistry(t)
	groupID := registry.NewID("test", "group1")
	policyID := registry.NewID("test", "missing-policy")

	// manually inject a group with a non-existent policy reference
	reg.groups.Store(groupID, []registry.ID{policyID})

	scope, err := reg.GetPolicyGroup(groupID)
	require.NoError(t, err)
	assert.Empty(t, scope.Policies())
}

// --- register policy in multiple groups ---

func TestPolicyRegistry_RegisterPolicy_MultipleGroups(t *testing.T) {
	reg := newTestRegistry(t)
	policy := newMockPolicy("shared", security.Allow)
	policyID := policy.ID()
	groupA := registry.NewID("test", "groupA")
	groupB := registry.NewID("test", "groupB")

	reg.handleEvent(event.Event{
		Kind: security.PolicyRegister,
		Path: policyID.String(),
		Data: &security.PolicyEntry{
			Policy: policy,
			Groups: []registry.ID{groupA, groupB},
		},
	})

	scopeA, err := reg.GetPolicyGroup(groupA)
	require.NoError(t, err)
	assert.Len(t, scopeA.Policies(), 1)

	scopeB, err := reg.GetPolicyGroup(groupB)
	require.NoError(t, err)
	assert.Len(t, scopeB.Policies(), 1)

	groups := reg.ListGroups()
	assert.Len(t, groups, 2)
}

// --- NewSubscriberError ---

func TestNewSubscriberError(t *testing.T) {
	cause := assert.AnError
	err := NewSubscriberError(cause)
	assert.Contains(t, err.Error(), "subscriber")
}

// --- Dispatcher ---

func TestNewDispatcher_DefaultWorkers(t *testing.T) {
	d := NewDispatcher(0)
	assert.Equal(t, 4, d.workers)
}

func TestNewDispatcher_NegativeWorkers(t *testing.T) {
	d := NewDispatcher(-1)
	assert.Equal(t, 4, d.workers)
}

func TestNewDispatcher_CustomWorkers(t *testing.T) {
	d := NewDispatcher(8)
	assert.Equal(t, 8, d.workers)
}

// --- Scope additional tests ---

func TestScope_EvaluateAllowPrecedence(t *testing.T) {
	policies := []security.Policy{
		newMockPolicy("p1", security.Allow),
		newMockPolicy("p2", security.Undefined),
		newMockPolicy("p3", security.Allow),
	}
	s := NewScope(policies)
	result := s.Evaluate(security.Actor{}, "act", "res", nil)
	assert.Equal(t, security.Allow, result)
}

func TestScope_WithDuplicatePolicy(t *testing.T) {
	policy := newMockPolicy("p1", security.Allow)
	s := NewScope([]security.Policy{policy})
	s2 := s.With(policy)
	assert.Len(t, s2.Policies(), 1)
}

// --- WithSecurityConfig integration ---

func TestWithSecurityConfig_WithRegisteredPolicies(t *testing.T) {
	bus := &mockEventBus{}
	reg := NewPolicyRegistry(bus, zap.NewNop())

	policy := newMockPolicy("policy1", security.Allow)
	policyID := policy.ID()
	groupID := registry.NewID("test", "group1")

	reg.handleEvent(event.Event{
		Kind: security.PolicyRegister,
		Path: policyID.String(),
		Data: &security.PolicyEntry{
			Policy: policy,
			Groups: []registry.ID{groupID},
		},
	})

	rootCtx := newFrameWithRegistry(t, reg)

	config := &security.Config{
		Actor:        security.Actor{ID: "user1"},
		PolicyGroups: []registry.ID{groupID},
	}

	result := WithSecurityConfig(rootCtx, config)
	actor, ok := security.GetActor(result)
	assert.True(t, ok)
	assert.Equal(t, "user1", actor.ID)

	scope, ok := security.GetScope(result)
	assert.True(t, ok)
	assert.Len(t, scope.Policies(), 1)
}

func TestWithSecurityConfig_WithDirectPolicy(t *testing.T) {
	bus := &mockEventBus{}
	reg := NewPolicyRegistry(bus, zap.NewNop())

	policy := newMockPolicy("policy1", security.Deny)
	policyID := policy.ID()
	reg.handleEvent(event.Event{
		Kind: security.PolicyRegister,
		Path: policyID.String(),
		Data: &security.PolicyEntry{Policy: policy, Groups: nil},
	})

	rootCtx := newFrameWithRegistry(t, reg)

	config := &security.Config{
		Actor:    security.Actor{ID: "user1"},
		Policies: []registry.ID{policyID},
	}

	result := WithSecurityConfig(rootCtx, config)
	scope, ok := security.GetScope(result)
	assert.True(t, ok)
	policies := scope.Policies()
	assert.Len(t, policies, 1)
	assert.Equal(t, security.Deny, policies[0].Evaluate(security.Actor{}, "", "", nil))
}

func TestWithSecurityConfig_NilConfig(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	result := WithSecurityConfig(ctx, nil)
	assert.Equal(t, ctx, result)
}

func TestWithSecurityConfig_NoRegistry(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx, fc := ctxapi.OpenFrameContext(ctx)
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(fc) })

	config := &security.Config{
		Actor: security.Actor{ID: "user1"},
	}

	result := WithSecurityConfig(ctx, config)
	// actor is set on frame context, but no registry means no policy resolution
	_, ok := security.GetScope(result)
	assert.False(t, ok)
}

func newFrameWithRegistry(t *testing.T, reg security.Registry) context.Context {
	t.Helper()
	ctx := ctxapi.NewRootContext()
	security.WithRegistry(ctx, reg)
	ctx, fc := ctxapi.OpenFrameContext(ctx)
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(fc) })
	return ctx
}
