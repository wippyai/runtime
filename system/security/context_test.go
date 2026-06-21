// SPDX-License-Identifier: MPL-2.0

package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/system/eventbus"
)

func TestWithSecurityConfig(t *testing.T) {
	rootCtx := ctxapi.NewRootContext()
	ctx, _ := ctxapi.OpenFrameContext(rootCtx)

	result := WithSecurityConfig(ctx, nil)
	assert.Equal(t, ctx, result)

	emptyConfig := &security.Config{}
	result = WithSecurityConfig(ctx, emptyConfig)
	_, ok := security.GetActor(result)
	assert.True(t, ok)

	actorConfig := &security.Config{
		Actor: security.Actor{ID: "test-user"},
	}
	result = WithSecurityConfig(ctx, actorConfig)
	_, ok = security.GetActor(result)
	assert.True(t, ok)

	policyConfig := &security.Config{
		Actor:        security.Actor{ID: "test-user"},
		Policies:     []registry.ID{registry.NewID("test", "policy1")},
		PolicyGroups: []registry.ID{registry.NewID("test", "group1")},
	}
	result = WithSecurityConfig(ctx, policyConfig)
	_, ok = security.GetActor(result)
	assert.True(t, ok)
	_, ok = security.GetScope(result)
	assert.False(t, ok)

	reg := NewPolicyRegistry(eventbus.NewBus(), nil)
	ctxWithReg := security.WithRegistry(ctx, reg)
	result = WithSecurityConfig(ctxWithReg, policyConfig)

	_, ok = security.GetActor(result)
	assert.True(t, ok)
	_, ok = security.GetScope(result)
	assert.False(t, ok)
}

func TestWithSecurityConfig_PreservesExistingActorWhenConfigHasNoActor(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx, fc := ctxapi.OpenFrameContext(ctx)
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(fc) })

	require.NoError(t, security.SetActor(ctx, security.Actor{ID: "caller"}))

	result := WithSecurityConfig(ctx, &security.Config{
		PolicyGroups: []registry.ID{registry.NewID("test", "group1")},
	})

	actor, ok := security.GetActor(result)
	require.True(t, ok)
	assert.Equal(t, "caller", actor.ID)
}

func TestWithSecurityConfig_MergesPoliciesIntoExistingScope(t *testing.T) {
	reg := NewPolicyRegistry(eventbus.NewBus(), nil)
	existingPolicy := newMockPolicy("existing", security.Allow)
	addedPolicy := newMockPolicy("added", security.Deny)
	addedPolicyID := addedPolicy.ID()

	reg.handleEvent(event.Event{
		Kind: security.PolicyRegister,
		Path: addedPolicyID.String(),
		Data: &security.PolicyEntry{Policy: addedPolicy},
	})

	ctx := security.WithRegistry(ctxapi.NewRootContext(), reg)
	ctx, fc := ctxapi.OpenFrameContext(ctx)
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(fc) })

	require.NoError(t, security.SetActor(ctx, security.Actor{ID: "caller"}))
	require.NoError(t, security.SetScope(ctx, NewScope([]security.Policy{existingPolicy})))

	result := WithSecurityConfig(ctx, &security.Config{
		Policies: []registry.ID{addedPolicyID},
	})

	scope, ok := security.GetScope(result)
	require.True(t, ok)
	assert.True(t, scope.Contains(existingPolicy.ID()))
	assert.True(t, scope.Contains(addedPolicyID))
}
