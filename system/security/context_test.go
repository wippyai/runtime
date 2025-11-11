package security

import (
	"testing"

	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/security"
	"github.com/stretchr/testify/assert"
)

func TestWithSecurityConfig(t *testing.T) {
	// Test with nil config
	ctx := ctxapi.NewRootContext()
	result := WithSecurityConfig(ctx, nil)
	assert.Equal(t, ctx, result, "WithSecurityConfig should return original context when config is nil")

	// Test with empty config
	emptyConfig := &security.Config{}
	result = WithSecurityConfig(ctx, emptyConfig)
	// Check that actor was set (even if empty) - WithSecurityConfig always sets the actor
	actor, ok := security.GetActor(result)
	assert.True(t, ok, "Actor should be set even with empty config")
	assert.Equal(t, "", actor.ID, "Actor ID should be empty string with empty config")

	// Test with actor only
	actorConfig := &security.Config{
		Actor: security.Actor{ID: "test-user"},
	}
	result = WithSecurityConfig(ctx, actorConfig)
	actor, ok = security.GetActor(result)
	assert.True(t, ok, "Actor should be set in context")
	assert.Equal(t, "test-user", actor.ID, "Actor ID should match")

	// Test with policies but no registry
	policyConfig := &security.Config{
		Actor:        security.Actor{ID: "test-user"},
		Policies:     []registry.ID{{NS: "test", Name: "policy1"}},
		PolicyGroups: []registry.ID{{NS: "test", Name: "group1"}},
	}
	result = WithSecurityConfig(ctx, policyConfig)
	actor, ok = security.GetActor(result)
	assert.True(t, ok, "Actor should still be set in context")
	assert.Equal(t, "test-user", actor.ID, "Actor ID should match")
	// Check that no scope was set since there's no registry
	_, ok = security.GetScope(result)
	assert.False(t, ok, "No scope should be set without registry")

	// Test with registry and policies (but policies don't exist in registry)
	reg := NewPolicyRegistry(nil, nil)
	ctxWithReg := security.WithRegistry(ctx, reg)
	result = WithSecurityConfig(ctxWithReg, policyConfig)

	// Verify actor is set
	actor, ok = security.GetActor(result)
	assert.True(t, ok, "Actor should be set in context")
	assert.Equal(t, "test-user", actor.ID, "Actor ID should match")

	// Verify scope is NOT set since the policies don't exist in the registry
	_, ok = security.GetScope(result)
	assert.False(t, ok, "Scope should not be set when policies don't exist in registry")
}
