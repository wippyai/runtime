package payload

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Note: Tests for transcode() method are not included as they require
// a transcoder to be set in the Lua context, which needs integration
// with the full runtime environment.

func assertLua(l *lua.LState) int {
	if l.ToBool(1) {
		return 0
	}
	l.RaiseError("%s", l.OptString(2, "assertion failed!"))
	return 0
}

func TestPayloadModule(t *testing.T) {
	logger := zap.NewNop()

	t.Run("module creation and loading", func(t *testing.T) {
		mod := NewPayloadModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local payload = require("payload")
			assert(type(payload) == "table")
			assert(type(payload.new) == "function")
			assert(type(payload.format) == "table")
			assert(type(payload.format.JSON) == "string")
			assert(type(payload.format.YAML) == "string")
			assert(type(payload.format.STRING) == "string")
			assert(type(payload.format.GOLANG) == "string")
			assert(type(payload.format.LUA) == "string")
			assert(type(payload.format.BYTES) == "string")
			assert(type(payload.format.ERROR) == "string")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("payload creation", func(t *testing.T) {
		testCases := []struct {
			name   string
			script string
		}{
			{
				name: "create payload from string",
				script: `
					local payload = require("payload")
					local p = payload.new("hello world")
					assert(p ~= nil, "payload should not be nil")
					assert(type(p.get_format) == "function", "should have get_format method")
					assert(type(p.data) == "function", "should have data method")
					assert(type(p.transcode) == "function", "should have transcode method")
					assert(type(p.unmarshal) == "function", "should have unmarshal method")
				`,
			},
			{
				name: "create payload from number",
				script: `
					local payload = require("payload")
					local p = payload.new(42)
					assert(p ~= nil, "payload should not be nil")
				`,
			},
			{
				name: "create payload from table",
				script: `
					local payload = require("payload")
					local p = payload.new({name = "test", value = 123})
					assert(p ~= nil, "payload should not be nil")
				`,
			},
			{
				name: "create payload from boolean",
				script: `
					local payload = require("payload")
					local p = payload.new(true)
					assert(p ~= nil, "payload should not be nil")
				`,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewPayloadModule()
				vm, err := engine.NewVM(logger,
					engine.WithLoader(mod.Name(), mod.Loader),
					engine.WithGlobalFunction("assert", assertLua),
				)
				require.NoError(t, err)
				defer vm.Close()

				err = vm.DoString(context.Background(), tc.script, "test")
				assert.NoError(t, err)
			})
		}
	})

	t.Run("get_format method", func(t *testing.T) {
		mod := NewPayloadModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local payload = require("payload")
			local p = payload.new("test data")
			local format = p:get_format()
			assert(type(format) == "string", "format should be a string")
			assert(format == payload.format.LUA, "new payload should have LUA format")
		`

		err = vm.DoString(context.Background(), script, "test")
		assert.NoError(t, err)
	})

	t.Run("data method with Lua format", func(t *testing.T) {
		testCases := []struct {
			name   string
			script string
		}{
			{
				name: "string data",
				script: `
					local payload = require("payload")
					local p = payload.new("hello")
					local data = p:data()
					assert(data == "hello", "data should match original value")
				`,
			},
			{
				name: "number data",
				script: `
					local payload = require("payload")
					local p = payload.new(42.5)
					local data = p:data()
					assert(data == 42.5, "data should match original value")
				`,
			},
			{
				name: "boolean data",
				script: `
					local payload = require("payload")
					local p = payload.new(true)
					local data = p:data()
					assert(data == true, "data should match original value")
				`,
			},
			{
				name: "table data",
				script: `
					local payload = require("payload")
					local original = {name = "test", value = 123}
					local p = payload.new(original)
					local data = p:data()
					assert(type(data) == "table", "data should be a table")
					assert(data.name == "test", "table data should be preserved")
					assert(data.value == 123, "table data should be preserved")
				`,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewPayloadModule()
				vm, err := engine.NewVM(logger,
					engine.WithLoader(mod.Name(), mod.Loader),
					engine.WithGlobalFunction("assert", assertLua),
				)
				require.NoError(t, err)
				defer vm.Close()

				err = vm.DoString(context.Background(), tc.script, "test")
				assert.NoError(t, err)
			})
		}
	})

	t.Run("unmarshal method with Lua format", func(t *testing.T) {
		testCases := []struct {
			name   string
			script string
		}{
			{
				name: "unmarshal string",
				script: `
					local payload = require("payload")
					local p = payload.new("test string")
					local data = p:unmarshal()
					assert(data == "test string", "unmarshaled data should match original")
				`,
			},
			{
				name: "unmarshal table",
				script: `
					local payload = require("payload")
					local original = {x = 1, y = 2, nested = {a = "b"}}
					local p = payload.new(original)
					local data = p:unmarshal()
					assert(type(data) == "table", "unmarshaled data should be a table")
					assert(data.x == 1, "table structure should be preserved")
					assert(data.y == 2, "table structure should be preserved")
					assert(data.nested.a == "b", "nested structure should be preserved")
				`,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewPayloadModule()
				vm, err := engine.NewVM(logger,
					engine.WithLoader(mod.Name(), mod.Loader),
					engine.WithGlobalFunction("assert", assertLua),
				)
				require.NoError(t, err)
				defer vm.Close()

				err = vm.DoString(context.Background(), tc.script, "test")
				assert.NoError(t, err)
			})
		}
	})

	t.Run("nil payload creation", func(t *testing.T) {
		mod := NewPayloadModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local payload = require("payload")
			local p = payload.new(nil)
			assert(p ~= nil, "payload object should not be nil even for nil value")
			local data = p:data()
			assert(data == nil, "nil data should remain nil")
		`

		err = vm.DoString(context.Background(), script, "test")
		assert.NoError(t, err)
	})

	t.Run("format constants", func(t *testing.T) {
		mod := NewPayloadModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local payload = require("payload")
			local formats = payload.format
			
			-- Check all format constants exist
			assert(formats.JSON ~= nil, "JSON format should exist")
			assert(formats.YAML ~= nil, "YAML format should exist")
			assert(formats.STRING ~= nil, "STRING format should exist")
			assert(formats.GOLANG ~= nil, "GOLANG format should exist")
			assert(formats.LUA ~= nil, "LUA format should exist")
			assert(formats.BYTES ~= nil, "BYTES format should exist")
			assert(formats.ERROR ~= nil, "ERROR format should exist")
			
			-- Check they are all strings
			assert(type(formats.JSON) == "string", "JSON format should be string")
			assert(type(formats.YAML) == "string", "YAML format should be string")
			assert(type(formats.STRING) == "string", "STRING format should be string")
			assert(type(formats.GOLANG) == "string", "GOLANG format should be string")
			assert(type(formats.LUA) == "string", "LUA format should be string")
			assert(type(formats.BYTES) == "string", "BYTES format should be string")
			assert(type(formats.ERROR) == "string", "ERROR format should be string")
			
			-- Check they are all different
			assert(formats.JSON ~= formats.YAML, "formats should be different")
			assert(formats.JSON ~= formats.LUA, "formats should be different")
			assert(formats.YAML ~= formats.STRING, "formats should be different")
		`

		err = vm.DoString(context.Background(), script, "test")
		assert.NoError(t, err)
	})

	t.Run("payload wrapper functions", func(t *testing.T) {
		mod := NewPayloadModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		// Test that payloads can be created and their types are correct
		script := `
			local payload = require("payload")
			
			-- Create different types of payloads
			local str_payload = payload.new("string value")
			local num_payload = payload.new(123.45)
			local bool_payload = payload.new(false)
			local table_payload = payload.new({a = 1, b = 2})
			
			-- Verify they all have the same methods
			local methods = {"get_format", "data", "transcode", "unmarshal"}
			for _, method in ipairs(methods) do
				assert(type(str_payload[method]) == "function", "string payload should have " .. method)
				assert(type(num_payload[method]) == "function", "number payload should have " .. method)
				assert(type(bool_payload[method]) == "function", "boolean payload should have " .. method)
				assert(type(table_payload[method]) == "function", "table payload should have " .. method)
			end
			
			-- Verify formats are correct
			assert(str_payload:get_format() == payload.format.LUA, "string payload format")
			assert(num_payload:get_format() == payload.format.LUA, "number payload format")
			assert(bool_payload:get_format() == payload.format.LUA, "boolean payload format")
			assert(table_payload:get_format() == payload.format.LUA, "table payload format")
		`

		err = vm.DoString(context.Background(), script, "test")
		assert.NoError(t, err)
	})

	t.Run("complex nested data structures", func(t *testing.T) {
		mod := NewPayloadModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local payload = require("payload")
			
			-- Create complex nested structure
			local complex_data = {
				users = {
					{id = 1, name = "Alice", active = true, scores = {85, 92, 78}},
					{id = 2, name = "Bob", active = false, scores = {90, 88, 95}}
				},
				metadata = {
					version = "1.0",
					created = "2024-01-01",
					settings = {
						debug = false,
						features = {"auth", "logging", "metrics"}
					}
				},
				numbers = {1, 2.5, -3, 0, 999.999}
			}
			
			local p = payload.new(complex_data)
			assert(p ~= nil, "complex payload should not be nil")
			assert(p:get_format() == payload.format.LUA, "should be LUA format")
			
			local data = p:data()
			assert(type(data) == "table", "data should be a table")
			assert(#data.users == 2, "should have 2 users")
			assert(data.users[1].name == "Alice", "first user name should be preserved")
			assert(data.users[2].active == false, "user active status should be preserved")
			assert(#data.users[1].scores == 3, "user scores should be preserved")
			assert(data.metadata.settings.debug == false, "nested boolean should be preserved")
			assert(#data.metadata.settings.features == 3, "nested array should be preserved")
			assert(data.numbers[5] == 999.999, "decimal numbers should be preserved")
			
			-- Test unmarshal gives same result
			local unmarshaled = p:unmarshal()
			assert(unmarshaled.users[1].name == "Alice", "unmarshaled data should match")
			assert(#unmarshaled.metadata.settings.features == 3, "unmarshaled nested data should match")
		`

		err = vm.DoString(context.Background(), script, "test")
		assert.NoError(t, err)
	})
}

func TestPayloadHelperFunctions(t *testing.T) {
	logger := zap.NewNop()

	t.Run("CheckPayload function", func(t *testing.T) {
		mod := NewPayloadModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		// This test verifies that CheckPayload works correctly by calling methods
		script := `
			local payload = require("payload")
			local p = payload.new("test")
			
			-- If CheckPayload works, these method calls should succeed
			local format = p:get_format()
			local data = p:data()
			local unmarshaled = p:unmarshal()
			
			assert(format == payload.format.LUA, "format should be correct")
			assert(data == "test", "data should be correct")
			assert(unmarshaled == "test", "unmarshaled should be correct")
		`

		err = vm.DoString(context.Background(), script, "test")
		assert.NoError(t, err)
	})

	t.Run("PushPayload function", func(t *testing.T) {
		mod := NewPayloadModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		// Test that PushPayload correctly creates userdata that can be used
		script := `
			local payload = require("payload")
			local p1 = payload.new("first")
			local p2 = payload.new("second")
			
			-- Both should be usable payload objects
			assert(p1:get_format() == payload.format.LUA, "first payload should work")
			assert(p2:get_format() == payload.format.LUA, "second payload should work")
			assert(p1:data() == "first", "first payload data should be correct")
			assert(p2:data() == "second", "second payload data should be correct")
		`

		err = vm.DoString(context.Background(), script, "test")
		assert.NoError(t, err)
	})
}

func TestPayloadEdgeCases(t *testing.T) {
	logger := zap.NewNop()

	t.Run("empty string payload", func(t *testing.T) {
		mod := NewPayloadModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local payload = require("payload")
			local p = payload.new("")
			assert(p:data() == "", "empty string should be preserved")
			assert(p:unmarshal() == "", "empty string should unmarshal correctly")
		`

		err = vm.DoString(context.Background(), script, "test")
		assert.NoError(t, err)
	})

	t.Run("zero number payload", func(t *testing.T) {
		mod := NewPayloadModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local payload = require("payload")
			local p = payload.new(0)
			assert(p:data() == 0, "zero should be preserved")
			assert(p:unmarshal() == 0, "zero should unmarshal correctly")
		`

		err = vm.DoString(context.Background(), script, "test")
		assert.NoError(t, err)
	})

	t.Run("empty table payload", func(t *testing.T) {
		mod := NewPayloadModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local payload = require("payload")
			local p = payload.new({})
			local data = p:data()
			assert(type(data) == "table", "empty table should be a table")
			-- In Lua, tables don't have a reliable length when empty, so we check type
			
			local unmarshaled = p:unmarshal()
			assert(type(unmarshaled) == "table", "empty table should unmarshal as table")
		`

		err = vm.DoString(context.Background(), script, "test")
		assert.NoError(t, err)
	})

	t.Run("very large numbers", func(t *testing.T) {
		mod := NewPayloadModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local payload = require("payload")
			local large_num = 9007199254740992  -- Near JavaScript MAX_SAFE_INTEGER
			local p = payload.new(large_num)
			assert(p:data() == large_num, "large number should be preserved")
			assert(p:unmarshal() == large_num, "large number should unmarshal correctly")
		`

		err = vm.DoString(context.Background(), script, "test")
		assert.NoError(t, err)
	})

	t.Run("negative numbers", func(t *testing.T) {
		mod := NewPayloadModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local payload = require("payload")
			local neg_num = -123.456
			local p = payload.new(neg_num)
			assert(p:data() == neg_num, "negative number should be preserved")
			assert(p:unmarshal() == neg_num, "negative number should unmarshal correctly")
		`

		err = vm.DoString(context.Background(), script, "test")
		assert.NoError(t, err)
	})
}

func TestPayloadTypeConsistency(t *testing.T) {
	logger := zap.NewNop()

	t.Run("type consistency across operations", func(t *testing.T) {
		mod := NewPayloadModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local payload = require("payload")
			
			-- Test different types maintain consistency
			local test_values = {
				"string_value",
				42,
				3.14159,
				true,
				false,
				{key = "value", num = 123},
				{1, 2, 3, "four", true}
			}
			
			for i, value in ipairs(test_values) do
				local p = payload.new(value)
				local original_type = type(value)
				local data = p:data()
				local unmarshaled = p:unmarshal()
				
				assert(type(data) == original_type, 
					"data() should preserve type for " .. original_type .. " at index " .. i)
				assert(type(unmarshaled) == original_type, 
					"unmarshal() should preserve type for " .. original_type .. " at index " .. i)
				
				-- For simple types, values should be identical
				if original_type ~= "table" then
					assert(data == value, "data should equal original for " .. original_type)
					assert(unmarshaled == value, "unmarshaled should equal original for " .. original_type)
				end
			end
		`

		err = vm.DoString(context.Background(), script, "test")
		assert.NoError(t, err)
	})
}
