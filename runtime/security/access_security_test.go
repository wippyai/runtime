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

// The runtime-level IsAllowed returns true when security context is incomplete
// and strict mode is disabled. The API-level secapi.IsAllowed always returns
// false for incomplete context. This discrepancy means the same action can be
// allowed in runtime code but denied at the API level.

func TestIsAllowed_RuntimeVsApiDiscrepancy(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = logs.WithLogger(ctx, zap.NewNop())
	ctx = secapi.SetStrictMode(ctx, false)

	// No actor or scope set - security context is incomplete.

	runtimeResult := IsAllowed(ctx, "admin.delete", "users.database", nil)
	assert.True(t, runtimeResult, "runtime-level allows access with no security context in non-strict mode")

	apiResult := secapi.IsAllowed(ctx, "admin.delete", "users.database", nil)
	assert.False(t, apiResult, "api-level denies access with no security context regardless of strict mode")
}

// An actor with empty ID is treated as having a valid actor present,
// bypassing the "missing actor" check path.

func TestIsAllowed_EmptyActorIDTreatedAsPresent(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = logs.WithLogger(ctx, zap.NewNop())
	ctx, _ = ctxapi.OpenFrameContext(ctx)

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

	scope := newMockScope()
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
