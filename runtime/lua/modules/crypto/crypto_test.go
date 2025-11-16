package crypto

import (
	"context"
	"crypto/rand"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"golang.org/x/crypto/chacha20poly1305"
)

func newTestContext() context.Context {
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	return ctx
}

func TestEncryptDecryptModuleWithVM(t *testing.T) {
	logger := zap.NewNop()

	// Test loading the module and registering the encrypt/decrypt submodules
	t.Run("module loading", func(t *testing.T) {
		mod := NewCryptoModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local crypto = require("crypto")
			-- Test encrypt submodule
			assert(type(crypto) == "table")
			assert(type(crypto.encrypt) == "table")
			assert(type(crypto.encrypt.aes) == "function")
			assert(type(crypto.encrypt.chacha20) == "function")
			
			-- Test decrypt submodule
			assert(type(crypto.decrypt) == "table")
			assert(type(crypto.decrypt.aes) == "function")
			assert(type(crypto.decrypt.chacha20) == "function")
		`, "test")
		assert.NoError(t, err)
	})

	// Test AES key validation
	t.Run("AES key validation", func(t *testing.T) {
		mod := NewCryptoModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		testCases := []struct {
			name    string
			keySize int
			isValid bool
		}{
			{
				name:    "invalid key size - 1 byte",
				keySize: 1,
				isValid: false,
			},
			{
				name:    "invalid key size - 15 bytes",
				keySize: 15,
				isValid: false,
			},
			{
				name:    "valid key size - 16 bytes (AES-128)",
				keySize: 16,
				isValid: true,
			},
			{
				name:    "invalid key size - 20 bytes",
				keySize: 20,
				isValid: false,
			},
			{
				name:    "valid key size - 24 bytes (AES-192)",
				keySize: 24,
				isValid: true,
			},
			{
				name:    "invalid key size - 30 bytes",
				keySize: 30,
				isValid: false,
			},
			{
				name:    "valid key size - 32 bytes (AES-256)",
				keySize: 32,
				isValid: true,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				// Create a key of the specified size
				key := make([]byte, tc.keySize)
				for i := range key {
					key[i] = byte(i % 256)
				}

				script := `
					local crypto = require("crypto")
					function test(key, data)
						local result = {}
						local encrypted, err = crypto.encrypt.aes(data, key)
						if err then
							result.success = false
							result.error = err
							return result
						end
						result.success = true
						return result
					end
					return test
				`
				err = vm.Import(script, "test", "test")
				require.NoError(t, err)

				result, err := vm.Execute(newTestContext(), "test", lua.LString(string(key)), lua.LString("test data"))
				require.NoError(t, err)

				resultTable, ok := result.(*lua.LTable)
				require.True(t, ok, "Expected result to be a table")

				success := resultTable.RawGetString("success")
				if tc.isValid {
					assert.Equal(t, lua.LTrue, success, "Expected valid key size to succeed")
				} else {
					assert.Equal(t, lua.LFalse, success, "Expected invalid key size to fail")
					errMsg := resultTable.RawGetString("error").String()
					assert.Contains(t, errMsg, "key must be", "Expected key size error message")
				}
			})
		}
	})

	// Test ChaCha20 key validation
	t.Run("ChaCha20 key validation", func(t *testing.T) {
		mod := NewCryptoModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		testCases := []struct {
			name    string
			keySize int
			isValid bool
		}{
			{
				name:    "invalid key size - 16 bytes",
				keySize: 16,
				isValid: false,
			},
			{
				name:    "invalid key size - 31 bytes",
				keySize: 31,
				isValid: false,
			},
			{
				name:    "valid key size - 32 bytes",
				keySize: chacha20poly1305.KeySize,
				isValid: true,
			},
			{
				name:    "invalid key size - 33 bytes",
				keySize: 33,
				isValid: false,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				// Create a key of the specified size
				key := make([]byte, tc.keySize)
				for i := range key {
					key[i] = byte(i % 256)
				}

				script := `
					local crypto = require("crypto")
					function test(key, data)
						local result = {}
						local encrypted, err = crypto.encrypt.chacha20(data, key)
						if err then
							result.success = false
							result.error = err
							return result
						end
						result.success = true
						return result
					end
					return test
				`
				err = vm.Import(script, "test", "test")
				require.NoError(t, err)

				result, err := vm.Execute(newTestContext(), "test", lua.LString(string(key)), lua.LString("test data"))
				require.NoError(t, err)

				resultTable, ok := result.(*lua.LTable)
				require.True(t, ok, "Expected result to be a table")

				success := resultTable.RawGetString("success")
				if tc.isValid {
					assert.Equal(t, lua.LTrue, success, "Expected valid key size to succeed")
				} else {
					assert.Equal(t, lua.LFalse, success, "Expected invalid key size to fail")
					errMsg := resultTable.RawGetString("error").String()
					assert.Contains(t, errMsg, "key must be", "Expected key size error message")
				}
			})
		}
	})

	// Test AES round-trip encryption/decryption
	t.Run("AES round-trip", func(t *testing.T) {
		mod := NewCryptoModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		testCases := []struct {
			name    string
			keySize int
			data    string
			aad     string // Additional authenticated data
		}{
			{
				name:    "AES-128 simple data",
				keySize: 16,
				data:    "hello world",
				aad:     "",
			},
			{
				name:    "AES-192 with AAD",
				keySize: 24,
				data:    "secret message",
				aad:     "additional data",
			},
			{
				name:    "AES-256 with unicode",
				keySize: 32,
				data:    "こんにちは世界",
				aad:     "認証データ",
			},
			{
				name:    "AES-256 empty data",
				keySize: 32,
				data:    "",
				aad:     "",
			},
			{
				name:    "AES-256 binary data",
				keySize: 32,
				data:    string([]byte{0, 1, 2, 3, 4, 5, 0xFF, 0xFE}),
				aad:     "",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				// Create a key of the specified size
				key := make([]byte, tc.keySize)
				_, err := rand.Read(key)
				require.NoError(t, err)

				var script string
				if tc.aad == "" {
					script = `
						local crypto = require("crypto")
						function test(key, data)
							local encrypted, err = crypto.encrypt.aes(data, key)
							assert(err == nil, "Encryption error: " .. tostring(err))
							
							local decrypted, err = crypto.decrypt.aes(encrypted, key)
							assert(err == nil, "Decryption error: " .. tostring(err))
							
							local result = {}
							result.encrypted = encrypted
							result.decrypted = decrypted
							return result
						end
						return test
					`
				} else {
					script = `
						local crypto = require("crypto")
						function test(key, data, aad)
							local encrypted, err = crypto.encrypt.aes(data, key, aad)
							assert(err == nil, "Encryption error: " .. tostring(err))
							
							local decrypted, err = crypto.decrypt.aes(encrypted, key, aad)
							assert(err == nil, "Decryption error: " .. tostring(err))
							
							local result = {}
							result.encrypted = encrypted
							result.decrypted = decrypted
							return result
						end
						return test
					`
				}

				err = vm.Import(script, "test", "test")
				require.NoError(t, err)

				var result lua.LValue
				if tc.aad == "" {
					result, err = vm.Execute(newTestContext(), "test", lua.LString(string(key)), lua.LString(tc.data))
				} else {
					result, err = vm.Execute(newTestContext(), "test", lua.LString(string(key)), lua.LString(tc.data), lua.LString(tc.aad))
				}
				require.NoError(t, err)

				resultTable, ok := result.(*lua.LTable)
				require.True(t, ok, "Expected result to be a table")

				encrypted := resultTable.RawGetString("encrypted").String()
				decrypted := resultTable.RawGetString("decrypted").String()

				assert.NotEqual(t, tc.data, encrypted, "Encrypted data should be different from original")
				assert.Equal(t, tc.data, decrypted, "Decrypted data should match original")
			})
		}
	})

	// Test ChaCha20 round-trip encryption/decryption
	t.Run("ChaCha20 round-trip", func(t *testing.T) {
		mod := NewCryptoModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		testCases := []struct {
			name string
			data string
			aad  string // Additional authenticated data
		}{
			{
				name: "simple data",
				data: "hello world",
				aad:  "",
			},
			{
				name: "with AAD",
				data: "secret message",
				aad:  "additional data",
			},
			{
				name: "with unicode",
				data: "こんにちは世界",
				aad:  "認証データ",
			},
			{
				name: "empty data",
				data: "",
				aad:  "",
			},
			{
				name: "binary data",
				data: string([]byte{0, 1, 2, 3, 4, 5, 0xFF, 0xFE}),
				aad:  "",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				// Create a key of the required size for ChaCha20
				key := make([]byte, chacha20poly1305.KeySize)
				_, err := rand.Read(key)
				require.NoError(t, err)

				var script string
				if tc.aad == "" {
					script = `
						local crypto = require("crypto")
						function test(key, data)
							local encrypted, err = crypto.encrypt.chacha20(data, key)
							assert(err == nil, "Encryption error: " .. tostring(err))
							
							local decrypted, err = crypto.decrypt.chacha20(encrypted, key)
							assert(err == nil, "Decryption error: " .. tostring(err))
							
							local result = {}
							result.encrypted = encrypted
							result.decrypted = decrypted
							return result
						end
						return test
					`
				} else {
					script = `
						local crypto = require("crypto")
						function test(key, data, aad)
							local encrypted, err = crypto.encrypt.chacha20(data, key, aad)
							assert(err == nil, "Encryption error: " .. tostring(err))
							
							local decrypted, err = crypto.decrypt.chacha20(encrypted, key, aad)
							assert(err == nil, "Decryption error: " .. tostring(err))
							
							local result = {}
							result.encrypted = encrypted
							result.decrypted = decrypted
							return result
						end
						return test
					`
				}

				err = vm.Import(script, "test", "test")
				require.NoError(t, err)

				var result lua.LValue
				if tc.aad == "" {
					result, err = vm.Execute(newTestContext(), "test", lua.LString(string(key)), lua.LString(tc.data))
				} else {
					result, err = vm.Execute(newTestContext(), "test", lua.LString(string(key)), lua.LString(tc.data), lua.LString(tc.aad))
				}
				require.NoError(t, err)

				resultTable, ok := result.(*lua.LTable)
				require.True(t, ok, "Expected result to be a table")

				encrypted := resultTable.RawGetString("encrypted").String()
				decrypted := resultTable.RawGetString("decrypted").String()

				assert.NotEqual(t, tc.data, encrypted, "Encrypted data should be different from original")
				assert.Equal(t, tc.data, decrypted, "Decrypted data should match original")
			})
		}
	})

	// Test decryption with invalid data
	t.Run("decrypt with invalid data", func(t *testing.T) {
		mod := NewCryptoModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		testCases := []struct {
			name        string
			algorithm   string
			keySize     int
			data        string
			expectedErr string
		}{
			{
				name:        "AES - data too short",
				algorithm:   "aes",
				keySize:     16,
				data:        "too short",
				expectedErr: "encrypted data too short",
			},
			{
				name:        "ChaCha20 - data too short",
				algorithm:   "chacha20",
				keySize:     chacha20poly1305.KeySize,
				data:        "too short",
				expectedErr: "encrypted data too short",
			},
			{
				name:        "AES - corrupted data",
				algorithm:   "aes",
				keySize:     16,
				data:        string(make([]byte, 32)), // Some garbage data with enough length
				expectedErr: "failed to decrypt",
			},
			{
				name:        "ChaCha20 - corrupted data",
				algorithm:   "chacha20",
				keySize:     chacha20poly1305.KeySize,
				data:        string(make([]byte, 32)), // Some garbage data with enough length
				expectedErr: "failed to decrypt",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				// Create a key of the specified size
				key := make([]byte, tc.keySize)
				_, err := rand.Read(key)
				require.NoError(t, err)

				script := `
					local crypto = require("crypto")
					function test(key, data)
						local result = {}
						local decrypted, err = crypto.decrypt.` + tc.algorithm + `(data, key)
						if err then
							result.success = false
							result.error = err
							return result
						end
						result.success = true
						result.decrypted = decrypted
						return result
					end
					return test
				`
				err = vm.Import(script, "test", "test")
				require.NoError(t, err)

				result, err := vm.Execute(newTestContext(), "test", lua.LString(string(key)), lua.LString(tc.data))
				require.NoError(t, err)

				resultTable, ok := result.(*lua.LTable)
				require.True(t, ok, "Expected result to be a table")

				success := resultTable.RawGetString("success")
				assert.Equal(t, lua.LFalse, success, "Expected decryption to fail")

				errMsg := resultTable.RawGetString("error").String()
				assert.Contains(t, errMsg, tc.expectedErr, "Expected specific error message")
			})
		}
	})

	// Test AAD validation
	t.Run("AAD validation", func(t *testing.T) {
		mod := NewCryptoModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Create valid keys for AES and ChaCha20
		aesKey := make([]byte, 16)
		_, err = rand.Read(aesKey)
		require.NoError(t, err)

		chachaKey := make([]byte, chacha20poly1305.KeySize)
		_, err = rand.Read(chachaKey)
		require.NoError(t, err)

		// Create test data
		data := "test data"

		script := `
			local crypto = require("crypto")
			function test(alg, key, data, encrypt_aad, decrypt_aad)
				local encrypt_func = crypto.encrypt[alg]
				local decrypt_func = crypto.decrypt[alg]
				
				-- Encrypt with one AAD
				local encrypted, err = encrypt_func(data, key, encrypt_aad)
				assert(err == nil, "Encryption error: " .. tostring(err))
				
				-- Try to decrypt with different AAD
				local result = {}
				local decrypted, err = decrypt_func(encrypted, key, decrypt_aad)
				
				if err then
					result.success = false
					result.error = err
					return result
				end
				
				result.success = true
				result.decrypted = decrypted
				return result
			end
			return test
		`
		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		// Test AES with mismatched AAD
		t.Run("AES with mismatched AAD", func(t *testing.T) {
			result, err := vm.Execute(newTestContext(), "test",
				lua.LString("aes"),
				lua.LString(string(aesKey)),
				lua.LString(data),
				lua.LString("correct aad"),
				lua.LString("wrong aad"))
			require.NoError(t, err)

			resultTable, ok := result.(*lua.LTable)
			require.True(t, ok, "Expected result to be a table")

			success := resultTable.RawGetString("success")
			assert.Equal(t, lua.LFalse, success, "Expected decryption to fail with mismatched AAD")

			errMsg := resultTable.RawGetString("error").String()
			assert.Contains(t, errMsg, "failed to decrypt", "Expected decryption error")
		})

		// Test ChaCha20 with mismatched AAD
		t.Run("ChaCha20 with mismatched AAD", func(t *testing.T) {
			result, err := vm.Execute(newTestContext(), "test",
				lua.LString("chacha20"),
				lua.LString(string(chachaKey)),
				lua.LString(data),
				lua.LString("correct aad"),
				lua.LString("wrong aad"))
			require.NoError(t, err)

			resultTable, ok := result.(*lua.LTable)
			require.True(t, ok, "Expected result to be a table")

			success := resultTable.RawGetString("success")
			assert.Equal(t, lua.LFalse, success, "Expected decryption to fail with mismatched AAD")

			errMsg := resultTable.RawGetString("error").String()
			assert.Contains(t, errMsg, "failed to decrypt", "Expected decryption error")
		})

		// Test AES with matching AAD
		t.Run("AES with matching AAD", func(t *testing.T) {
			result, err := vm.Execute(newTestContext(), "test",
				lua.LString("aes"),
				lua.LString(string(aesKey)),
				lua.LString(data),
				lua.LString("same aad"),
				lua.LString("same aad"))
			require.NoError(t, err)

			resultTable, ok := result.(*lua.LTable)
			require.True(t, ok, "Expected result to be a table")

			success := resultTable.RawGetString("success")
			assert.Equal(t, lua.LTrue, success, "Expected decryption to succeed with matching AAD")

			decrypted := resultTable.RawGetString("decrypted").String()
			assert.Equal(t, data, decrypted, "Decrypted data should match original")
		})

		// Test ChaCha20 with matching AAD
		t.Run("ChaCha20 with matching AAD", func(t *testing.T) {
			result, err := vm.Execute(newTestContext(), "test",
				lua.LString("chacha20"),
				lua.LString(string(chachaKey)),
				lua.LString(data),
				lua.LString("same aad"),
				lua.LString("same aad"))
			require.NoError(t, err)

			resultTable, ok := result.(*lua.LTable)
			require.True(t, ok, "Expected result to be a table")

			success := resultTable.RawGetString("success")
			assert.Equal(t, lua.LTrue, success, "Expected decryption to succeed with matching AAD")

			decrypted := resultTable.RawGetString("decrypted").String()
			assert.Equal(t, data, decrypted, "Decrypted data should match original")
		})
	})

	// Test error handling in Lua
	t.Run("error handling in Lua", func(t *testing.T) {
		mod := NewCryptoModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local crypto = require("crypto")
			function test()
				local results = {}
				
				-- AES parameter validation
				local success, result = pcall(function()
					return crypto.encrypt.aes(nil, "key")
				end)
				assert(not success, "Expected error for nil data")
				
				success, result = pcall(function()
					return crypto.encrypt.aes("data", nil)
				end)
				assert(not success, "Expected error for nil key")
				
				success, result = pcall(function()
					return crypto.decrypt.aes(nil, "key")
				end)
				assert(not success, "Expected error for nil encrypted data")
				
				-- ChaCha20 parameter validation
				success, result = pcall(function()
					return crypto.encrypt.chacha20(nil, "key")
				end)
				assert(not success, "Expected error for nil data")
				
				success, result = pcall(function()
					return crypto.encrypt.chacha20("data", nil)
				end)
				assert(not success, "Expected error for nil key")
				
				success, result = pcall(function()
					return crypto.decrypt.chacha20(nil, "key")
				end)
				assert(not success, "Expected error for nil encrypted data")
				
				-- Test successful operation
				local key = string.rep("k", 32) -- 32-byte key works for both
				local data = "test data"
				
				local aes_encrypted, err = crypto.encrypt.aes(data, string.rep("k", 16))
				assert(err == nil, "Unexpected AES encryption error")
				
				local chacha_encrypted, err = crypto.encrypt.chacha20(data, key)
				assert(err == nil, "Unexpected ChaCha20 encryption error")
				
				results.aes_encrypted_len = #aes_encrypted
				results.chacha_encrypted_len = #chacha_encrypted
				
				return results
			end
			return test
		`
		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		result, err := vm.Execute(newTestContext(), "test")
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, lua.LTTable, result.Type())
	})
}
