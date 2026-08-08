// SPDX-License-Identifier: MPL-2.0

package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/system/eventbus"
)

func TestResolveConfigPairs(t *testing.T) {
	rootCtx := ctxapi.NewRootContext()
	ctx, fc := ctxapi.OpenFrameContext(rootCtx)
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(fc) })

	t.Run("nil config resolves to no pairs", func(t *testing.T) {
		assert.Nil(t, ResolveConfigPairs(ctx, nil))
	})

	t.Run("actor without registry resolves to the actor pair only", func(t *testing.T) {
		pairs := ResolveConfigPairs(ctx, &security.Config{
			Actor:    security.Actor{ID: "wippy.test:runner"},
			Policies: []registry.ID{registry.NewID("test", "policy")},
		})
		require.Len(t, pairs, 1)
		actor, ok := pairs[0].Value.(security.Actor)
		require.True(t, ok)
		assert.Equal(t, "wippy.test:runner", actor.ID)
	})

	t.Run("unresolvable references are skipped", func(t *testing.T) {
		reg := NewPolicyRegistry(eventbus.NewBus(), nil)
		regCtx := security.WithRegistry(ctx, reg)
		pairs := ResolveConfigPairs(regCtx, &security.Config{
			Actor:    security.Actor{ID: "a"},
			Policies: []registry.ID{registry.NewID("test", "missing")},
		})
		require.Len(t, pairs, 1)
	})

	t.Run("pairs install actor and scope into a frame context", func(t *testing.T) {
		pairs := ResolveConfigPairs(ctx, &security.Config{Actor: security.Actor{ID: "a"}})
		childCtx, childFC := ctxapi.OpenFrameContext(rootCtx)
		t.Cleanup(func() { ctxapi.ReleaseFrameContext(childFC) })
		require.NoError(t, childFC.SetMultiple(pairs...))
		actor, ok := security.GetActor(childCtx)
		require.True(t, ok)
		assert.Equal(t, "a", actor.ID)
	})
}
