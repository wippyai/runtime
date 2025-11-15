package ctx

import (
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/coroutine"
	"go.uber.org/zap"
)

func TestCtxModule(t *testing.T) {
	logger := zap.NewNop()

	t.Run("get and set with valid context", func(t *testing.T) {
		// Create VM with ctx module
		mod := NewCtxModule(logger)
		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		L := vm.State()
		L.PreloadModule(mod.Name(), mod.Loader)
		defer vm.Close()

		// Import test function
		err = vm.Import(`
			function test_ctx()
				local ctx = require("ctx")

				-- Set values
				local ok, err = ctx.set("stringKey", "stringValue")
				assert(ok, "set stringKey failed")

				ctx.set("numberKey", 123)
				ctx.set("boolKey", true)
				ctx.set("tableKey", {name = "John"})

				-- Get values back
				local strVal = ctx.get("stringKey")
				assert(strVal == "stringValue", "stringKey mismatch")

				local numVal = ctx.get("numberKey")
				assert(numVal == 123, "numberKey mismatch")

				local boolVal = ctx.get("boolKey")
				assert(boolVal == true, "boolKey mismatch")

				local tblVal = ctx.get("tableKey")
				assert(tblVal.name == "John", "tableKey mismatch")

				return true
			end
		`, "test", "test_ctx")
		require.NoError(t, err)

		// Create runner and context with Values
		runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		values := ctxapi.NewValues()
		_ = ctxapi.SetValues(ctx, values)

		// Execute
		result, err := runner.Execute(ctx, "test_ctx")
		require.NoError(t, err)
		assert.NotNil(t, result)
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

	t.Run("get and set with pre-populated values", func(t *testing.T) {
		// Create VM with ctx module
		mod := NewCtxModule(logger)
		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		L := vm.State()
		L.PreloadModule(mod.Name(), mod.Loader)
		defer vm.Close()

		// Import test function that reads pre-populated values
		err = vm.Import(`
			function test_get_prepopulated()
				local ctx = require("ctx")
				local val = ctx.get("presetKey")
				assert(val == "presetValue", "prepopulated value mismatch")
				return val
			end
		`, "test", "test_get_prepopulated")
		require.NoError(t, err)

		// Create runner and context with pre-populated Values
		runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		values := ctxapi.NewValues()
		values.Set("presetKey", "presetValue")
		_ = ctxapi.SetValues(ctx, values)

		// Execute
		result, err := runner.Execute(ctx, "test_get_prepopulated")
		require.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("get with empty key", func(t *testing.T) {
		mod := NewCtxModule(logger)
		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		L := vm.State()
		L.PreloadModule(mod.Name(), mod.Loader)
		defer vm.Close()

		err = vm.Import(`
			function test_empty_key()
				local ctx = require("ctx")
				local val = ctx.get("")
				return val
			end
		`, "test", "test_empty_key")
		require.NoError(t, err)

		runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		values := ctxapi.NewValues()
		_ = ctxapi.SetValues(ctx, values)

		_, err = runner.Execute(ctx, "test_empty_key")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty key provided")
	})

	t.Run("set with empty key", func(t *testing.T) {
		mod := NewCtxModule(logger)
		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		L := vm.State()
		L.PreloadModule(mod.Name(), mod.Loader)
		defer vm.Close()

		err = vm.Import(`
			function test_set_empty()
				local ctx = require("ctx")
				local ok = ctx.set("", "value")
				return ok
			end
		`, "test", "test_set_empty")
		require.NoError(t, err)

		runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		values := ctxapi.NewValues()
		_ = ctxapi.SetValues(ctx, values)

		_, err = runner.Execute(ctx, "test_set_empty")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty key provided")
	})

	t.Run("set with value conversion", func(t *testing.T) {
		mod := NewCtxModule(logger)
		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		L := vm.State()
		L.PreloadModule(mod.Name(), mod.Loader)
		defer vm.Close()

		err = vm.Import(`
			function test_set_function()
				local ctx = require("ctx")
				local ok = ctx.set("funcKey", function() end)
				return ok
			end
		`, "test", "test_set_function")
		require.NoError(t, err)

		runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		values := ctxapi.NewValues()
		_ = ctxapi.SetValues(ctx, values)

		_, err = runner.Execute(ctx, "test_set_function")
		// Functions can be stored, this should succeed
		require.NoError(t, err)
	})
	// Add this test case to the TestCtxModule function in ctx_test.go

	t.Run("all with valid context", func(t *testing.T) {
		mod := NewCtxModule(logger)
		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		L := vm.State()
		L.PreloadModule(mod.Name(), mod.Loader)
		defer vm.Close()

		err = vm.Import(`
			function test_all()
				local ctx = require("ctx")

				-- Set some values
				ctx.set("key1", "value1")
				ctx.set("key2", 42)
				ctx.set("key3", true)
				ctx.set("key4", {nested = "value"})

				-- Retrieve all
				local tbl = ctx.all()
				assert(tbl.key1 == "value1", "key1 mismatch")
				assert(tbl.key2 == 42, "key2 mismatch")
				assert(tbl.key3 == true, "key3 mismatch")
				assert(tbl.key4.nested == "value", "key4.nested mismatch")

				-- Count
				local count = 0
				for _ in pairs(tbl) do count = count + 1 end
				assert(count == 4, "expected 4 keys")

				return true
			end
		`, "test", "test_all")
		require.NoError(t, err)

		runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		values := ctxapi.NewValues()
		_ = ctxapi.SetValues(ctx, values)

		result, err := runner.Execute(ctx, "test_all")
		require.NoError(t, err)
		assert.NotNil(t, result)
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

	t.Run("all with prepopulated values", func(t *testing.T) {
		mod := NewCtxModule(logger)
		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		L := vm.State()
		L.PreloadModule(mod.Name(), mod.Loader)
		defer vm.Close()

		err = vm.Import(`
			function test_all_prepopulated()
				local ctx = require("ctx")
				local tbl = ctx.all()
				assert(tbl.preset1 == "value1", "preset1 mismatch")
				assert(tbl.preset2 == 123, "preset2 mismatch")
				return true
			end
		`, "test", "test_all_prepopulated")
		require.NoError(t, err)

		runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		values := ctxapi.NewValues()
		values.Set("preset1", "value1")
		values.Set("preset2", 123)
		_ = ctxapi.SetValues(ctx, values)

		result, err := runner.Execute(ctx, "test_all_prepopulated")
		require.NoError(t, err)
		assert.NotNil(t, result)
	})
}
