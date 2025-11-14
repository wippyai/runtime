// Package pidgen provides process ID generation.
package pidgen

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/internal/uniqid"
)

func TestContext_Generator(t *testing.T) {
	t.Run("with app context", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()

		gen := GetGenerator(ctx)
		assert.Nil(t, gen)

		mockGen := &uniqid.PIDGenerator{}
		ctx = WithGenerator(ctx, mockGen)

		retrieved := GetGenerator(ctx)
		assert.Equal(t, mockGen, retrieved)
	})

	t.Run("without app context", func(t *testing.T) {
		ctx := context.Background()

		gen := GetGenerator(ctx)
		assert.Nil(t, gen)

		mockGen := &uniqid.PIDGenerator{}
		ctx = WithGenerator(ctx, mockGen)
		assert.Equal(t, context.Background(), ctx)

		gen = GetGenerator(ctx)
		assert.Nil(t, gen)
	})

	t.Run("wrong type in context", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()
		ac := ctxapi.AppFromContext(ctx)

		// Set wrong type
		ac.With(generatorCtx, "wrong type")

		gen := GetGenerator(ctx)
		assert.Nil(t, gen)
	})

	t.Run("set twice returns same context", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()

		gen1 := &uniqid.PIDGenerator{}
		ctx = WithGenerator(ctx, gen1)

		gen2 := &uniqid.PIDGenerator{}
		ctx2 := WithGenerator(ctx, gen2)

		// Should return same context since already set
		assert.Equal(t, ctx, ctx2)

		// Should still have first generator
		retrieved := GetGenerator(ctx)
		assert.Equal(t, gen1, retrieved)
	})
}
