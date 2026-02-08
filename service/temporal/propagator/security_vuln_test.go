package propagator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	secapi "github.com/wippyai/runtime/api/security"
)

// ApplySecurityPayload accepts any actor ID from the payload without
// cryptographic verification. An attacker controlling Temporal headers
// can impersonate any user.

func TestApplySecurityPayload_ArbitraryActorIDAccepted(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	payload := &SecurityPayload{
		Actor: &ActorPayload{
			ID: "system-admin",
			Meta: map[string]any{
				"role":   "superuser",
				"bypass": true,
			},
		},
	}

	err := ApplySecurityPayload(ctx, payload)
	require.NoError(t, err)

	actor, hasActor := secapi.GetActor(ctx)
	require.True(t, hasActor)
	assert.Equal(t, "system-admin", actor.ID, "arbitrary actor ID accepted without signature verification")
	assert.Equal(t, "superuser", actor.Meta["role"], "arbitrary meta accepted without validation")
}

func TestApplySecurityPayload_EmptyActorIDAccepted(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	payload := &SecurityPayload{
		Actor: &ActorPayload{ID: ""},
	}

	err := ApplySecurityPayload(ctx, payload)
	require.NoError(t, err)

	actor, hasActor := secapi.GetActor(ctx)
	assert.True(t, hasActor, "empty actor ID is accepted as a valid actor")
	assert.Empty(t, actor.ID)
}

// When registry is unavailable, ApplySecurityPayload sets the actor
// but silently discards all policies. The result is an authenticated
// context with no authorization (actor set, scope not set).

func TestApplySecurityPayload_AuthenticatedWithoutAuthorization(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	payload := &SecurityPayload{
		Actor:    &ActorPayload{ID: "user-1"},
		Policies: []string{"policies:admin", "policies:write"},
	}

	// No registry in context - policies cannot be resolved
	err := ApplySecurityPayload(ctx, payload)
	require.NoError(t, err, "silent success despite unresolvable policies")

	// Actor IS set - identity established
	actor, hasActor := secapi.GetActor(ctx)
	assert.True(t, hasActor)
	assert.Equal(t, "user-1", actor.ID)

	// Scope is NOT set - authorization silently lost
	_, hasScope := secapi.GetScope(ctx)
	assert.False(t, hasScope, "all authorization policies silently dropped")
}

// When all policy IDs in the payload are fabricated/missing, no scope
// is set and no error is returned. The payload's security intent
// is completely lost.

func TestApplySecurityPayload_AllPoliciesFabricatedNoError(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)

	reg := &mockSecurityRegistry{
		policies: map[string]secapi.Policy{},
	}
	ctx = secapi.WithRegistry(ctx, reg)
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	// Payload claims policies that don't exist
	payload := &SecurityPayload{
		Actor:    &ActorPayload{ID: "attacker"},
		Policies: []string{"policies:fabricated1", "policies:fabricated2"},
	}

	err := ApplySecurityPayload(ctx, payload)
	require.NoError(t, err, "fabricated policy IDs produce no error")

	// Actor is set
	actor, hasActor := secapi.GetActor(ctx)
	assert.True(t, hasActor)
	assert.Equal(t, "attacker", actor.ID)

	// No scope set because all policies were missing
	_, hasScope := secapi.GetScope(ctx)
	assert.False(t, hasScope, "no scope set - fabricated policies silently dropped")
}

// WithSecurityCtx/GetSecurityFromCtx uses plain context.WithValue
// with no integrity check. Any code with context access can inject
// a security payload.

func TestWithSecurityCtx_NoIntegrityProtection(t *testing.T) {
	ctx := context.Background()

	injected := &SecurityPayload{
		Actor: &ActorPayload{
			ID: "injected-admin",
		},
		Policies: []string{"policies:root"},
	}

	ctx = WithSecurityCtx(ctx, injected)
	recovered := GetSecurityFromCtx(ctx)

	require.NotNil(t, recovered)
	assert.Equal(t, "injected-admin", recovered.Actor.ID,
		"security payload injected into context without verification")
}
