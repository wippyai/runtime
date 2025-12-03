package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
)

func TestContext_Actor(t *testing.T) {
	t.Run("with frame context", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)

		actor, ok := GetActor(ctx)
		assert.False(t, ok)
		assert.Equal(t, Actor{}, actor)

		testActor := Actor{ID: "user123"}
		err := SetActor(ctx, testActor)
		require.NoError(t, err)

		retrieved, ok := GetActor(ctx)
		assert.True(t, ok)
		assert.Equal(t, testActor, retrieved)
	})

	t.Run("without frame context", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()

		actor, ok := GetActor(ctx)
		assert.False(t, ok)
		assert.Equal(t, Actor{}, actor)

		testActor := Actor{ID: "user123"}
		err := SetActor(ctx, testActor)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no frame context available")
	})
}

func TestContext_Scope(t *testing.T) {
	t.Run("with frame context", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)

		scope, ok := GetScope(ctx)
		assert.False(t, ok)
		assert.Nil(t, scope)

		type mockScope struct{ Scope }
		testScope := &mockScope{}
		err := SetScope(ctx, testScope)
		require.NoError(t, err)

		retrieved, ok := GetScope(ctx)
		assert.True(t, ok)
		assert.Equal(t, testScope, retrieved)
	})

	t.Run("without frame context", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()

		scope, ok := GetScope(ctx)
		assert.False(t, ok)
		assert.Nil(t, scope)

		type mockScope struct{ Scope }
		testScope := &mockScope{}
		err := SetScope(ctx, testScope)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no frame context available")
	})
}

func TestContext_Registry(t *testing.T) {
	t.Run("with app context", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()

		reg, ok := GetRegistry(ctx)
		assert.False(t, ok)
		assert.Nil(t, reg)

		type mockRegistry struct{ Registry }
		mockReg := &mockRegistry{}

		ctx = WithRegistry(ctx, mockReg)

		retrieved, ok := GetRegistry(ctx)
		assert.True(t, ok)
		assert.Equal(t, mockReg, retrieved)
	})

	t.Run("without app context", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()

		type mockRegistry struct{ Registry }
		mockReg := &mockRegistry{}

		ctx = WithRegistry(ctx, mockReg)

		reg, ok := GetRegistry(ctx)
		assert.True(t, ok)
		assert.NotNil(t, reg)
	})
}

func TestContext_WithPolicy(t *testing.T) {
	t.Run("without scope", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)

		type mockPolicy struct{ Policy }
		testPolicy := &mockPolicy{}
		err := WithPolicy(ctx, testPolicy)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "security scope not found in context")
	})
}

func TestContext_ActorPair(t *testing.T) {
	testActor := Actor{ID: "user123"}
	pair := ActorPair(testActor)

	assert.Equal(t, actorCtx, pair.Key)
	assert.Equal(t, testActor, pair.Value)
}

func TestContext_ScopePair(t *testing.T) {
	type mockScope struct{ Scope }
	testScope := &mockScope{}
	pair := ScopePair(testScope)

	assert.Equal(t, scopeCtx, pair.Key)
	assert.Equal(t, testScope, pair.Value)
}

func TestContext_GetPolicy(t *testing.T) {
	t.Run("without registry", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()

		testID := registry.NewID("policies", "admin")
		_, err := GetPolicy(ctx, testID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "security registry not found in context")
	})
}

func TestContext_GetPolicyGroup(t *testing.T) {
	t.Run("without registry", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()

		testID := registry.NewID("groups", "admins")
		_, err := GetPolicyGroup(ctx, testID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "security registry not found in context")
	})
}

func TestContext_IsAllowed(t *testing.T) {
	t.Run("without actor or scope", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)

		allowed := IsAllowed(ctx, "read", "resource", nil)
		assert.False(t, allowed)
	})

	t.Run("with actor but no scope", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)

		testActor := Actor{ID: "user123"}
		err := SetActor(ctx, testActor)
		require.NoError(t, err)

		allowed := IsAllowed(ctx, "read", "resource", nil)
		assert.False(t, allowed)
	})

	t.Run("with scope but no actor", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)

		type mockScope struct{ Scope }
		testScope := &mockScope{}
		err := SetScope(ctx, testScope)
		require.NoError(t, err)

		allowed := IsAllowed(ctx, "read", "resource", nil)
		assert.False(t, allowed)
	})
}
