// SPDX-License-Identifier: MPL-2.0

package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/system/eventbus"
)

func TestResolveConfigPairs(t *testing.T) {
	rootCtx := ctxapi.NewRootContext()
	ctx, fc := ctxapi.OpenFrameContext(rootCtx)
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(fc) })

	t.Run("nil config resolves to no pairs", func(t *testing.T) {
		pairs, err := ResolveConfigPairs(ctx, nil)
		require.NoError(t, err)
		assert.Nil(t, pairs)
	})

	t.Run("actor without policy references needs no registry", func(t *testing.T) {
		pairs, err := ResolveConfigPairs(ctx, &security.Config{
			Actor: security.Actor{ID: "wippy.test:runner"},
		})
		require.NoError(t, err)
		require.Len(t, pairs, 1)
		actor, ok := pairs[0].Value.(security.Actor)
		require.True(t, ok)
		assert.Equal(t, "wippy.test:runner", actor.ID)
	})

	t.Run("declared references require a registry", func(t *testing.T) {
		pairs, err := ResolveConfigPairs(ctx, &security.Config{
			Actor:    security.Actor{ID: "wippy.test:runner"},
			Policies: []registry.ID{registry.NewID("test", "policy")},
		})
		assert.Nil(t, pairs)
		require.EqualError(t, err, "security registry not available")
	})

	t.Run("unresolvable references return no partial pairs", func(t *testing.T) {
		reg := NewPolicyRegistry(eventbus.NewBus(), nil)
		regCtx := security.WithRegistry(ctx, reg)
		pairs, err := ResolveConfigPairs(regCtx, &security.Config{
			Actor:    security.Actor{ID: "a"},
			Policies: []registry.ID{registry.NewID("test", "missing")},
		})
		assert.Nil(t, pairs)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "test:missing")
	})

	t.Run("mixed valid and missing references are atomic", func(t *testing.T) {
		reg := NewPolicyRegistry(eventbus.NewBus(), nil)
		valid := newMockPolicy("valid", security.Allow)
		validID := valid.ID()
		reg.handleEvent(event.Event{
			Kind: security.PolicyRegister,
			Path: validID.String(),
			Data: &security.PolicyEntry{Policy: valid},
		})
		regCtx := security.WithRegistry(ctx, reg)
		pairs, err := ResolveConfigPairs(regCtx, &security.Config{
			Actor: security.Actor{ID: "a"},
			Policies: []registry.ID{
				validID,
				registry.NewID("test", "missing"),
			},
		})
		assert.Nil(t, pairs)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "test:missing")
	})

	t.Run("empty declared group is rejected", func(t *testing.T) {
		reg := NewPolicyRegistry(eventbus.NewBus(), nil)
		groupID := registry.NewID("test", "empty")
		reg.groups.Store(groupID, []registry.ID{})
		emptyRoot := ctxapi.NewRootContext()
		regCtx, emptyFrame := ctxapi.OpenFrameContext(emptyRoot)
		t.Cleanup(func() { ctxapi.ReleaseFrameContext(emptyFrame) })
		regCtx = security.WithRegistry(regCtx, reg)
		pairs, err := ResolveConfigPairs(regCtx, &security.Config{
			Actor:        security.Actor{ID: "a"},
			PolicyGroups: []registry.ID{groupID},
		})
		assert.Nil(t, pairs)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no policies")
	})

	t.Run("pairs install actor and scope into a frame context", func(t *testing.T) {
		pairs, err := ResolveConfigPairs(ctx, &security.Config{Actor: security.Actor{ID: "a"}})
		require.NoError(t, err)
		childCtx, childFC := ctxapi.OpenFrameContext(rootCtx)
		t.Cleanup(func() { ctxapi.ReleaseFrameContext(childFC) })
		require.NoError(t, childFC.SetMultiple(pairs...))
		actor, ok := security.GetActor(childCtx)
		require.True(t, ok)
		assert.Equal(t, "a", actor.ID)
	})
}

func TestResolveConfigPairs_TransportsInheritedSecurityToProcessFrame(t *testing.T) {
	existingPolicy := newMockPolicy("existing", security.Allow)
	addedPolicy := newMockPolicy("added", security.Deny)
	addedPolicyID := addedPolicy.ID()
	reg := NewPolicyRegistry(eventbus.NewBus(), nil)
	reg.handleEvent(event.Event{
		Kind: security.PolicyRegister,
		Path: addedPolicyID.String(),
		Data: &security.PolicyEntry{Policy: addedPolicy},
	})

	rootCtx := security.WithRegistry(ctxapi.NewRootContext(), reg)
	callerCtx, callerFrame := ctxapi.OpenFrameContext(rootCtx)
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(callerFrame) })
	require.NoError(t, security.SetActor(callerCtx, security.Actor{
		ID:   "caller",
		Meta: attrs.Bag{"tenant": "acme"},
	}))
	require.NoError(t, security.SetScope(callerCtx, NewScope([]security.Policy{existingPolicy})))

	pairs, err := ResolveConfigPairs(callerCtx, &security.Config{
		Policies: []registry.ID{addedPolicyID},
	})
	require.NoError(t, err)

	// This is the same frame boundary used by terminal.Host.prepareContext:
	// inherit the caller, then apply process.Start.Context pairs.
	processCtx, processFrame := ctxapi.OpenFrameContextOn(rootCtx, callerCtx)
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(processFrame) })
	require.NoError(t, processFrame.SetMultiple(pairs...))

	actor, ok := security.GetActor(processCtx)
	require.True(t, ok)
	assert.Equal(t, "caller", actor.ID)
	assert.Equal(t, "acme", actor.Meta["tenant"])
	scope, ok := security.GetScope(processCtx)
	require.True(t, ok)
	assert.True(t, scope.Contains(existingPolicy.ID()))
	assert.True(t, scope.Contains(addedPolicyID))
}
