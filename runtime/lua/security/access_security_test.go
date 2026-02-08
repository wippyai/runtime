package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/registry"
	secapi "github.com/wippyai/runtime/api/security"
	"go.uber.org/zap"
)

// The Lua-level IsAllowed returns true when security context is incomplete
// and strict mode is disabled. The API-level secapi.IsAllowed always returns
// false for incomplete context. This discrepancy means the same action can be
// allowed in Lua code but denied at the API level.

func TestIsAllowed_LuaVsApiDiscrepancy(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = logs.WithLogger(ctx, zap.NewNop())
	ctx = secapi.SetStrictMode(ctx, false)

	// No actor or scope set - security context is incomplete.

	// Lua-level: allows in non-strict mode
	luaResult := IsAllowed(ctx, "admin.delete", "users.database", nil)
	assert.True(t, luaResult, "lua-level allows access with no security context in non-strict mode")

	// API-level: always denies when actor or scope is missing
	apiResult := secapi.IsAllowed(ctx, "admin.delete", "users.database", nil)
	assert.False(t, apiResult, "api-level denies access with no security context regardless of strict mode")
}

// An actor with empty ID is treated as having a valid actor present,
// bypassing the "missing actor" check path.

func TestIsAllowed_EmptyActorIDTreatedAsPresent(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = logs.WithLogger(ctx, zap.NewNop())
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	// Set actor with empty ID
	_ = secapi.SetActor(ctx, secapi.Actor{ID: ""})

	policy := newMockPolicy(registry.NewID("test", "allow-all"))
	policy.Allow("read", "data")
	scope := newMockScope()
	scope.AddPolicy(policy)
	_ = secapi.SetScope(ctx, scope)

	result := IsAllowed(ctx, "read", "data", nil)
	assert.True(t, result, "empty actor ID is treated as a valid identity")
}

// When the scope has no policies, Evaluate returns Deny (not Undefined),
// so IsAllowed correctly returns false. This verifies that an empty scope
// doesn't accidentally allow access.

func TestIsAllowed_EmptyScopeDoesNotAllow(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = logs.WithLogger(ctx, zap.NewNop())
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	_ = secapi.SetActor(ctx, secapi.Actor{ID: "user-1"})

	scope := newMockScope() // no policies
	_ = secapi.SetScope(ctx, scope)

	result := IsAllowed(ctx, "read", "data", nil)
	assert.False(t, result, "empty scope denies access")
}

// Non-strict mode allows destructive operations when security context
// setup fails (e.g., misconfigured worker, propagation error).

func TestIsAllowed_NonStrictAllowsDestructiveOps(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = logs.WithLogger(ctx, zap.NewNop())
	ctx = secapi.SetStrictMode(ctx, false)

	// No actor, no scope - simulates failed security context setup
	destructiveOps := []struct{ action, resource string }{
		{"admin.delete", "users.all"},
		{"system.shutdown", "cluster"},
		{"security.token.revoke", "all"},
		{"data.export", "customer.pii"},
	}

	for _, op := range destructiveOps {
		result := IsAllowed(ctx, op.action, op.resource, nil)
		assert.True(t, result,
			"non-strict mode allows %s on %s with no security context", op.action, op.resource)
	}
}
