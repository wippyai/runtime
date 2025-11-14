package crypto

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func TestCryptoModuleWithVM(t *testing.T) {
	logger := zap.NewNop()

	t.Run("module creation and loading", func(t *testing.T) {
		mod := NewCryptoModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local crypto = require("crypto")
			assert(type(crypto) == "table")
			assert(type(crypto.random) == "table")
			assert(type(crypto.hmac) == "table")
			assert(type(crypto.encrypt) == "table")
			assert(type(crypto.decrypt) == "table")
			assert(type(crypto.jwt) == "table")
			assert(type(crypto.pbkdf2) == "function")
			assert(type(crypto.constant_time_compare) == "function")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("pbkdf2 function with Serve", func(t *testing.T) {
		mod := NewCryptoModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local crypto = require("crypto")
			function test_pbkdf2(password, salt, iterations, key_length, hash_func)
				local result, err = crypto.pbkdf2(password, salt, iterations, key_length, hash_func)
				if err then
					return nil, err
				end
				return result
			end
			return test_pbkdf2
		`
		err = vm.Import(script, "test", "test_pbkdf2")
		require.NoError(t, err)

		// Test with valid parameters
		password := "password"
		salt := "salt"
		iterations := 1000
		keyLength := 32
		hashFunc := "sha256"

		result, err := vm.Execute(context.Background(), "test_pbkdf2",
			lua.LString(password),
			lua.LString(salt),
			lua.LNumber(iterations),
			lua.LNumber(keyLength),
			lua.LString(hashFunc))
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, lua.LTString, result.Type())
		assert.Equal(t, keyLength, len([]byte(result.String())))

		// Test with SHA512
		result, err = vm.Execute(context.Background(), "test_pbkdf2",
			lua.LString(password),
			lua.LString(salt),
			lua.LNumber(iterations),
			lua.LNumber(keyLength),
			lua.LString("sha512"))
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, lua.LTString, result.Type())
		assert.Equal(t, keyLength, len([]byte(result.String())))
	})

	t.Run("pbkdf2 function error cases", func(t *testing.T) {
		mod := NewCryptoModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Test with invalid iterations
		err = vm.DoString(context.Background(), `
			local crypto = require("crypto")
			local password = "password"
			local salt = "salt"
			local iterations = -1
			local key_length = 32
			local hash_func = "sha256"
			
			local result, err = crypto.pbkdf2(password, salt, iterations, key_length, hash_func)
			assert(result == nil, "Expected result to be nil")
			assert(err ~= nil, "Expected error")
			assert(string.find(err, "iterations must be positive"), "Expected error about iterations")
		`, "test_invalid_iterations")
		assert.NoError(t, err)

		// Test with invalid key length
		err = vm.DoString(context.Background(), `
			local crypto = require("crypto")
			local password = "password"
			local salt = "salt"
			local iterations = 1000
			local key_length = -1
			local hash_func = "sha256"
			
			local result, err = crypto.pbkdf2(password, salt, iterations, key_length, hash_func)
			assert(result == nil, "Expected result to be nil")
			assert(err ~= nil, "Expected error")
			assert(string.find(err, "key length must be positive"), "Expected error about key length")
		`, "test_invalid_key_length")
		assert.NoError(t, err)

		// Test with empty password
		err = vm.DoString(context.Background(), `
			local crypto = require("crypto")
			local password = ""
			local salt = "salt"
			local iterations = 1000
			local key_length = 32
			local hash_func = "sha256"
			
			local result, err = crypto.pbkdf2(password, salt, iterations, key_length, hash_func)
			assert(result == nil, "Expected result to be nil")
			assert(err ~= nil, "Expected error")
			assert(string.find(err, "password cannot be empty"), "Expected error about empty password")
		`, "test_empty_password")
		assert.NoError(t, err)

		// Test with empty salt
		err = vm.DoString(context.Background(), `
			local crypto = require("crypto")
			local password = "password"
			local salt = ""
			local iterations = 1000
			local key_length = 32
			local hash_func = "sha256"
			
			local result, err = crypto.pbkdf2(password, salt, iterations, key_length, hash_func)
			assert(result == nil, "Expected result to be nil")
			assert(err ~= nil, "Expected error")
			assert(string.find(err, "salt cannot be empty"), "Expected error about empty salt")
		`, "test_empty_salt")
		assert.NoError(t, err)

		// Test with invalid hash function
		err = vm.DoString(context.Background(), `
			local crypto = require("crypto")
			local password = "password"
			local salt = "salt"
			local iterations = 1000
			local key_length = 32
			local hash_func = "invalid_hash"
			
			local result, err = crypto.pbkdf2(password, salt, iterations, key_length, hash_func)
			assert(result == nil, "Expected result to be nil")
			assert(err ~= nil, "Expected error")
			assert(string.find(err, "unsupported hash function"), "Expected error about unsupported hash function")
		`, "test_invalid_hash")
		assert.NoError(t, err)
	})

	t.Run("constant_time_compare function", func(t *testing.T) {
		mod := NewCryptoModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local crypto = require("crypto")
			function test_compare(a, b)
				return crypto.constant_time_compare(a, b)
			end
			return test_compare
		`
		err = vm.Import(script, "test", "test_compare")
		require.NoError(t, err)

		// Test with equal strings
		result, err := vm.Execute(context.Background(), "test_compare",
			lua.LString("test"),
			lua.LString("test"))
		require.NoError(t, err)
		assert.Equal(t, lua.LTrue, result)

		// Test with different strings of same length
		result, err = vm.Execute(context.Background(), "test_compare",
			lua.LString("test"),
			lua.LString("tess"))
		require.NoError(t, err)
		assert.Equal(t, lua.LFalse, result)

		// Test with different length strings
		result, err = vm.Execute(context.Background(), "test_compare",
			lua.LString("test"),
			lua.LString("testing"))
		require.NoError(t, err)
		assert.Equal(t, lua.LFalse, result)

		// Test with empty strings
		result, err = vm.Execute(context.Background(), "test_compare",
			lua.LString(""),
			lua.LString(""))
		require.NoError(t, err)
		assert.Equal(t, lua.LTrue, result)
	})
}
