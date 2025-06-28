package json

import (
	"context"
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func assertLua(l *lua.LState) int {
	if l.ToBool(1) {
		return 0
	}
	l.RaiseError("%s", l.OptString(2, "assertion failed!"))
	return 0
}

func TestJsonModule(t *testing.T) {
	logger := zap.NewNop()

	t.Run("module creation and loading", func(t *testing.T) {
		mod := NewJSONModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local json = require("json")
			assert(type(json) == "table")
			assert(type(json.encode) == "function")
			assert(type(json.decode) == "function")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("encode", func(t *testing.T) {
		testCases := []struct {
			name     string
			script   string
			expected string
		}{
			{
				name: "simple string",
				script: `
					local json = require("json")
					local value = "hello"
					return json.encode(value)
				`,
				expected: `"hello"`,
			},
			{
				name: "number",
				script: `
					local json = require("json")
					local value = 42.5
					return json.encode(value)
				`,
				expected: "42.5",
			},
			{
				name: "boolean",
				script: `
					local json = require("json")
					local value = true
					return json.encode(value)
				`,
				expected: "true",
			},
			{
				name: "null",
				script: `
					local json = require("json")
					local value = nil
					return json.encode(value)
				`,
				expected: "null",
			},
			{
				name: "array",
				script: `
					local json = require("json")
					local value = {1, 2, 3, "four", true}
					return json.encode(value)
				`,
				expected: `[1,2,3,"four",true]`,
			},
			{
				name: "object",
				script: `
					local json = require("json")
					local value = {name = "test", age = 25, active = true}
					return json.encode(value)
				`,
				expected: `{"active":true,"age":25,"name":"test"}`,
			},
			{
				name: "nested structure",
				script: `
					local json = require("json")
					local value = {
						name = "test",
						data = {
							items = {1, 2, 3},
							active = true
						}
					}
					return json.encode(value)
				`,
				expected: `{"data":{"active":true,"items":[1,2,3]},"name":"test"}`,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewJSONModule()
				vm, err := engine.NewVM(logger,
					engine.WithLoader(mod.Name(), mod.Loader),
					engine.WithGlobalFunction("assert", assertLua),
				)
				require.NoError(t, err)
				defer vm.Close()

				err = vm.DoString(context.Background(), tc.script, "test")
				require.NoError(t, err)

				result := vm.State().Get(-1)
				assert.Equal(t, tc.expected, result.String())
				vm.State().Pop(1)
			})
		}
	})

	t.Run("decode", func(t *testing.T) {
		testCases := []struct {
			name   string
			script string
		}{
			{
				name: "simple string",
				script: `
					local json = require("json")
					local value = json.decode('"hello"')
					assert(value == "hello", "value should be 'hello'")
				`,
			},
			{
				name: "number",
				script: `
					local json = require("json")
					local value = json.decode("42.5")
					assert(value == 42.5, "value should be 42.5")
				`,
			},
			{
				name: "boolean",
				script: `
					local json = require("json")
					local value = json.decode("true")
					assert(value == true, "value should be true")
				`,
			},
			{
				name: "null",
				script: `
					local json = require("json")
					local value = json.decode("null")
					assert(value == nil, "value should be nil")
				`,
			},
			{
				name: "array",
				script: `
					local json = require("json")
					local value = json.decode('[1,2,3,"four",true]')
					assert(#value == 5, "array should have 5 elements")
					assert(value[1] == 1)
					assert(value[2] == 2)
					assert(value[3] == 3)
					assert(value[4] == "four")
					assert(value[5] == true)
				`,
			},
			{
				name: "object",
				script: `
					local json = require("json")
					local value = json.decode('{"name":"test","age":25,"active":true}')
					assert(value.name == "test")
					assert(value.age == 25)
					assert(value.active == true)
				`,
			},
			{
				name: "nested structure",
				script: `
					local json = require("json")
					local value = json.decode('{"data":{"items":[1,2,3],"active":true},"name":"test"}')
					assert(value.name == "test")
					assert(value.data.active == true)
					assert(#value.data.items == 3)
					assert(value.data.items[1] == 1)
					assert(value.data.items[2] == 2)
					assert(value.data.items[3] == 3)
				`,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewJSONModule()
				vm, err := engine.NewVM(logger,
					engine.WithLoader(mod.Name(), mod.Loader),
					engine.WithGlobalFunction("assert", assertLua),
				)
				require.NoError(t, err)
				defer vm.Close()

				err = vm.DoString(context.Background(), tc.script, "test")
				require.NoError(t, err)
			})
		}
	})

	t.Run("error cases", func(t *testing.T) {
		testCases := []struct {
			name          string
			script        string
			expectedError string
		}{
			{
				name: "invalid json decode",
				script: `
					local json = require("json")
					local value, err = json.decode("invalid json")
					return value, err
				`,
				expectedError: "invalid character",
			},
			{
				name: "encode recursive table",
				script: `
					local json = require("json")
					local t = {}
					t.x = t
					local value, err = json.encode(t)
					return value, err
				`,
				expectedError: "cannot encode recursively nested tables",
			},
			{
				name: "encode mixed keys",
				script: `
					local json = require("json")
					local t = {}
					t[1] = 1
					t["key"] = 2
					local value, err = json.encode(t)
					return value, err
				`,
				expectedError: "table has both numeric and non-numeric keys",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewJSONModule()
				vm, err := engine.NewVM(logger,
					engine.WithLoader(mod.Name(), mod.Loader),
					engine.WithGlobalFunction("assert", assertLua),
				)
				require.NoError(t, err)
				defer vm.Close()

				err = vm.DoString(context.Background(), tc.script, "test")
				require.NoError(t, err)

				errStr := vm.State().Get(-1).String()
				assert.Contains(t, errStr, tc.expectedError)
				vm.State().Pop(2) // pop both nil and error
			})
		}
	})

	t.Run("round trip", func(t *testing.T) {
		mod := NewJSONModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local json = require("json")
			local original = {
				name = "test",
				numbers = {1, 2, 3},
				nested = {
					active = true,
					data = {
						x = 1,
						y = 2
					}
				}
			}
			local encoded = json.encode(original)
			local decoded = json.decode(encoded)
			
			assert(decoded.name == original.name)
			assert(#decoded.numbers == #original.numbers)
			for i=1,#original.numbers do
				assert(decoded.numbers[i] == original.numbers[i])
			end
			assert(decoded.nested.active == original.nested.active)
			assert(decoded.nested.data.x == original.nested.data.x)
			assert(decoded.nested.data.y == original.nested.data.y)
			
			return encoded
		`

		err = vm.DoString(context.Background(), script, "test")
		require.NoError(t, err)
	})
}

func TestSharedTableReferences(t *testing.T) {
	logger := zap.NewNop()

	t.Run("non-recursive shared table references", func(t *testing.T) {
		mod := NewJSONModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local json = require("json")
			
			-- Create a shared table that will be referenced multiple times
			local shared = {name = "shared_table", value = 42}
			
			-- Create a structure with multiple references to the same table
			local container = {
				first = shared,
				second = shared,  -- Same table reference, not recursive
				third = {x = 1, y = 2}  -- Different table
			}
			
			-- This should succeed because while the table is reused, there's no circular reference
			local result, err = json.encode(container)
			
			if err then
				return nil, "Failed to encode shared tables: " .. err
			end
			
			-- Decode to verify the structure
			local decoded = json.decode(result)
			assert(decoded.first.name == "shared_table")
			assert(decoded.second.name == "shared_table")
			assert(decoded.first.value == 42)
			assert(decoded.second.value == 42)
			
			return result
		`

		err = vm.DoString(context.Background(), script, "test")

		// With the current implementation, this should fail due to false positive detection
		// of shared references as recursion
		if err == nil {
			result := vm.State().Get(-1).String()
			assert.Contains(t, result, `"first":{"name":"shared_table","value":42}`)
			assert.Contains(t, result, `"second":{"name":"shared_table","value":42}`)
		} else {
			// This assertion will fail with the current implementation
			assert.Fail(t, "Should allow shared non-recursive table references, but got error: "+err.Error())
		}
	})

	t.Run("truly recursive table references", func(t *testing.T) {
		mod := NewJSONModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local json = require("json")
			
			-- Create a truly recursive structure
			local recursive = {}
			recursive.self = recursive  -- circular reference
			
			-- This should fail because it would create infinite recursion
			local result, err = json.encode(recursive)
			
			if not err then
				return nil, "Expected encoding recursive tables to fail, but it succeeded"
			end
			
			return err
		`

		err = vm.DoString(context.Background(), script, "test")
		require.NoError(t, err)

		// Verify the result contains the expected error about recursion
		result := vm.State().Get(-1).String()
		assert.Contains(t, result, "recursively nested")
	})

	t.Run("complex but valid structures", func(t *testing.T) {
		mod := NewJSONModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local json = require("json")
			
			-- Create several shared components that get reused
			local metadata = {version = "1.0", author = "test"}
			local tags = {"important", "test", "shared"}
			
			-- Create a complex nested structure with shared references
			local data = {
				items = {
					{id = 1, meta = metadata, tags = tags},
					{id = 2, meta = metadata, tags = tags},
					{id = 3, meta = metadata, tags = tags}
				},
				config = {
					settings = {
						global = {meta = metadata},
						local_settings = {tags = tags}
					}
				}
			}
			
			-- This complex but valid structure should encode without errors
			local result, err = json.encode(data)
			
			if err then
				return nil, "Failed to encode complex structure: " .. err
			end
			
			return result
		`

		err = vm.DoString(context.Background(), script, "test")

		// With the current implementation, this will likely fail
		if err == nil {
			result := vm.State().Get(-1).String()
			assert.Contains(t, result, `"version":"1.0"`)
			assert.Contains(t, result, `"important"`)
		} else {
			assert.Fail(t, "Should allow complex non-recursive structures, but got error: "+err.Error())
		}
	})
}
