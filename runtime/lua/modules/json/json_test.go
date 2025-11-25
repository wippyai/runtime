package json

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/engine"
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

// Helper function to decode JSON to Go value for comparison
func decodeJSONToGo(jsonStr string) (interface{}, error) {
	var result interface{}
	err := json.Unmarshal([]byte(jsonStr), &result)
	return result, err
}

// Helper function to compare JSON outputs semantically
func assertJSONEqual(t *testing.T, expected, actual string) {
	expectedValue, err := decodeJSONToGo(expected)
	require.NoError(t, err, "Expected JSON should be valid")

	actualValue, err := decodeJSONToGo(actual)
	require.NoError(t, err, "Actual JSON should be valid")

	assert.Equal(t, expectedValue, actualValue, "JSON values should be semantically equal")
}

func TestJsonModule(t *testing.T) {
	logger := zap.NewNop()

	t.Run("module creation and loading", func(t *testing.T) {
		mod := NewJSONModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Info().Name, mod.Loader),
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
					engine.WithLoader(mod.Info().Name, mod.Loader),
					engine.WithGlobalFunction("assert", assertLua),
				)
				require.NoError(t, err)
				defer vm.Close()

				err = vm.DoString(context.Background(), tc.script, "test")
				require.NoError(t, err)

				result := vm.State().Get(-1)
				actualJSON := result.String()

				// Use semantic comparison instead of exact string match
				assertJSONEqual(t, tc.expected, actualJSON)
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
					engine.WithLoader(mod.Info().Name, mod.Loader),
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
					return value, err and tostring(err) or nil
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
					return value, err and tostring(err) or nil
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
					return value, err and tostring(err) or nil
				`,
				expectedError: "table has both numeric and non-numeric keys",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewJSONModule()
				vm, err := engine.NewVM(logger,
					engine.WithLoader(mod.Info().Name, mod.Loader),
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
			engine.WithLoader(mod.Info().Name, mod.Loader),
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
			engine.WithLoader(mod.Info().Name, mod.Loader),
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

			// Use semantic validation instead of exact string matching
			decoded, err := decodeJSONToGo(result)
			require.NoError(t, err)

			resultMap := decoded.(map[string]interface{})
			first := resultMap["first"].(map[string]interface{})
			second := resultMap["second"].(map[string]interface{})

			assert.Equal(t, "shared_table", first["name"])
			assert.Equal(t, float64(42), first["value"])
			assert.Equal(t, "shared_table", second["name"])
			assert.Equal(t, float64(42), second["value"])
		} else {
			// This assertion will fail with the current implementation
			assert.Fail(t, "Should allow shared non-recursive table references, but got error: "+err.Error())
		}
	})

	t.Run("truly recursive table references", func(t *testing.T) {
		mod := NewJSONModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Info().Name, mod.Loader),
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

			return tostring(err)
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
			engine.WithLoader(mod.Info().Name, mod.Loader),
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

			// Use semantic validation
			decoded, err := decodeJSONToGo(result)
			require.NoError(t, err)

			resultMap := decoded.(map[string]interface{})
			items := resultMap["items"].([]interface{})
			firstItem := items[0].(map[string]interface{})
			meta := firstItem["meta"].(map[string]interface{})

			assert.Equal(t, "1.0", meta["version"])
			assert.Contains(t, result, "important")
		} else {
			assert.Fail(t, "Should allow complex non-recursive structures, but got error: "+err.Error())
		}
	})
}

// Additional test to verify our optimization is working correctly
func TestJSONOptimization(t *testing.T) {
	logger := zap.NewNop()

	t.Run("large table encoding performance", func(t *testing.T) {
		mod := NewJSONModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Info().Name, mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local json = require("json")
			
			-- Create a moderately large table to test our optimization
			local largeTable = {}
			for i = 1, 100 do
				largeTable["key" .. i] = {
					id = i,
					data = {"item1", "item2", "item3"},
					nested = {
						value = i * 2,
						flag = i % 2 == 0
					}
				}
			end
			
			local result = json.encode(largeTable)
			return result
		`

		err = vm.DoString(context.Background(), script, "test")
		require.NoError(t, err)

		result := vm.State().Get(-1).String()

		// Verify the result is valid JSON and contains expected data
		decoded, err := decodeJSONToGo(result)
		require.NoError(t, err)

		resultMap := decoded.(map[string]interface{})
		assert.Equal(t, 100, len(resultMap))

		// Check a few entries
		key1 := resultMap["key1"].(map[string]interface{})
		assert.Equal(t, float64(1), key1["id"])

		nested := key1["nested"].(map[string]interface{})
		assert.Equal(t, float64(2), nested["value"])
		assert.Equal(t, false, nested["flag"])
	})
}

func TestJsonModuleOptions(t *testing.T) {
	t.Run("default options", func(t *testing.T) {
		mod := NewJSONModule()
		assert.False(t, mod.EnableCache)
		assert.Equal(t, 100, mod.CacheSize)
	})

	t.Run("with cache enabled", func(t *testing.T) {
		mod := NewJSONModule(WithCache(true))
		assert.True(t, mod.EnableCache)
		assert.Equal(t, 100, mod.CacheSize)
	})

	t.Run("with custom capacity", func(t *testing.T) {
		mod := NewJSONModule(WithCapacity(500))
		assert.False(t, mod.EnableCache)
		assert.Equal(t, 500, mod.CacheSize)
	})

	t.Run("with cache enabled and custom capacity", func(t *testing.T) {
		mod := NewJSONModule(WithCache(true), WithCapacity(1000))
		assert.True(t, mod.EnableCache)
		assert.Equal(t, 1000, mod.CacheSize)
	})
}

func TestJsonValidation(t *testing.T) {
	logger := zap.NewNop()

	t.Run("validate with schema string and valid data", func(t *testing.T) {
		mod := NewJSONModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Info().Name, mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local json = require("json")
			local schema = '{"type": "object", "properties": {"name": {"type": "string"}, "age": {"type": "number"}}, "required": ["name"]}'
			local data = {name = "John", age = 30}
			local ok, err = json.validate(schema, data)
			assert(ok == true, "validation should succeed")
			assert(err == nil, "should have no error")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("validate with schema string and invalid data", func(t *testing.T) {
		mod := NewJSONModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Info().Name, mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local json = require("json")
			local schema = '{"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]}'
			local data = {age = 30}
			local ok, err = json.validate(schema, data)
			assert(ok == false, "validation should fail")
			assert(err ~= nil, "should have error")
			assert(err.type == "validation_error", "error type should be validation_error")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("validate with schema table and valid data", func(t *testing.T) {
		mod := NewJSONModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Info().Name, mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local json = require("json")
			local schema = {
				type = "object",
				properties = {
					name = {type = "string"},
					age = {type = "number"}
				},
				required = {"name"}
			}
			local data = {name = "Alice", age = 25}
			local ok, err = json.validate(schema, data)
			assert(ok == true, "validation should succeed")
			assert(err == nil, "should have no error")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("validate with missing schema", func(t *testing.T) {
		mod := NewJSONModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Info().Name, mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local json = require("json")
			local data = {name = "John"}
			local ok, err = json.validate(nil, data)
			assert(ok == false, "validation should fail")
			assert(err ~= nil, "should have error")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("validate with missing data", func(t *testing.T) {
		mod := NewJSONModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Info().Name, mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local json = require("json")
			local schema = '{"type": "string"}'
			local ok, err = json.validate(schema, nil)
			assert(ok == false, "validation should fail")
			assert(err ~= nil, "should have error")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("validate_string with valid JSON", func(t *testing.T) {
		mod := NewJSONModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Info().Name, mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local json = require("json")
			local schema = '{"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]}'
			local jsonStr = '{"name": "Bob", "age": 35}'
			local ok, err = json.validate_string(schema, jsonStr)
			assert(ok == true, "validation should succeed")
			assert(err == nil, "should have no error")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("validate_string with invalid JSON", func(t *testing.T) {
		mod := NewJSONModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Info().Name, mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local json = require("json")
			local schema = '{"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]}'
			local jsonStr = '{"age": 35}'
			local ok, err = json.validate_string(schema, jsonStr)
			assert(ok == false, "validation should fail")
			assert(err ~= nil, "should have error")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("validate_string with non-string data", func(t *testing.T) {
		mod := NewJSONModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Info().Name, mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local json = require("json")
			local schema = '{"type": "string"}'
			local ok, err = json.validate_string(schema, {name = "test"})
			assert(ok == false, "validation should fail")
			assert(err ~= nil, "should have error")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("validate with caching enabled", func(t *testing.T) {
		mod := NewJSONModule()
		mod.EnableCache = true
		mod.CacheSize = 10

		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Info().Name, mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local json = require("json")
			local schema = '{"type": "string"}'

			local ok1, err1 = json.validate(schema, "hello")
			assert(ok1 == true, "first validation should succeed")

			local ok2, err2 = json.validate(schema, "world")
			assert(ok2 == true, "second validation should succeed with cached schema")

			local ok3, err3 = json.validate(schema, 123)
			assert(ok3 == false, "third validation should fail")
		`, "test")
		assert.NoError(t, err)
	})
}
