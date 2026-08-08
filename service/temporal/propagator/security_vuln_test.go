// SPDX-License-Identifier: MPL-2.0

package propagator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
	secapi "github.com/wippyai/runtime/api/security"
	commonpb "go.temporal.io/api/common/v1"
)

func TestExtractSecurityFromHeader_RejectsUnsignedClaims(t *testing.T) {
	dc := newTestDataConverter()
	claims, err := dc.ToPayload(&SecurityPayload{
		Actor:    &ActorPayload{ID: "system-admin"},
		Audience: testSecurityAudience,
		Policies: []string{"policies:admin"},
		Scope:    true,
	})
	require.NoError(t, err)
	header := &commonpb.Header{Fields: map[string]*commonpb.Payload{
		SecurityHeaderKey: claims,
	}}

	_, err = ExtractSecurityFromHeader(dc, header, testSecurityAudience, testSecurityHMACKey)
	require.Error(t, err)
}

func TestExtractSecurityFromHeader_RejectsTamperedClaims(t *testing.T) {
	dc := newTestDataConverter()
	header, err := AddSecurityToHeader(dc, nil, &SecurityPayload{
		Actor:    &ActorPayload{ID: "user"},
		Audience: testSecurityAudience,
		Scope:    true,
	}, testSecurityHMACKey)
	require.NoError(t, err)
	header.Fields[SecurityHeaderKey].Data = append(header.Fields[SecurityHeaderKey].Data, byte('x'))

	_, err = ExtractSecurityFromHeader(dc, header, testSecurityAudience, testSecurityHMACKey)
	require.Error(t, err)
}

func TestExtractSecurityFromHeader_RejectsDifferentAudience(t *testing.T) {
	dc := newTestDataConverter()
	header, err := AddSecurityToHeader(dc, nil, &SecurityPayload{
		Actor:    &ActorPayload{ID: "user"},
		Audience: "workflow-a",
		Scope:    true,
	}, testSecurityHMACKey)
	require.NoError(t, err)

	_, err = ExtractSecurityFromHeader(dc, header, "workflow-b", testSecurityHMACKey)
	require.ErrorContains(t, err, "audience")
}

func TestExtractSecurityFromHeader_AcceptsPreviousRotationKey(t *testing.T) {
	dc := newTestDataConverter()
	previousKey := []byte("previous-0123456789abcdef01234567")
	activeKey := []byte("active---0123456789abcdef01234567")
	header, err := AddSecurityToHeader(dc, nil, &SecurityPayload{
		Actor:    &ActorPayload{ID: "user"},
		Audience: testSecurityAudience,
		Scope:    true,
	}, previousKey)
	require.NoError(t, err)

	payload, err := ExtractSecurityFromHeader(dc, header, testSecurityAudience, activeKey, previousKey)
	require.NoError(t, err)
	require.Equal(t, "user", payload.Actor.ID)
}

func TestApplySecurityPayload_RejectsIncompleteContext(t *testing.T) {
	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	err := ApplySecurityPayload(ctx, &SecurityPayload{Actor: &ActorPayload{ID: "user"}})
	require.Error(t, err)
	_, hasActor := secapi.GetActor(ctx)
	assert.False(t, hasActor)
}

func TestApplySecurityPayload_RejectsEmptyActor(t *testing.T) {
	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	err := ApplySecurityPayload(ctx, &SecurityPayload{Actor: &ActorPayload{}, Scope: true})
	require.Error(t, err)
}

func TestApplySecurityPayload_RejectsUnknownPolicyWithoutPartialState(t *testing.T) {
	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	ctx = secapi.WithRegistry(ctx, &mockSecurityRegistry{policies: map[string]secapi.Policy{
		"policies:admin": &mockPolicy{id: registry.NewID("policies", "admin")},
	}})
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	err := ApplySecurityPayload(ctx, &SecurityPayload{
		Actor:    &ActorPayload{ID: "attacker"},
		Policies: []string{"policies:admin", "policies:missing"},
		Scope:    true,
	})
	require.Error(t, err)
	_, hasActor := secapi.GetActor(ctx)
	_, hasScope := secapi.GetScope(ctx)
	assert.False(t, hasActor)
	assert.False(t, hasScope)
}
