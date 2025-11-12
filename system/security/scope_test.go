package security

import (
	"testing"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/security"
	"github.com/stretchr/testify/assert"
)

// MockPolicy implements the security.Policy interface for testing
type MockPolicy struct {
	id       registry.ID
	decision security.Result
}

func (m *MockPolicy) ID() registry.ID {
	return m.id
}

func (m *MockPolicy) Evaluate(_ security.Actor, _, _ string, _ registry.Metadata) security.Result {
	return m.decision
}

func NewMockPolicy(ns, name string, decision security.Result) security.Policy {
	return &MockPolicy{
		id: registry.ID{
			NS:   ns,
			Name: name,
		},
		decision: decision,
	}
}

func TestNewScope(t *testing.T) {
	scope := NewScope(nil)
	assert.NotNil(t, scope)
	assert.Empty(t, scope.Policies())

	scope = NewScope([]security.Policy{})
	assert.NotNil(t, scope)
	assert.Empty(t, scope.Policies())

	policies := []security.Policy{
		NewMockPolicy("test", "policy1", security.Allow),
		NewMockPolicy("test", "policy2", security.Deny),
	}
	scope = NewScope(policies)
	assert.NotNil(t, scope)
	assert.Len(t, scope.Policies(), 2)
}

func TestScopeWith(t *testing.T) {
	policy1 := NewMockPolicy("test", "policy1", security.Allow)
	scope := NewScope([]security.Policy{policy1})

	policy2 := NewMockPolicy("test", "policy2", security.Deny)
	newScope := scope.With(policy2)

	assert.Len(t, scope.Policies(), 1)
	assert.True(t, scope.Contains(policy1.ID()))
	assert.False(t, scope.Contains(policy2.ID()))

	assert.Len(t, newScope.Policies(), 2)
	assert.True(t, newScope.Contains(policy1.ID()))
	assert.True(t, newScope.Contains(policy2.ID()))

	policy1Override := NewMockPolicy("test", "policy1", security.Deny)
	overrideScope := newScope.With(policy1Override)
	assert.Len(t, overrideScope.Policies(), 2)

	actor := security.Actor{ID: "user1"}
	result := overrideScope.Evaluate(actor, "read", "resource", nil)
	assert.Equal(t, security.Deny, result)
}

func TestScopeWithout(t *testing.T) {
	policy1 := NewMockPolicy("test", "policy1", security.Allow)
	policy2 := NewMockPolicy("test", "policy2", security.Deny)
	policies := []security.Policy{policy1, policy2}
	scope := NewScope(policies)

	policyID := policy1.ID()
	newScope := scope.Without(policyID)

	assert.Len(t, scope.Policies(), 2)
	assert.True(t, scope.Contains(policy1.ID()))
	assert.True(t, scope.Contains(policy2.ID()))

	assert.Len(t, newScope.Policies(), 1)
	assert.False(t, newScope.Contains(policy1.ID()))
	assert.True(t, newScope.Contains(policy2.ID()))

	nonExistentID := registry.ID{NS: "test", Name: "nonexistent"}
	sameScope := newScope.Without(nonExistentID)
	assert.Equal(t, newScope, sameScope)
}

func TestScopeEvaluate(t *testing.T) {
	actor := security.Actor{ID: "user1"}
	meta := registry.Metadata{}

	tests := []struct {
		name     string
		policies []security.Policy
		expected security.Result
	}{
		{
			name:     "Empty scope",
			policies: []security.Policy{},
			expected: security.Undefined,
		},
		{
			name: "All undefined",
			policies: []security.Policy{
				NewMockPolicy("test", "policy1", security.Undefined),
				NewMockPolicy("test", "policy2", security.Undefined),
			},
			expected: security.Undefined,
		},
		{
			name: "One allow",
			policies: []security.Policy{
				NewMockPolicy("test", "policy1", security.Allow),
			},
			expected: security.Allow,
		},
		{
			name: "One deny",
			policies: []security.Policy{
				NewMockPolicy("test", "policy1", security.Deny),
			},
			expected: security.Deny,
		},
		{
			name: "One allow, one undefined",
			policies: []security.Policy{
				NewMockPolicy("test", "policy1", security.Allow),
				NewMockPolicy("test", "policy2", security.Undefined),
			},
			expected: security.Allow,
		},
		{
			name: "One deny overrides allow",
			policies: []security.Policy{
				NewMockPolicy("test", "policy1", security.Allow),
				NewMockPolicy("test", "policy2", security.Deny),
			},
			expected: security.Deny,
		},
		{
			name: "Deny takes precedence regardless of order",
			policies: []security.Policy{
				NewMockPolicy("test", "policy1", security.Deny),
				NewMockPolicy("test", "policy2", security.Allow),
			},
			expected: security.Deny,
		},
		{
			name: "Last meaningful result is returned when no deny",
			policies: []security.Policy{
				NewMockPolicy("test", "policy1", security.Undefined),
				NewMockPolicy("test", "policy2", security.Allow),
				NewMockPolicy("test", "policy3", security.Undefined),
			},
			expected: security.Allow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope := NewScope(tt.policies)
			result := scope.Evaluate(actor, "action", "resource", meta)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestScopeContains(t *testing.T) {
	policy1 := NewMockPolicy("test", "policy1", security.Allow)
	policy2 := NewMockPolicy("test", "policy2", security.Deny)
	policies := []security.Policy{policy1, policy2}
	scope := NewScope(policies)

	assert.True(t, scope.Contains(policy1.ID()))
	assert.True(t, scope.Contains(policy2.ID()))

	nonExistentID := registry.ID{NS: "test", Name: "nonexistent"}
	assert.False(t, scope.Contains(nonExistentID))
}

func TestScopePolicies(t *testing.T) {
	emptyScope := NewScope(nil)
	assert.Empty(t, emptyScope.Policies())

	policy1 := NewMockPolicy("test", "policy1", security.Allow)
	policy2 := NewMockPolicy("test", "policy2", security.Deny)
	policies := []security.Policy{policy1, policy2}
	scope := NewScope(policies)

	resultPolicies := scope.Policies()
	assert.Len(t, resultPolicies, 2)

	foundPolicy1 := false
	foundPolicy2 := false
	for _, p := range resultPolicies {
		if p.ID() == policy1.ID() {
			foundPolicy1 = true
		}
		if p.ID() == policy2.ID() {
			foundPolicy2 = true
		}
	}
	assert.True(t, foundPolicy1)
	assert.True(t, foundPolicy2)
}

func TestScopeImmutability(t *testing.T) {
	policy1 := NewMockPolicy("test", "policy1", security.Allow)
	scope := NewScope([]security.Policy{policy1})

	policy2 := NewMockPolicy("test", "policy2", security.Deny)
	scopeWithAdded := scope.With(policy2)
	scopeWithRemoved := scopeWithAdded.Without(policy1.ID())

	assert.Len(t, scope.Policies(), 1)
	assert.True(t, scope.Contains(policy1.ID()))
	assert.False(t, scope.Contains(policy2.ID()))

	assert.Len(t, scopeWithAdded.Policies(), 2)
	assert.True(t, scopeWithAdded.Contains(policy1.ID()))
	assert.True(t, scopeWithAdded.Contains(policy2.ID()))

	assert.Len(t, scopeWithRemoved.Policies(), 1)
	assert.False(t, scopeWithRemoved.Contains(policy1.ID()))
	assert.True(t, scopeWithRemoved.Contains(policy2.ID()))
}
