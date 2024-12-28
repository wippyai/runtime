package base64

import (
	"context"
	"testing"

	lua "github.com/ponyruntime/go-lua"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestBase64ModuleWithVM(t *testing.T) {
	logger := zap.NewNop()

	t.Run("module creation and loading", func(t *testing.T) {
		mod := NewBase64Module()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `
			local base64 = require("base64")
			assert(type(base64) == "table")
			assert(type(base64.encode) == "function")
			assert(type(base64.decode) == "function")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("encoding test cases", func(t *testing.T) {
		testCases := []struct {
			name     string
			input    string
			expected string
		}{
			{
				name:     "simple string",
				input:    "hello world",
				expected: "aGVsbG8gd29ybGQ=",
			},
			{
				name:     "empty string",
				input:    "",
				expected: "",
			},
			{
				name:     "special characters",
				input:    "!@#$%^&*()",
				expected: "IUAjJCVeJiooKQ==",
			},
			{
				name:     "unicode characters",
				input:    "こんにちは",
				expected: "44GT44KT44Gr44Gh44Gv",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewBase64Module()
				vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
				require.NoError(t, err)
				defer vm.Close()

				script := `
					local base64 = require("base64")
					function test(input)
						return base64.encode(input)
					end
					return test
				`
				err = vm.CompileFunction("test", script)
				require.NoError(t, err)

				result, err := vm.Execute(context.Background(), "test", lua.LString(tc.input))
				require.NoError(t, err)
				assert.Equal(t, tc.expected, result.String())
			})
		}
	})

	t.Run("decoding test cases", func(t *testing.T) {
		testCases := []struct {
			name        string
			input       string
			expected    string
			shouldError bool
		}{
			{
				name:        "simple string",
				input:       "aGVsbG8gd29ybGQ=",
				expected:    "hello world",
				shouldError: false,
			},
			{
				name:        "empty string",
				input:       "",
				expected:    "",
				shouldError: false,
			},
			{
				name:        "special characters",
				input:       "IUAjJCVeJiooKQ==",
				expected:    "!@#$%^&*()",
				shouldError: false,
			},
			{
				name:        "unicode characters",
				input:       "44GT44KT44Gr44Gh44Gv",
				expected:    "こんにちは",
				shouldError: false,
			},
			{
				name:        "invalid base64",
				input:       "invalid!base64",
				expected:    "",
				shouldError: true,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewBase64Module()
				vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
				require.NoError(t, err)
				defer vm.Close()

				script := `
					local base64 = require("base64")
					function test(input)
						local success, result = pcall(base64.decode, input)
						if not success then
							return nil
						end
						return result
					end
					return test
				`
				err = vm.CompileFunction("test", script)
				require.NoError(t, err)

				result, err := vm.Execute(context.Background(), "test", lua.LString(tc.input))
				require.NoError(t, err)

				if tc.shouldError {
					assert.Equal(t, lua.LNil, result)
				} else {
					assert.Equal(t, tc.expected, result.String())
				}
			})
		}
	})

	t.Run("round trip test", func(t *testing.T) {
		mod := NewBase64Module()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local base64 = require("base64")
			function test(input)
				local encoded = base64.encode(input)
				local success, decoded = pcall(base64.decode, encoded)
				if not success then
					return nil
				end
				return decoded
			end
			return test
		`
		err = vm.CompileFunction("test", script)
		require.NoError(t, err)

		input := "Hello, 世界! 🌍"
		result, err := vm.Execute(context.Background(), "test", lua.LString(input))
		require.NoError(t, err)
		assert.Equal(t, input, result.String())
	})
}
