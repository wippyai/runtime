package env

import (
	"context"
	"testing"

	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestEnvModule(t *testing.T) {
	logger := zap.NewNop()

	t.Run("get environment variable", func(t *testing.T) {
		contexter := ctxapi.NewContexter[string]()
		contexter.WithValue("TEST_VAR", "test_value")
		ctx := context.WithValue(context.Background(), ctxapi.EnvCtx, contexter)

		mod := New(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
            local env = require("env")
            local value, err = env.get("TEST_VAR")
            assert(err == nil)
            assert(value == "test_value")
        `, "test_get")
		require.NoError(t, err)
	})

	t.Run("get non-existent variable", func(t *testing.T) {
		contexter := ctxapi.NewContexter[string]()
		ctx := context.WithValue(context.Background(), ctxapi.EnvCtx, contexter)

		mod := New(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
            local env = require("env")
            local value, err = env.get("NON_EXISTENT")
            assert(value == nil)
            assert(err == nil)
        `, "test_get_non_existent")
		require.NoError(t, err)
	})

	t.Run("get with empty key", func(t *testing.T) {
		contexter := ctxapi.NewContexter[string]()
		ctx := context.WithValue(context.Background(), ctxapi.EnvCtx, contexter)

		mod := New(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
            local env = require("env")
            local value, err = env.get("")
        `, "test_get_empty_key")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bad argument")
	})

	t.Run("get with no context", func(t *testing.T) {
		mod := New(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// This should cause a Lua error because there is no context
		err = vm.DoString(context.Background(), `
            local env = require("env")
            local value, err = env.get("TEST_VAR")
        `, "test_no_context")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid environment context")
	})

	t.Run("get with invalid context", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), ctxapi.EnvCtx, "not a contexter")

		mod := New(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
            local env = require("env")
            local value, err = env.get("TEST_VAR")
        `, "test_invalid_context")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid environment context")
	})
}
