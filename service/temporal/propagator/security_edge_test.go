// SPDX-License-Identifier: MPL-2.0

package propagator

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
	secapi "github.com/wippyai/runtime/api/security"
	secsystem "github.com/wippyai/runtime/system/security"
	commonpb "go.temporal.io/api/common/v1"
)

// mockSecurityRegistry implements secapi.Registry for testing
type mockSecurityRegistry struct {
	policies map[string]secapi.Policy
}

func (r *mockSecurityRegistry) GetPolicy(id registry.ID) (secapi.Policy, error) {
	p, ok := r.policies[id.String()]
	if !ok {
		return nil, fmt.Errorf("policy not found: %s", id.String())
	}
	return p, nil
}

func (r *mockSecurityRegistry) GetPolicyGroup(_ registry.ID) (secapi.Scope, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *mockSecurityRegistry) ListGroups() []registry.ID   { return nil }
func (r *mockSecurityRegistry) ListPolicies() []registry.ID { return nil }

// --- ExtractSecurityFromHeader edge cases ---

func TestExtractSecurityFromHeader_NilDataConverter(t *testing.T) {
	_, err := ExtractSecurityFromHeader(nil, &commonpb.Header{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "data converter not available")
}

func TestExtractSecurityFromHeader_MissingSecurityKey(t *testing.T) {
	dc := newTestDataConverter()
	header := &commonpb.Header{
		Fields: map[string]*commonpb.Payload{
			"other-key": {},
		},
	}
	payload, err := ExtractSecurityFromHeader(dc, header)
	require.NoError(t, err)
	assert.Nil(t, payload)
}

// --- AddSecurityToHeader edge cases ---

func TestAddSecurityToHeader_NilDataConverter(t *testing.T) {
	_, err := AddSecurityToHeader(nil, nil, &SecurityPayload{Actor: &ActorPayload{ID: "x"}})
	require.Error(t, err)
}

func TestAddSecurityToHeader_AddsToExistingHeader(t *testing.T) {
	dc := newTestDataConverter()
	existing := &commonpb.Header{
		Fields: map[string]*commonpb.Payload{
			"existing-key": {},
		},
	}
	payload := &SecurityPayload{Actor: &ActorPayload{ID: "user-1"}}

	header, err := AddSecurityToHeader(dc, existing, payload)
	require.NoError(t, err)
	assert.Contains(t, header.Fields, "existing-key")
	assert.Contains(t, header.Fields, SecurityHeaderKey)
}

func TestAddSecurityToHeader_HeaderWithNilFields(t *testing.T) {
	dc := newTestDataConverter()
	existing := &commonpb.Header{}
	payload := &SecurityPayload{Actor: &ActorPayload{ID: "user-1"}}

	header, err := AddSecurityToHeader(dc, existing, payload)
	require.NoError(t, err)
	assert.Contains(t, header.Fields, SecurityHeaderKey)
}

// --- ApplySecurityPayload edge cases ---

func TestApplySecurityPayload_NoRegistry(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	payload := &SecurityPayload{
		Policies: []string{"policies:admin"},
	}

	// No registry in context - should succeed silently
	err := ApplySecurityPayload(ctx, payload)
	require.NoError(t, err)
}

func TestApplySecurityPayload_WithRegistryAndPolicies(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)

	reg := &mockSecurityRegistry{
		policies: map[string]secapi.Policy{
			"policies:admin": &mockPolicy{id: registry.NewID("policies", "admin")},
			"policies:read":  &mockPolicy{id: registry.NewID("policies", "read")},
		},
	}
	ctx = secapi.WithRegistry(ctx, reg)
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	payload := &SecurityPayload{
		Policies: []string{"policies:admin", "policies:read"},
	}

	err := ApplySecurityPayload(ctx, payload)
	require.NoError(t, err)

	scope, hasScope := secapi.GetScope(ctx)
	require.True(t, hasScope)
	assert.Len(t, scope.Policies(), 2)
}

func TestApplySecurityPayload_SkipsMissingPolicies(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)

	reg := &mockSecurityRegistry{
		policies: map[string]secapi.Policy{
			"policies:admin": &mockPolicy{id: registry.NewID("policies", "admin")},
		},
	}
	ctx = secapi.WithRegistry(ctx, reg)
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	payload := &SecurityPayload{
		Policies: []string{"policies:admin", "policies:nonexistent"},
	}

	err := ApplySecurityPayload(ctx, payload)
	require.NoError(t, err)

	scope, hasScope := secapi.GetScope(ctx)
	require.True(t, hasScope)
	assert.Len(t, scope.Policies(), 1)
}

func TestApplySecurityPayload_AllPoliciesMissing(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)

	reg := &mockSecurityRegistry{
		policies: map[string]secapi.Policy{},
	}
	ctx = secapi.WithRegistry(ctx, reg)
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	payload := &SecurityPayload{
		Policies: []string{"policies:missing1", "policies:missing2"},
	}

	// All policies missing - no scope set, no error
	err := ApplySecurityPayload(ctx, payload)
	require.NoError(t, err)
}

func TestApplySecurityPayload_ActorAndPolicies(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)

	reg := &mockSecurityRegistry{
		policies: map[string]secapi.Policy{
			"policies:admin": &mockPolicy{id: registry.NewID("policies", "admin")},
		},
	}
	ctx = secapi.WithRegistry(ctx, reg)
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	payload := &SecurityPayload{
		Actor: &ActorPayload{
			ID:   "user-full",
			Meta: map[string]any{"level": 10},
		},
		Policies: []string{"policies:admin"},
	}

	err := ApplySecurityPayload(ctx, payload)
	require.NoError(t, err)

	actor, hasActor := secapi.GetActor(ctx)
	require.True(t, hasActor)
	assert.Equal(t, "user-full", actor.ID)

	scope, hasScope := secapi.GetScope(ctx)
	require.True(t, hasScope)
	assert.Len(t, scope.Policies(), 1)
}

// --- ExtractSecurityPayload edge cases ---

func TestExtractSecurityPayload_ActorOnly(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	actor := secapi.Actor{ID: "actor-only"}
	err := secapi.SetActor(ctx, actor)
	require.NoError(t, err)

	payload := ExtractSecurityPayload(ctx)
	require.NotNil(t, payload)
	assert.NotNil(t, payload.Actor)
	assert.Equal(t, "actor-only", payload.Actor.ID)
	assert.Empty(t, payload.Policies)
}

func TestExtractSecurityPayload_ScopeWithEmptyPolicies(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	scope := secsystem.NewScope(nil)
	err := secapi.SetScope(ctx, scope)
	require.NoError(t, err)

	payload := ExtractSecurityPayload(ctx)
	// Has scope but no policies - payload may be non-nil but policies empty
	if payload != nil {
		assert.Empty(t, payload.Policies)
	}
}

// --- RoundTrip: Extract -> Header -> Apply ---

func TestSecurityPayload_RoundTrip(t *testing.T) {
	dc := newTestDataConverter()

	original := &SecurityPayload{
		Actor: &ActorPayload{
			ID:   "roundtrip-user",
			Meta: map[string]any{"org": "acme", "tier": "premium"},
		},
		Policies: []string{"policies:admin", "policies:write"},
	}

	header, err := AddSecurityToHeader(dc, nil, original)
	require.NoError(t, err)

	extracted, err := ExtractSecurityFromHeader(dc, header)
	require.NoError(t, err)
	require.NotNil(t, extracted)

	assert.Equal(t, "roundtrip-user", extracted.Actor.ID)
	assert.Equal(t, "acme", extracted.Actor.Meta["org"])
	assert.Len(t, extracted.Policies, 2)
	assert.Contains(t, extracted.Policies, "policies:admin")
	assert.Contains(t, extracted.Policies, "policies:write")
}
