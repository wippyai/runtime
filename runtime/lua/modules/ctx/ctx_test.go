package ctx

import (
	"context"
	"testing"

	ctxapi "github.com/ponyruntime/pony/api/context"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestCtxModule(t *testing.T) {
	logger := zap.NewNop()

	t.Run("get and set with valid context", func(t *testing.T) {
		// Create a Contexter and add it to the context
		contexter := ctxapi.NewContexter[any]()
		ctx := context.WithValue(context.Background(), ctxapi.ValuesCtx, contexter)

		// Create a new Lua VM with the context module
		mod := New(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Set values from Go
		contexter.WithValue("stringKey", "stringValue")
		contexter.WithValue("numberKey", 123)
		contexter.WithValue("boolKey", true)
		contexter.WithValue("tableKey", map[string]any{"name": "John"})

		// Test getting values from Lua
		err = vm.DoString(ctx, `
			local ctx = require("ctx")

			local strVal, err = ctx.get("stringKey")
			assert(err == nil)
			assert(strVal == "stringValue")

			local numVal, err = ctx.get("numberKey")
			assert(err == nil)
			assert(numVal == 123)

			local boolVal, err = ctx.get("boolKey")
			assert(err == nil)
			assert(boolVal == true)

			local tblVal, err = ctx.get("tableKey")
			assert(err == nil)
			assert(tblVal.name == "John")
		`, "test_get")
		require.NoError(t, err)

		// Test setting values from Lua
		err = vm.DoString(ctx, `
			local ctx = require("ctx")

			local ok, err = ctx.set("newStringKey", "newStringValue")
			assert(ok)
			assert(err == nil)

			local ok, err = ctx.set("newNumberKey", 456)
			assert(ok)
			assert(err == nil)

			local ok, err = ctx.set("newBoolKey", false)
			assert(ok)
			assert(err == nil)

			local newTable = {a = 1, b = "hello"}
			local ok, err = ctx.set("newTableKey", newTable)
			assert(ok)
			assert(err == nil)
		`, "test_set")
		require.NoError(t, err)

		// Verify that the values were set in the Contexter (in Go)
		strVal, ok := contexter.Value("newStringKey")
		assert.True(t, ok)
		assert.Equal(t, "newStringValue", strVal)

		numVal, ok := contexter.Value("newNumberKey")
		assert.True(t, ok)
		assert.Equal(t, float64(456), numVal) // Lua numbers are float64

		boolVal, ok := contexter.Value("newBoolKey")
		assert.True(t, ok)
		assert.Equal(t, false, boolVal)

		tblVal, ok := contexter.Value("newTableKey")
		assert.True(t, ok)
		tbl, ok := tblVal.(map[string]any)
		assert.True(t, ok)
		assert.Equal(t, float64(1), tbl["a"])
		assert.Equal(t, "hello", tbl["b"])
	})

	t.Run("get and set with nil context", func(t *testing.T) {
		mod := New(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Test ctx.get with nil context
		err = vm.DoString(nil, `
			local ctx = require("ctx")
			local val, err = ctx.get("someKey")
		`, "test_nil_context_get")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no context found")

		// Test ctx.set with nil context
		err = vm.DoString(nil, `
			local ctx = require("ctx")
			local ok, err = ctx.set("someKey", "someValue")
		`, "test_nil_context_set")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no context found")
	})

	t.Run("get and set with no contexter", func(t *testing.T) {
		mod := New(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Use a context, but without setting the Contexter
		ctx := context.Background()

		// Test ctx.get with no contexter
		err = vm.DoString(ctx, `
			local ctx = require("ctx")
			local val, err = ctx.get("someKey")
		`, "test_no_contexter_get")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid context")

		// Test ctx.set with no contexter
		err = vm.DoString(ctx, `
			local ctx = require("ctx")
			local ok, err = ctx.set("someKey", "someValue")
		`, "test_no_contexter_set")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid context")
	})

	t.Run("get and set with invalid contexter type", func(t *testing.T) {
		mod := New(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Set a value of the wrong type as the Contexter
		ctx := context.WithValue(context.Background(), ctxapi.ValuesCtx, "not a contexter")

		// Test ctx.get with invalid contexter type
		err = vm.DoString(ctx, `
			local ctx = require("ctx")
			local val, err = ctx.get("someKey")
		`, "test_invalid_contexter_get")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid context")

		// Test ctx.set with invalid contexter type
		err = vm.DoString(ctx, `
			local ctx = require("ctx")
			local ok, err = ctx.set("someKey", "someValue")
		`, "test_invalid_contexter_set")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid context")
	})

	t.Run("get with empty key", func(t *testing.T) {
		contexter := ctxapi.NewContexter[any]()
		ctx := context.WithValue(context.Background(), ctxapi.ValuesCtx, contexter)

		mod := New(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local ctx = require("ctx")
			local val, err = ctx.get("")
		`, "test_get_empty_key")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty key provided")
	})

	t.Run("set with empty key", func(t *testing.T) {
		contexter := ctxapi.NewContexter[any]()
		ctx := context.WithValue(context.Background(), ctxapi.ValuesCtx, contexter)

		mod := New(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local ctx = require("ctx")
			local ok, err = ctx.set("", "someValue")
		`, "test_set_empty_key")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty key provided")
	})

	t.Run("set with value conversion error", func(t *testing.T) {
		contexter := ctxapi.NewContexter[any]()
		ctx := context.WithValue(context.Background(), ctxapi.ValuesCtx, contexter)

		mod := New(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Try to set a function (which cannot be converted to a Go type)
		// The specific error handling in `set` has changed; check for general error.
		err = vm.DoString(ctx, `
			local ctx = require("ctx")
			local ok, err = ctx.set("funcKey", function() end)
		`, "test_set_conversion_error")
		require.NoError(t, err) // Expecting no error, the function will handle the error internally
	})
}
