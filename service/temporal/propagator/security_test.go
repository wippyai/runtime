package propagator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
	secapi "github.com/wippyai/runtime/api/security"
	secsystem "github.com/wippyai/runtime/system/security"
	commonpb "go.temporal.io/api/common/v1"
)

func TestSecurityPayloadSerialization(t *testing.T) {
	dc := newTestDataConverter()
	t.Run("serialize actor only", func(t *testing.T) {
		payload := &SecurityPayload{
			Actor: &ActorPayload{
				ID:   "user-123",
				Meta: map[string]any{"role": "admin"},
			},
		}

		header, err := AddSecurityToHeader(dc, nil, payload)
		require.NoError(t, err)
		require.NotNil(t, header)
		require.NotNil(t, header.Fields[SecurityHeaderKey])

		extracted, err := ExtractSecurityFromHeader(dc, header)
		require.NoError(t, err)
		require.NotNil(t, extracted)
		assert.Equal(t, "user-123", extracted.Actor.ID)
		assert.Equal(t, "admin", extracted.Actor.Meta["role"])
	})

	t.Run("serialize policies", func(t *testing.T) {
		payload := &SecurityPayload{
			Policies: []string{"policies:admin", "policies:readonly"},
		}

		header, err := AddSecurityToHeader(dc, nil, payload)
		require.NoError(t, err)

		extracted, err := ExtractSecurityFromHeader(dc, header)
		require.NoError(t, err)
		assert.Len(t, extracted.Policies, 2)
		assert.Contains(t, extracted.Policies, "policies:admin")
	})

	t.Run("nil payload returns nil header", func(t *testing.T) {
		header, err := AddSecurityToHeader(dc, nil, nil)
		require.NoError(t, err)
		assert.Nil(t, header)
	})

	t.Run("nil header returns nil payload", func(t *testing.T) {
		payload, err := ExtractSecurityFromHeader(dc, nil)
		require.NoError(t, err)
		assert.Nil(t, payload)
	})

	t.Run("empty header returns nil payload", func(t *testing.T) {
		header := &commonpb.Header{}
		payload, err := ExtractSecurityFromHeader(dc, header)
		require.NoError(t, err)
		assert.Nil(t, payload)
	})
}

func TestExtractSecurityPayload(t *testing.T) {
	t.Run("no security context returns nil", func(t *testing.T) {
		ctx := context.Background()
		payload := ExtractSecurityPayload(ctx)
		assert.Nil(t, payload)
	})

	t.Run("extract actor from context", func(t *testing.T) {
		ctx := context.Background()
		appCtx := ctxapi.NewAppContext()
		ctx = ctxapi.WithAppContext(ctx, appCtx)
		ctx, _ = ctxapi.OpenFrameContext(ctx)

		actor := secapi.Actor{ID: "user-456", Meta: map[string]any{"org": "acme"}}
		err := secapi.SetActor(ctx, actor)
		require.NoError(t, err)

		payload := ExtractSecurityPayload(ctx)
		require.NotNil(t, payload)
		require.NotNil(t, payload.Actor)
		assert.Equal(t, "user-456", payload.Actor.ID)
		assert.Equal(t, "acme", payload.Actor.Meta["org"])
	})
}

func TestApplySecurityPayload(t *testing.T) {
	t.Run("nil payload is no-op", func(t *testing.T) {
		ctx := context.Background()
		err := ApplySecurityPayload(ctx, nil)
		require.NoError(t, err)
	})

	t.Run("apply actor to context", func(t *testing.T) {
		ctx := context.Background()
		appCtx := ctxapi.NewAppContext()
		ctx = ctxapi.WithAppContext(ctx, appCtx)
		ctx, _ = ctxapi.OpenFrameContext(ctx)

		payload := &SecurityPayload{
			Actor: &ActorPayload{
				ID:   "user-789",
				Meta: map[string]any{"level": 5},
			},
		}

		err := ApplySecurityPayload(ctx, payload)
		require.NoError(t, err)

		actor, ok := secapi.GetActor(ctx)
		require.True(t, ok)
		assert.Equal(t, "user-789", actor.ID)
	})
}

func TestSecurityCtxHelpers(t *testing.T) {
	t.Run("store and retrieve payload", func(t *testing.T) {
		ctx := context.Background()
		payload := &SecurityPayload{
			Actor: &ActorPayload{ID: "test"},
		}

		ctx = WithSecurityCtx(ctx, payload)
		retrieved := GetSecurityFromCtx(ctx)

		require.NotNil(t, retrieved)
		assert.Equal(t, "test", retrieved.Actor.ID)
	})

	t.Run("nil payload returns same context", func(t *testing.T) {
		ctx := context.Background()
		newCtx := WithSecurityCtx(ctx, nil)
		assert.Equal(t, ctx, newCtx)
	})

	t.Run("no payload returns nil", func(t *testing.T) {
		ctx := context.Background()
		payload := GetSecurityFromCtx(ctx)
		assert.Nil(t, payload)
	})
}

// mockPolicy implements secapi.Policy for testing
type mockPolicy struct {
	id registry.ID
}

func (p *mockPolicy) ID() registry.ID {
	return p.id
}

func (p *mockPolicy) Evaluate(_ secapi.Actor, _, _ string, _ attrs.Bag) secapi.Result {
	return secapi.Allow
}

func TestExtractSecurityPayloadWithScope(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	// Create scope with policies
	policies := []secapi.Policy{
		&mockPolicy{id: registry.NewID("policies", "admin")},
		&mockPolicy{id: registry.NewID("policies", "read")},
	}
	scope := secsystem.NewScope(policies)
	err := secapi.SetScope(ctx, scope)
	require.NoError(t, err)

	// Extract
	payload := ExtractSecurityPayload(ctx)
	require.NotNil(t, payload)
	assert.Len(t, payload.Policies, 2)
	assert.Contains(t, payload.Policies, "policies:admin")
	assert.Contains(t, payload.Policies, "policies:read")
}
