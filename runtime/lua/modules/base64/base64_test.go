package base64

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func TestBase64ModuleWithVM(t *testing.T) {
	logger := zap.NewNop()

	t.Run("module creation and loading", func(t *testing.T) {
		mod := NewBase64Module()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
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
				vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
				require.NoError(t, err)
				defer vm.Close()

				script := `
					local base64 = require("base64")
					function test(input)
						return base64.encode(input)
					end
					return test
				`
				err = vm.Import(script, "test", "test")
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
				vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
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
				err = vm.Import(script, "test", "test")
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
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
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
		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		input := "Hello, 世界! 🌍"
		result, err := vm.Execute(context.Background(), "test", lua.LString(input))
		require.NoError(t, err)
		assert.Equal(t, input, result.String())
	})

	t.Run("encode with invalid input types", func(t *testing.T) {
		testCases := []struct {
			name        string
			input       lua.LValue
			shouldError bool
		}{
			{
				name:        "nil input",
				input:       lua.LNil,
				shouldError: true,
			},
			{
				name:        "number input",
				input:       lua.LNumber(123),
				shouldError: true,
			},
			{
				name:        "boolean input",
				input:       lua.LBool(true),
				shouldError: true,
			},
			{
				name:        "table input",
				input:       &lua.LTable{},
				shouldError: true,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewBase64Module()
				vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
				require.NoError(t, err)
				defer vm.Close()

				script := `
					local base64 = require("base64")
					function test(input)
						local success, result = pcall(base64.encode, input)
						if not success then
							return nil, result
						end
						return result
					end
					return test
				`
				err = vm.Import(script, "test", "test")
				require.NoError(t, err)

				result, err := vm.Execute(context.Background(), "test", tc.input)
				require.NoError(t, err)

				if tc.shouldError {
					assert.Equal(t, lua.LNil, result)
				}
			})
		}
	})

	t.Run("decode with invalid input types", func(t *testing.T) {
		testCases := []struct {
			name        string
			input       lua.LValue
			shouldError bool
		}{
			{
				name:        "nil input",
				input:       lua.LNil,
				shouldError: true,
			},
			{
				name:        "number input",
				input:       lua.LNumber(123),
				shouldError: true,
			},
			{
				name:        "boolean input",
				input:       lua.LBool(true),
				shouldError: true,
			},
			{
				name:        "table input",
				input:       &lua.LTable{},
				shouldError: true,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewBase64Module()
				vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
				require.NoError(t, err)
				defer vm.Close()

				script := `
					local base64 = require("base64")
					function test(input)
						local success, result = pcall(base64.decode, input)
						if not success then
							return nil, result
						end
						return result
					end
					return test
				`
				err = vm.Import(script, "test", "test")
				require.NoError(t, err)

				result, err := vm.Execute(context.Background(), "test", tc.input)
				require.NoError(t, err)

				if tc.shouldError {
					assert.Equal(t, lua.LNil, result)
				}
			})
		}
	})
}
