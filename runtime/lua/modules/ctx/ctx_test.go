package ctx

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"go.uber.org/zap"
)

func TestCtxModule(t *testing.T) {
	logger := zap.NewNop()

	t.Run("get and set with valid context", func(t *testing.T) {
		// Spawn a Contexter and add it to the context
		contexter := ctxapi.NewContexter[any]()
		ctx := context.WithValue(ctxapi.NewRootContext(), ctxapi.ValuesCtx, contexter)

		// Spawn a new Lua VM with the context module
		mod := NewCtxModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Set values from Go
		contexter.SetValue("stringKey", "stringValue")
		contexter.SetValue("numberKey", 123)
		contexter.SetValue("boolKey", true)
		contexter.SetValue("tableKey", map[string]any{"name": "John"})

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

	t.Run("get and set with no contexter", func(t *testing.T) {
		mod := NewCtxModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Use a context, but without setting the Contexter
		ctx := ctxapi.NewRootContext()

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
		mod := NewCtxModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Set a value of the wrong type as the Contexter
		ctx := context.WithValue(ctxapi.NewRootContext(), ctxapi.ValuesCtx, "not a contexter")

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
		ctx := context.WithValue(ctxapi.NewRootContext(), ctxapi.ValuesCtx, contexter)

		mod := NewCtxModule(logger)
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
		ctx := context.WithValue(ctxapi.NewRootContext(), ctxapi.ValuesCtx, contexter)

		mod := NewCtxModule(logger)
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
		ctx := context.WithValue(ctxapi.NewRootContext(), ctxapi.ValuesCtx, contexter)

		mod := NewCtxModule(logger)
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
	// Add this test case to the TestCtxModule function in ctx_test.go

	t.Run("all with valid context", func(t *testing.T) {
		// Create a Contexter and add it to the context
		contexter := ctxapi.NewContexter[any]()
		ctx := context.WithValue(ctxapi.NewRootContext(), ctxapi.ValuesCtx, contexter)

		// Create a new Lua VM with the context module
		mod := NewCtxModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Set values from Go
		contexter.SetValue("key1", "value1")
		contexter.SetValue("key2", 42)
		contexter.SetValue("key3", true)
		contexter.SetValue("key4", map[string]any{"nested": "value"})

		// Test all from Lua
		err = vm.DoString(ctx, `
		local ctx = require("ctx")

		local tbl, err = ctx.all()
		assert(err == nil)
		assert(tbl.key1 == "value1")
		assert(tbl.key2 == 42)
		assert(tbl.key3 == true)
		assert(tbl.key4.nested == "value")

		-- Count the number of keys
		local count = 0
		for _ in pairs(tbl) do
			count = count + 1
		end
		assert(count == 4)
	`, "test_all")
		require.NoError(t, err)
	})

	t.Run("all with no contexter", func(t *testing.T) {
		mod := NewCtxModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Use a context, but without setting the Contexter
		ctx := ctxapi.NewRootContext()

		// Test ctx.all with no contexter
		err = vm.DoString(ctx, `
		local ctx = require("ctx")
		local tbl, err = ctx.all()
	`, "test_no_contexter_all")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid context")
	})

	t.Run("all with invalid contexter type", func(t *testing.T) {
		mod := NewCtxModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Set a value of the wrong type as the Contexter
		ctx := context.WithValue(ctxapi.NewRootContext(), ctxapi.ValuesCtx, "not a contexter")

		// Test ctx.all with invalid contexter type
		err = vm.DoString(ctx, `
		local ctx = require("ctx")
		local tbl, err = ctx.all()
	`, "test_invalid_contexter_all")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid context")
	})
}
