package hash

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func calculateHash(data string, hashType string) string {
	var sum []byte
	switch hashType {
	case "md5":
		h := md5.New()
		h.Write([]byte(data))
		sum = h.Sum(nil)
	case "sha1":
		h := sha1.New()
		h.Write([]byte(data))
		sum = h.Sum(nil)
	case "sha256":
		h := sha256.New()
		h.Write([]byte(data))
		sum = h.Sum(nil)
	case "sha512":
		h := sha512.New()
		h.Write([]byte(data))
		sum = h.Sum(nil)
	}
	return hex.EncodeToString(sum)
}

func TestHashModuleWithVM(t *testing.T) {
	logger := zap.NewNop()

	t.Run("module creation and loading", func(t *testing.T) {
		mod := NewHashModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `
			local hash = require("hash")
			assert(type(hash) == "table")
			assert(type(hash.md5) == "function")
			assert(type(hash.sha1) == "function")
			assert(type(hash.sha256) == "function")
			assert(type(hash.sha512) == "function")
			assert(type(hash.fnv32) == "function")
			assert(type(hash.fnv64) == "function")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("input validation", func(t *testing.T) {
		testCases := []struct {
			name     string
			function string
			input    lua.LValue
		}{
			{
				name:     "md5 with nil",
				function: "md5",
				input:    lua.LNil,
			},
			{
				name:     "sha1 with number",
				function: "sha1",
				input:    lua.LNumber(123),
			},
			{
				name:     "sha256 with bool",
				function: "sha256",
				input:    lua.LBool(true),
			},
			{
				name:     "sha512 with table",
				function: "sha512",
				input:    &lua.LTable{},
			},
			{
				name:     "fnv32 with function",
				function: "fnv32",
				input:    &lua.LFunction{},
			},
			{
				name:     "fnv64 with userdata",
				function: "fnv64",
				input:    &lua.LUserData{},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewHashModule()
				vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
				require.NoError(t, err)
				defer vm.Close()

				script := `
					local hash = require("hash")
					function test(input)
						return hash.` + tc.function + `(input)
					end
					return test
				`
				err = vm.Import("test", script)
				require.NoError(t, err)

				_, err = vm.Execute(context.Background(), "test", tc.input)
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "string expected")
			})
		}
	})

	t.Run("comprehensive hash test cases", func(t *testing.T) {
		testCases := []struct {
			name  string
			input string
		}{
			{
				name:  "empty string",
				input: "",
			},
			{
				name:  "simple string",
				input: "hello world",
			},
			{
				name:  "unicode string",
				input: "Hello, 世界!",
			},
			{
				name:  "long string",
				input: string(make([]byte, 10000)),
			},
			{
				name:  "special characters",
				input: "!@#$%^&*()_+-=[]{}|;:,.<>?",
			},
		}

		hashFuncs := []string{"md5", "sha1", "sha256", "sha512"}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewHashModule()
				vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
				require.NoError(t, err)
				defer vm.Close()

				for _, hashFunc := range hashFuncs {
					script := `
						local hash = require("hash")
						function test(input)
							local result, err = hash.` + hashFunc + `(input)
							if err then
								return nil, err
							end
							return result
						end
						return test
					`
					err = vm.Import("test", script)
					require.NoError(t, err)

					result, err := vm.Execute(context.Background(), "test", lua.LString(tc.input))
					require.NoError(t, err)
					require.NotNil(t, result)

					expected := calculateHash(tc.input, hashFunc)
					assert.Equal(t, expected, result.String(), "Hash mismatch for %s with input %q", hashFunc, tc.input)
				}
			})
		}
	})

	t.Run("fnv hash test cases", func(t *testing.T) {
		testCases := []struct {
			name  string
			input string
		}{
			{
				name:  "empty string",
				input: "",
			},
			{
				name:  "simple string",
				input: "hello world",
			},
			{
				name:  "unicode string",
				input: "Hello, 世界!",
			},
			{
				name:  "long string",
				input: string(make([]byte, 10000)),
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewHashModule()
				vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
				require.NoError(t, err)
				defer vm.Close()

				// Test FNV32
				script32 := `
					local hash = require("hash")
					function test(input)
						local result, err = hash.fnv32(input)
						if err then
							return nil, err
						end
						return result
					end
					return test
				`
				err = vm.Import("test32", script32)
				require.NoError(t, err)

				result32, err := vm.Execute(context.Background(), "test32", lua.LString(tc.input))
				require.NoError(t, err)
				assert.NotNil(t, result32)
				assert.Equal(t, lua.LTNumber, result32.Type())

				// Test FNV64
				script64 := `
					local hash = require("hash")
					function test(input)
						local result, err = hash.fnv64(input)
						if err then
							return nil, err
						end
						return result
					end
					return test
				`
				err = vm.Import("test64", script64)
				require.NoError(t, err)

				result64, err := vm.Execute(context.Background(), "test64", lua.LString(tc.input))
				require.NoError(t, err)
				assert.NotNil(t, result64)
				assert.Equal(t, lua.LTNumber, result64.Type())
			})
		}
	})

	t.Run("error handling in Lua", func(t *testing.T) {
		mod := NewHashModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local hash = require("hash")
			function test()
				-- Test error handling for all hash functions
				local functions = {"md5", "sha1", "sha256", "sha512", "fnv32", "fnv64"}
				local results = {}
				
				for _, func_name in ipairs(functions) do
					-- Test with invalid input type
					local success, result = pcall(hash[func_name], 123)
					assert(not success, "Expected error for " .. func_name .. " with number input")
					
					-- Test with nil
					success, result = pcall(hash[func_name], nil)
					assert(not success, "Expected error for " .. func_name .. " with nil input")
					
					-- Test with valid input (should not error)
					success, result = pcall(hash[func_name], "test string")
					assert(success, "Unexpected error for " .. func_name .. " with valid input")
					
					results[func_name] = {
						valid_input = success,
						result = result
					}
				end
				
				return results
			end
			return test
		`
		err = vm.Import("test", script)
		require.NoError(t, err)

		result, err := vm.Execute(context.Background(), "test")
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, lua.LTTable, result.Type())
	})
}
