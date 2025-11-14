package security

import (
	ctxapi "github.com/wippyai/runtime/api/context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/registry"
	secapi "github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/system/security"
	"go.uber.org/zap"
)

// mockPolicy implements secapi.Policy for testing
type mockPolicy struct {
	id       registry.ID
	allowed  map[string]bool
	metadata map[string]interface{}
}

func newMockPolicy(id registry.ID) *mockPolicy {
	return &mockPolicy{
		id:       id,
		allowed:  make(map[string]bool),
		metadata: make(map[string]interface{}),
	}
}

func (m *mockPolicy) ID() registry.ID {
	return m.id
}

func (m *mockPolicy) Evaluate(_ secapi.Actor, action, resource string, _ registry.Metadata) secapi.Result {
	key := action + ":" + resource
	if m.allowed[key] {
		return secapi.Allow
	}
	return secapi.Deny
}

func (m *mockPolicy) Allow(action, resource string) {
	m.allowed[action+":"+resource] = true
}

func (m *mockPolicy) Deny(action, resource string) {
	m.allowed[action+":"+resource] = false
}

func (m *mockPolicy) Metadata() map[string]interface{} {
	return m.metadata
}

// mockScope implements secapi.Scope for testing
type mockScope struct {
	policies []secapi.Policy
}

func newMockScope() *mockScope {
	return &mockScope{
		policies: make([]secapi.Policy, 0),
	}
}

func (m *mockScope) Evaluate(actor secapi.Actor, action, resource string, metadata registry.Metadata) secapi.Result {
	for _, policy := range m.policies {
		result := policy.Evaluate(actor, action, resource, metadata)
		if result == secapi.Allow {
			return secapi.Allow
		}
	}
	return secapi.Deny
}

func (m *mockScope) With(policy secapi.Policy) secapi.Scope {
	newScope := newMockScope()
	newScope.policies = append(newScope.policies, m.policies...)
	newScope.policies = append(newScope.policies, policy)
	return newScope
}

func (m *mockScope) Without(policyID registry.ID) secapi.Scope {
	newScope := newMockScope()
	for _, policy := range m.policies {
		if policy.ID() != policyID {
			newScope.policies = append(newScope.policies, policy)
		}
	}
	return newScope
}

func (m *mockScope) Contains(policyID registry.ID) bool {
	for _, policy := range m.policies {
		if policy.ID() == policyID {
			return true
		}
	}
	return false
}

func (m *mockScope) Policies() []secapi.Policy {
	return m.policies
}

func (m *mockScope) AddPolicy(policy secapi.Policy) {
	m.policies = append(m.policies, policy)
}

func TestIsAllowed_WithCompleteContext_Allow(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	ctx := ctxapi.NewRootContext()
	ctx = logs.WithLogger(ctx, logger)

	actor := secapi.Actor{ID: "test-actor"}
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_ = secapi.SetActor(ctx, actor)

	policy := newMockPolicy(registry.ID{NS: "test", Name: "policy"})
	policy.Allow("read", "test-resource")

	scope := newMockScope()
	scope.AddPolicy(policy)
	_ = secapi.SetScope(ctx, scope)

	// Execute
	result := IsAllowed(ctx, "read", "test-resource", nil)

	// Assert
	assert.True(t, result, "Should allow access when policy permits")
}

func TestIsAllowed_WithCompleteContext_Deny(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	ctx := ctxapi.NewRootContext()
	ctx = logs.WithLogger(ctx, logger)

	actor := secapi.Actor{ID: "test-actor"}
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_ = secapi.SetActor(ctx, actor)

	policy := newMockPolicy(registry.ID{NS: "test", Name: "policy"})
	policy.Deny("read", "test-resource")

	scope := newMockScope()
	scope.AddPolicy(policy)
	_ = secapi.SetScope(ctx, scope)

	// Execute
	result := IsAllowed(ctx, "read", "test-resource", nil)

	// Assert
	assert.False(t, result, "Should deny access when policy denies")
}

func TestIsAllowed_WithCompleteContext_MultiplePolicies(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	ctx := ctxapi.NewRootContext()
	ctx = logs.WithLogger(ctx, logger)

	actor := secapi.Actor{ID: "test-actor"}
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_ = secapi.SetActor(ctx, actor)

	// First policy denies
	policy1 := newMockPolicy(registry.ID{NS: "test", Name: "policy-1"})
	policy1.Deny("read", "test-resource")

	// Second policy allows
	policy2 := newMockPolicy(registry.ID{NS: "test", Name: "policy-2"})
	policy2.Allow("read", "test-resource")

	scope := newMockScope()
	scope.AddPolicy(policy1)
	scope.AddPolicy(policy2)
	_ = secapi.SetScope(ctx, scope)

	// Execute
	result := IsAllowed(ctx, "read", "test-resource", nil)

	// Assert
	assert.True(t, result, "Should allow access when any policy permits")
}

func TestIsAllowed_WithCompleteContext_WithMetadata(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	ctx := ctxapi.NewRootContext()
	ctx = logs.WithLogger(ctx, logger)

	actor := secapi.Actor{ID: "test-actor"}
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_ = secapi.SetActor(ctx, actor)

	policy := newMockPolicy(registry.ID{NS: "test", Name: "policy"})
	policy.Allow("read", "test-resource")

	scope := newMockScope()
	scope.AddPolicy(policy)
	_ = secapi.SetScope(ctx, scope)

	metadata := registry.Metadata{"key": "value"}

	// Execute
	result := IsAllowed(ctx, "read", "test-resource", metadata)

	// Assert
	assert.True(t, result, "Should allow access with metadata")
}

func TestIsAllowed_WithoutActor_NonStrictMode(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	ctx := ctxapi.NewRootContext()
	ctx = logs.WithLogger(ctx, logger)

	policy := newMockPolicy(registry.ID{NS: "test", Name: "policy"})
	scope := newMockScope()
	scope.AddPolicy(policy)
	_ = secapi.SetScope(ctx, scope)

	// Execute
	result := IsAllowed(ctx, "read", "test-resource", nil)

	// Assert - since STRICT is false, should allow access
	assert.True(t, result, "Should allow access in non-strict mode when actor is missing")
}

func TestIsAllowed_WithoutScope_NonStrictMode(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	ctx := ctxapi.NewRootContext()
	ctx = logs.WithLogger(ctx, logger)

	actor := secapi.Actor{ID: "test-actor"}
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_ = secapi.SetActor(ctx, actor)

	// Execute
	result := IsAllowed(ctx, "read", "test-resource", nil)

	// Assert - since STRICT is false, should allow access
	assert.True(t, result, "Should allow access in non-strict mode when scope is missing")
}

func TestIsAllowed_WithoutActorAndScope_NonStrictMode(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	ctx := ctxapi.NewRootContext()
	ctx = logs.WithLogger(ctx, logger)

	// Execute
	result := IsAllowed(ctx, "read", "test-resource", nil)

	// Assert - since STRICT is false, should allow access
	assert.True(t, result, "Should allow access in non-strict mode when both actor and scope are missing")
}

func TestIsAllowed_WithEmptyActionAndResource(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	ctx := ctxapi.NewRootContext()
	ctx = logs.WithLogger(ctx, logger)

	actor := secapi.Actor{ID: "test-actor"}
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_ = secapi.SetActor(ctx, actor)

	policy := newMockPolicy(registry.ID{NS: "test", Name: "policy"})
	policy.Allow("", "")

	scope := newMockScope()
	scope.AddPolicy(policy)
	_ = secapi.SetScope(ctx, scope)

	// Execute
	result := IsAllowed(ctx, "", "", nil)

	// Assert
	assert.True(t, result, "Should handle empty action and resource")
}

func TestIsAllowed_WithComplexMetadata(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	ctx := ctxapi.NewRootContext()
	ctx = logs.WithLogger(ctx, logger)

	actor := secapi.Actor{ID: "test-actor"}
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_ = secapi.SetActor(ctx, actor)

	policy := newMockPolicy(registry.ID{NS: "test", Name: "policy"})
	policy.Allow("read", "test-resource")

	scope := newMockScope()
	scope.AddPolicy(policy)
	_ = secapi.SetScope(ctx, scope)

	metadata := registry.Metadata{
		"user_id":     123,
		"role":        "admin",
		"permissions": []string{"read", "write"},
		"nested": map[string]interface{}{
			"key": "value",
		},
	}

	// Execute
	result := IsAllowed(ctx, "read", "test-resource", metadata)

	// Assert
	assert.True(t, result, "Should handle complex metadata")
}

func TestIsAllowed_ConcurrentAccess(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	ctx := ctxapi.NewRootContext()
	ctx = logs.WithLogger(ctx, logger)

	actor := secapi.Actor{ID: "test-actor"}
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_ = secapi.SetActor(ctx, actor)

	policy := newMockPolicy(registry.ID{NS: "test", Name: "policy"})
	policy.Allow("read", "test-resource")

	scope := newMockScope()
	scope.AddPolicy(policy)
	_ = secapi.SetScope(ctx, scope)

	// Execute concurrent access
	const numGoroutines = 100
	results := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			results <- IsAllowed(ctx, "read", "test-resource", nil)
		}()
	}

	// Collect results
	allAllowed := true
	for i := 0; i < numGoroutines; i++ {
		if !<-results {
			allAllowed = false
		}
	}

	// Assert
	assert.True(t, allAllowed, "All concurrent calls should return the same result")
}

func TestIsAllowed_WithRealSecurityScope(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	ctx := ctxapi.NewRootContext()
	ctx = logs.WithLogger(ctx, logger)

	actor := secapi.Actor{ID: "test-actor"}
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_ = secapi.SetActor(ctx, actor)

	// Use the real security scope implementation
	scope := security.NewScope(nil)
	_ = secapi.SetScope(ctx, scope)

	// Execute
	result := IsAllowed(ctx, "read", "test-resource", nil)

	// Assert - real scope with no policies should deny by default
	assert.False(t, result, "Real scope with no policies should deny access")
}

func TestIsAllowed_STRICTConstant(t *testing.T) {
	// Test that STRICT constant is properly defined
	assert.False(t, STRICT, "STRICT should be false by default")
}
