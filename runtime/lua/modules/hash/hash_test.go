package hash

import (
	"context"
	"crypto/hmac"
	"crypto/md5"  //nolint:gosec
	"crypto/sha1" //nolint:gosec
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"hash"
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
		h := md5.New() //nolint:gosec
		h.Write([]byte(data))
		sum = h.Sum(nil)
	case "sha1":
		h := sha1.New() //nolint:gosec
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

func calculateRawHash(data string, hashType string) []byte {
	var sum []byte
	switch hashType {
	case "md5":
		h := md5.New() //nolint:gosec
		h.Write([]byte(data))
		sum = h.Sum(nil)
	case "sha1":
		h := sha1.New() //nolint:gosec
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
	return sum
}

func calculateHMAC(data, secret string, hashType string) string {
	var h hash.Hash
	switch hashType {
	case "md5":
		h = hmac.New(md5.New, []byte(secret))
	case "sha1":
		h = hmac.New(sha1.New, []byte(secret))
	case "sha256":
		h = hmac.New(sha256.New, []byte(secret))
	case "sha512":
		h = hmac.New(sha512.New, []byte(secret))
	}
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

func calculateRawHMAC(data, secret string, hashType string) []byte {
	var h hash.Hash
	switch hashType {
	case "md5":
		h = hmac.New(md5.New, []byte(secret))
	case "sha1":
		h = hmac.New(sha1.New, []byte(secret))
	case "sha256":
		h = hmac.New(sha256.New, []byte(secret))
	case "sha512":
		h = hmac.New(sha512.New, []byte(secret))
	}
	h.Write([]byte(data))
	return h.Sum(nil)
}

func TestHashModuleWithVM(t *testing.T) {
	logger := zap.NewNop()

	t.Run("module creation and loading", func(t *testing.T) {
		mod := NewHashModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
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
				err = vm.Import(script, "test", "test")
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
				input: string(make([]byte, 1000)),
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
					// Test hex output (default)
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
					err = vm.Import(script, "test_hex", "test_hex")
					require.NoError(t, err)

					result, err := vm.Execute(context.Background(), "test_hex", lua.LString(tc.input))
					require.NoError(t, err)
					require.NotNil(t, result)

					expected := calculateHash(tc.input, hashFunc)
					assert.Equal(t, expected, result.String(), "Hex hash mismatch for %s with input %q", hashFunc, tc.input)

					// Test binary output
					scriptBin := `
						local hash = require("hash")
						function test(input)
							local result, err = hash.` + hashFunc + `(input, true)
							if err then
								return nil, err
							end
							return result
						end
						return test
					`
					err = vm.Import(scriptBin, "test_bin", "test_bin")
					require.NoError(t, err)

					resultBin, err := vm.Execute(context.Background(), "test_bin", lua.LString(tc.input))
					require.NoError(t, err)
					require.NotNil(t, resultBin)

					// For binary output, we need to compare byte by byte
					expectedBin := calculateRawHash(tc.input, hashFunc)
					resultBytes := []byte(resultBin.String())
					assert.Equal(t, expectedBin, resultBytes, "Binary hash mismatch for %s with input %q", hashFunc, tc.input)
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
				input: string(make([]byte, 1000)),
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
				err = vm.Import(script32, "test_file", "test32")
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
				err = vm.Import(script64, "test_demo", "test64")
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
		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		result, err := vm.Execute(context.Background(), "test")
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, lua.LTTable, result.Type())
	})

	t.Run("binary option test", func(t *testing.T) {
		mod := NewHashModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local hash = require("hash")
			function test()
				local input = "test string"
				local results = {}
				
				-- Test binary option for each hash function
				results.md5_hex = hash.md5(input)
				results.md5_bin = hash.md5(input, true)
				results.sha1_hex = hash.sha1(input)
				results.sha1_bin = hash.sha1(input, true)
				results.sha256_hex = hash.sha256(input)
				results.sha256_bin = hash.sha256(input, true)
				results.sha512_hex = hash.sha512(input)
				results.sha512_bin = hash.sha512(input, true)
				
				-- Verify bin and hex are different
				assert(results.md5_hex ~= results.md5_bin)
				assert(results.sha1_hex ~= results.sha1_bin)
				assert(results.sha256_hex ~= results.sha256_bin)
				assert(results.sha512_hex ~= results.sha512_bin)
				
				-- Verify bin length is correct
				assert(#results.md5_bin == 16) -- MD5 is 16 bytes
				assert(#results.sha1_bin == 20) -- SHA1 is 20 bytes
				assert(#results.sha256_bin == 32) -- SHA256 is 32 bytes
				assert(#results.sha512_bin == 64) -- SHA512 is 64 bytes
				
				return results
			end
			return test
		`
		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		result, err := vm.Execute(context.Background(), "test")
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, lua.LTTable, result.Type())
	})
}

func TestHashModuleWithVM_HMAC(t *testing.T) {
	logger := zap.NewNop()

	t.Run("module hmac functions loading", func(t *testing.T) {
		mod := NewHashModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local hash = require("hash")
			assert(type(hash) == "table")
			assert(type(hash.hmac_sha256) == "function")
			assert(type(hash.hmac_sha512) == "function")
			assert(type(hash.hmac_sha1) == "function")
			assert(type(hash.hmac_md5) == "function")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("hmac input validation", func(t *testing.T) {
		testCases := []struct {
			name     string
			function string
			dataArg  lua.LValue
			keyArg   lua.LValue
		}{
			{
				name:     "hmac_sha256 with nil data",
				function: "hmac_sha256",
				dataArg:  lua.LNil,
				keyArg:   lua.LString("secret"),
			},
			{
				name:     "hmac_sha256 with nil key",
				function: "hmac_sha256",
				dataArg:  lua.LString("data"),
				keyArg:   lua.LNil,
			},
			{
				name:     "hmac_sha512 with number data",
				function: "hmac_sha512",
				dataArg:  lua.LNumber(123),
				keyArg:   lua.LString("secret"),
			},
			{
				name:     "hmac_sha1 with bool key",
				function: "hmac_sha1",
				dataArg:  lua.LString("data"),
				keyArg:   lua.LBool(true),
			},
			{
				name:     "hmac_md5 with table data",
				function: "hmac_md5",
				dataArg:  &lua.LTable{},
				keyArg:   lua.LString("secret"),
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
					function test(data, key)
						return hash.` + tc.function + `(data, key)
					end
					return test
				`
				err = vm.Import(script, "test", "test")
				require.NoError(t, err)

				_, err = vm.Execute(context.Background(), "test", tc.dataArg, tc.keyArg)
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "string expected")
			})
		}
	})

	t.Run("comprehensive hmac test cases", func(t *testing.T) {
		testCases := []struct {
			name   string
			data   string
			secret string
		}{
			{
				name:   "empty string",
				data:   "",
				secret: "secret",
			},
			{
				name:   "simple string",
				data:   "hello world",
				secret: "secret-key",
			},
			{
				name:   "unicode string",
				data:   "Hello, 世界!",
				secret: "unicode-key",
			},
			{
				name:   "long string",
				data:   string(make([]byte, 1000)),
				secret: "key",
			},
			{
				name:   "special characters",
				data:   "!@#$%^&*()_+-=[]{}|;:,.<>?",
				secret: "special-key!@#",
			},
		}

		hmacFuncs := map[string]string{
			"hmac_md5":    "md5",
			"hmac_sha1":   "sha1",
			"hmac_sha256": "sha256",
			"hmac_sha512": "sha512",
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewHashModule()
				vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
				require.NoError(t, err)
				defer vm.Close()

				for luaFunc, goFunc := range hmacFuncs {
					// Test hex output (default)
					script := `
						local hash = require("hash")
						function test(data, secret)
							local result, err = hash.` + luaFunc + `(data, secret)
							if err then
								return nil, err
							end
							return result
						end
						return test
					`
					err = vm.Import(script, "test_hex", "test_hex")
					require.NoError(t, err)

					result, err := vm.Execute(context.Background(), "test_hex", lua.LString(tc.data), lua.LString(tc.secret))
					require.NoError(t, err)
					require.NotNil(t, result)

					expected := calculateHMAC(tc.data, tc.secret, goFunc)
					assert.Equal(t, expected, result.String(), "Hex HMAC mismatch for %s with data %q and secret %q", luaFunc, tc.data, tc.secret)

					// Test binary output
					scriptBin := `
						local hash = require("hash")
						function test(data, secret)
							local result, err = hash.` + luaFunc + `(data, secret, true)
							if err then
								return nil, err
							end
							return result
						end
						return test
					`
					err = vm.Import(scriptBin, "test_bin", "test_bin")
					require.NoError(t, err)

					resultBin, err := vm.Execute(context.Background(), "test_bin", lua.LString(tc.data), lua.LString(tc.secret))
					require.NoError(t, err)
					require.NotNil(t, resultBin)

					expectedBin := calculateRawHMAC(tc.data, tc.secret, goFunc)
					resultBytes := []byte(resultBin.String())
					assert.Equal(t, expectedBin, resultBytes, "Binary HMAC mismatch for %s with data %q and secret %q", luaFunc, tc.data, tc.secret)
				}
			})
		}
	})

	t.Run("hmac binary option test", func(t *testing.T) {
		mod := NewHashModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local hash = require("hash")
			function test()
				local input = "test string"
				local secret = "test secret"
				local results = {}
				
				-- Test binary option for each hmac function
				results.hmac_md5_hex = hash.hmacMd5(input, secret)
				results.hmac_md5_bin = hash.hmac_md5(input, secret, true)
				results.hmac_sha1_hex = hash.hmac_sha1(input, secret)
				results.hmac_sha1_bin = hash.hmac_sha1(input, secret, true)
				results.hmac_sha256_hex = hash.hmac_sha256(input, secret)
				results.hmac_sha256_bin = hash.hmac_sha256(input, secret, true)
				results.hmac_sha512_hex = hash.hmac_sha512(input, secret)
				results.hmac_sha512_bin = hash.hmac_sha512(input, secret, true)
				
				-- Verify bin and hex are different
				assert(results.hmac_md5_hex ~= results.hmac_md5_bin)
				assert(results.hmac_sha1_hex ~= results.hmac_sha1_bin)
				assert(results.hmac_sha256_hex ~= results.hmac_sha256_bin)
				assert(results.hmac_sha512_hex ~= results.hmac_sha512_bin)
				
				-- Verify bin length is correct
				assert(#results.hmac_md5_bin == 16) -- HMAC-MD5 is 16 bytes
				assert(#results.hmac_sha1_bin == 20) -- HMAC-SHA1 is 20 bytes
				assert(#results.hmac_sha256_bin == 32) -- HMAC-SHA256 is 32 bytes
				assert(#results.hmac_sha512_bin == 64) -- HMAC-SHA512 is 64 bytes
				
				return results
			end
			return test
		`
		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		result, err := vm.Execute(context.Background(), "test")
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, lua.LTTable, result.Type())
	})
}
