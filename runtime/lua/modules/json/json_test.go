package json

import (
	"testing"

	lua "github.com/ponyruntime/go-lua"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func assertLua(L *lua.LState) int {
	if L.ToBool(1) {
		return 0
	}
	L.RaiseError(L.OptString(2, "assertion failed!"))
	return 0
}

func TestJsonModule(t *testing.T) {
	logger := zap.NewNop()

	t.Run("module creation and loading", func(t *testing.T) {
		mod := NewJsonModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `
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
				mod := NewJsonModule()
				vm, err := engine.NewVM(logger,
					engine.WithLoader(mod.Name(), mod.Loader),
					engine.WithGlobalFunction("assert", assertLua),
				)
				require.NoError(t, err)
				defer vm.Close()

				err = vm.DoString(nil, tc.script, "test")
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
				mod := NewJsonModule()
				vm, err := engine.NewVM(logger,
					engine.WithLoader(mod.Name(), mod.Loader),
					engine.WithGlobalFunction("assert", assertLua),
				)
				require.NoError(t, err)
				defer vm.Close()

				err = vm.DoString(nil, tc.script, "test")
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
				name: "encode sparse array",
				script: `
					local json = require("json")
					local t = {}
					t[1] = 1
					t[3] = 3
					local value, err = json.encode(t)
					return value, err
				`,
				expectedError: "cannot encode sparse array",
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
				expectedError: "cannot encode mixed or invalid key types",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewJsonModule()
				vm, err := engine.NewVM(logger,
					engine.WithLoader(mod.Name(), mod.Loader),
					engine.WithGlobalFunction("assert", assertLua),
				)
				require.NoError(t, err)
				defer vm.Close()

				err = vm.DoString(nil, tc.script, "test")
				require.NoError(t, err)

				errStr := vm.State().Get(-1).String()
				assert.Contains(t, errStr, tc.expectedError)
				vm.State().Pop(2) // pop both nil and error
			})
		}
	})

	t.Run("round trip", func(t *testing.T) {
		mod := NewJsonModule()
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

		err = vm.DoString(nil, script, "test")
		require.NoError(t, err)
	})
}
