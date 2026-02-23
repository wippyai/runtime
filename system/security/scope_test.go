// SPDX-License-Identifier: MPL-2.0

package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
)

type mockPolicy struct {
	id       registry.ID
	decision security.Result
}

func (m *mockPolicy) ID() registry.ID {
	return m.id
}

func (m *mockPolicy) Evaluate(_ security.Actor, _, _ string, _ attrs.Bag) security.Result {
	return m.decision
}

func newMockPolicy(name string, decision security.Result) *mockPolicy {
	return &mockPolicy{
		id:       registry.NewID("test", name),
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
		newMockPolicy("policy1", security.Allow),
		newMockPolicy("policy2", security.Deny),
	}
	scope = NewScope(policies)
	assert.NotNil(t, scope)
	assert.Len(t, scope.Policies(), 2)
}

func TestScopeWith(t *testing.T) {
	policy1 := newMockPolicy("policy1", security.Allow)
	scope := NewScope([]security.Policy{policy1})

	policy2 := newMockPolicy("policy2", security.Deny)
	newScope := scope.With(policy2)

	assert.Len(t, scope.Policies(), 1)
	assert.True(t, scope.Contains(policy1.ID()))
	assert.False(t, scope.Contains(policy2.ID()))

	assert.Len(t, newScope.Policies(), 2)
	assert.True(t, newScope.Contains(policy1.ID()))
	assert.True(t, newScope.Contains(policy2.ID()))
}

func TestScopeWithout(t *testing.T) {
	policy1 := newMockPolicy("policy1", security.Allow)
	policy2 := newMockPolicy("policy2", security.Deny)
	scope := NewScope([]security.Policy{policy1, policy2})

	newScope := scope.Without(policy1.ID())

	assert.Len(t, scope.Policies(), 2)
	assert.Len(t, newScope.Policies(), 1)
	assert.False(t, newScope.Contains(policy1.ID()))
	assert.True(t, newScope.Contains(policy2.ID()))

	nonExistentID := registry.NewID("test", "nonexistent")
	sameScope := newScope.Without(nonExistentID)
	assert.Equal(t, newScope, sameScope)
}

func TestScopeEvaluate(t *testing.T) {
	actor := security.Actor{ID: "user1"}
	meta := attrs.Bag{}

	tests := []struct {
		name     string
		policies []security.Policy
		expected security.Result
	}{
		{
			name:     "Empty scope returns Undefined",
			policies: []security.Policy{},
			expected: security.Undefined,
		},
		{
			name: "All undefined returns Undefined",
			policies: []security.Policy{
				newMockPolicy("policy1", security.Undefined),
			},
			expected: security.Undefined,
		},
		{
			name: "Single Allow",
			policies: []security.Policy{
				newMockPolicy("policy1", security.Allow),
			},
			expected: security.Allow,
		},
		{
			name: "Single Deny",
			policies: []security.Policy{
				newMockPolicy("policy1", security.Deny),
			},
			expected: security.Deny,
		},
		{
			name: "Deny takes precedence",
			policies: []security.Policy{
				newMockPolicy("policy1", security.Allow),
				newMockPolicy("policy2", security.Deny),
			},
			expected: security.Deny,
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
	policy1 := newMockPolicy("policy1", security.Allow)
	scope := NewScope([]security.Policy{policy1})

	assert.True(t, scope.Contains(policy1.ID()))
	assert.False(t, scope.Contains(registry.NewID("test", "nonexistent")))
}
