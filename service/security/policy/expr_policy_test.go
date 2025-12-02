package policy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
	policyapi "github.com/wippyai/runtime/api/service/security/policy"
)

func TestExprPolicy_SimpleExpression(t *testing.T) {
	config := &policyapi.ExprConfig{
		Policy: policyapi.ExprDefinition{
			Actions:    "*",
			Resources:  "*",
			Expression: `actor.meta.role == "admin"`,
			Effect:     policyapi.Allow,
		},
	}

	policy, err := NewExprPolicy(registry.ID{NS: "test", Name: "admin"}, config)
	require.NoError(t, err)

	// Admin user should be allowed
	adminActor := security.Actor{
		ID:   "admin1",
		Meta: registry.Metadata{"role": "admin"},
	}
	result := policy.Evaluate(adminActor, "any.action", "any.resource", registry.Metadata{})
	assert.Equal(t, security.Allow, result)

	// Non-admin user should not match
	userActor := security.Actor{
		ID:   "user1",
		Meta: registry.Metadata{"role": "user"},
	}
	result = policy.Evaluate(userActor, "any.action", "any.resource", registry.Metadata{})
	assert.Equal(t, security.Undefined, result)
}

func TestExprPolicy_ComplexBooleanLogic(t *testing.T) {
	config := &policyapi.ExprConfig{
		Policy: policyapi.ExprDefinition{
			Actions:    "*",
			Resources:  "*",
			Expression: `actor.meta.role == "admin" || (action == "document.read" && meta.public == true)`,
			Effect:     policyapi.Allow,
		},
	}

	policy, err := NewExprPolicy(registry.ID{NS: "test", Name: "complex"}, config)
	require.NoError(t, err)

	actor := security.Actor{
		ID:   "user1",
		Meta: registry.Metadata{"role": "user"},
	}

	// Public document read - should match
	result := policy.Evaluate(actor, "document.read", "doc:1", registry.Metadata{"public": true})
	assert.Equal(t, security.Allow, result)

	// Private document read - should not match
	result = policy.Evaluate(actor, "document.read", "doc:1", registry.Metadata{"public": false})
	assert.Equal(t, security.Undefined, result)

	// Public document write - should not match (action doesn't match)
	result = policy.Evaluate(actor, "document.write", "doc:1", registry.Metadata{"public": true})
	assert.Equal(t, security.Undefined, result)
}

func TestExprPolicy_ResourceAccess(t *testing.T) {
	config := &policyapi.ExprConfig{
		Policy: policyapi.ExprDefinition{
			Actions:    "*",
			Resources:  "*",
			Expression: `resource matches "^document:.*" && action == "document.read"`,
			Effect:     policyapi.Allow,
		},
	}

	policy, err := NewExprPolicy(registry.ID{NS: "test", Name: "doc-read"}, config)
	require.NoError(t, err)

	actor := security.Actor{ID: "user1", Meta: registry.Metadata{}}

	// Document resource with read action - should match
	result := policy.Evaluate(actor, "document.read", "document:123", registry.Metadata{})
	assert.Equal(t, security.Allow, result)

	// Non-document resource - should not match
	result = policy.Evaluate(actor, "document.read", "file:123", registry.Metadata{})
	assert.Equal(t, security.Undefined, result)

	// Document resource with write action - should not match
	result = policy.Evaluate(actor, "document.write", "document:123", registry.Metadata{})
	assert.Equal(t, security.Undefined, result)
}

func TestExprPolicy_ActionList(t *testing.T) {
	config := &policyapi.ExprConfig{
		Policy: policyapi.ExprDefinition{
			Actions:    []any{"document.read", "document.update"},
			Resources:  "*",
			Expression: `meta.owner == actor.id`,
			Effect:     policyapi.Allow,
		},
	}

	policy, err := NewExprPolicy(registry.ID{NS: "test", Name: "owner"}, config)
	require.NoError(t, err)

	actor := security.Actor{ID: "user123", Meta: registry.Metadata{}}

	// Owner with allowed action - should match
	result := policy.Evaluate(actor, "document.read", "doc:1", registry.Metadata{"owner": "user123"})
	assert.Equal(t, security.Allow, result)

	// Owner with disallowed action - policy doesn't apply (actions filter)
	result = policy.Evaluate(actor, "document.delete", "doc:1", registry.Metadata{"owner": "user123"})
	assert.Equal(t, security.Undefined, result)

	// Non-owner with allowed action - expression doesn't match
	result = policy.Evaluate(actor, "document.read", "doc:1", registry.Metadata{"owner": "other"})
	assert.Equal(t, security.Undefined, result)
}

func TestExprPolicy_DenyEffect(t *testing.T) {
	config := &policyapi.ExprConfig{
		Policy: policyapi.ExprDefinition{
			Actions:    "*",
			Resources:  "*",
			Expression: `meta.blocked == true`,
			Effect:     policyapi.Deny,
		},
	}

	policy, err := NewExprPolicy(registry.ID{NS: "test", Name: "deny"}, config)
	require.NoError(t, err)

	actor := security.Actor{ID: "user1", Meta: registry.Metadata{}}

	// Blocked resource - should deny
	result := policy.Evaluate(actor, "any.action", "any.resource", registry.Metadata{"blocked": true})
	assert.Equal(t, security.Deny, result)

	// Not blocked - should not apply
	result = policy.Evaluate(actor, "any.action", "any.resource", registry.Metadata{"blocked": false})
	assert.Equal(t, security.Undefined, result)
}

func TestExprPolicy_InvalidExpression(t *testing.T) {
	config := &policyapi.ExprConfig{
		Policy: policyapi.ExprDefinition{
			Actions:    "*",
			Resources:  "*",
			Expression: `invalid syntax here`,
			Effect:     policyapi.Allow,
		},
	}

	_, err := NewExprPolicy(registry.ID{NS: "test", Name: "invalid"}, config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expression compilation failed")
}

func TestExprPolicy_EmptyExpression(t *testing.T) {
	config := &policyapi.ExprConfig{
		Policy: policyapi.ExprDefinition{
			Actions:    "*",
			Resources:  "*",
			Expression: "",
			Effect:     policyapi.Allow,
		},
	}

	_, err := NewExprPolicy(registry.ID{NS: "test", Name: "empty"}, config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expression cannot be empty")
}

func TestExprPolicy_ArrayOperations(t *testing.T) {
	config := &policyapi.ExprConfig{
		Policy: policyapi.ExprDefinition{
			Actions:    "*",
			Resources:  "*",
			Expression: `action in ["document.read", "document.list"] && all(meta.tags, {# != "private"})`,
			Effect:     policyapi.Allow,
		},
	}

	policy, err := NewExprPolicy(registry.ID{NS: "test", Name: "arrays"}, config)
	require.NoError(t, err)

	actor := security.Actor{ID: "user1", Meta: registry.Metadata{}}

	// Allowed action with public tags - should match
	result := policy.Evaluate(actor, "document.read", "doc:1", registry.Metadata{
		"tags": []any{"public", "shared"},
	})
	assert.Equal(t, security.Allow, result)

	// Allowed action with private tag - should not match
	result = policy.Evaluate(actor, "document.read", "doc:1", registry.Metadata{
		"tags": []any{"public", "private"},
	})
	assert.Equal(t, security.Undefined, result)

	// Disallowed action - should not match
	result = policy.Evaluate(actor, "document.write", "doc:1", registry.Metadata{
		"tags": []any{"public"},
	})
	assert.Equal(t, security.Undefined, result)
}

func TestExprPolicy_NilConfig(t *testing.T) {
	_, err := NewExprPolicy(registry.ID{NS: "test", Name: "nil"}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "config cannot be nil")
}

func TestExprPolicy_ID(t *testing.T) {
	id := registry.ID{NS: "test", Name: "policy1"}
	config := &policyapi.ExprConfig{
		Policy: policyapi.ExprDefinition{
			Actions:    "*",
			Resources:  "*",
			Expression: "true",
			Effect:     policyapi.Allow,
		},
	}

	policy, err := NewExprPolicy(id, config)
	require.NoError(t, err)
	assert.Equal(t, id, policy.ID())
}
