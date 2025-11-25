package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"hash"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func calculateHMAC(key, data string, hashType string) string {
	var h func() hash.Hash
	switch hashType {
	case "sha256":
		h = sha256.New
	case "sha512":
		h = sha512.New
	}

	mac := hmac.New(h, []byte(key))
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestHMACModuleWithVM(t *testing.T) {
	logger := zap.NewNop()

	// Test loading the module and registering the HMAC submodule
	t.Run("module loading", func(t *testing.T) {
		mod := NewCryptoModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local crypto = require("crypto")
			assert(type(crypto) == "table")
			assert(type(crypto.hmac) == "table")
			assert(type(crypto.hmac.sha256) == "function")
			assert(type(crypto.hmac.sha512) == "function")
		`, "test")
		assert.NoError(t, err)
	})

	// Test input validation
	t.Run("input validation", func(t *testing.T) {
		mod := NewCryptoModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Test empty key
		t.Run("empty key", func(t *testing.T) {
			err := vm.DoString(newTestContext(), `
				local crypto = require("crypto")
				local digest, err = crypto.hmac.sha256("", "data")
				assert(digest == nil, "Expected nil digest")
				assert(err ~= nil, "Expected error")
				assert(string.find(err, "key cannot be empty"), "Expected error about empty key")
			`, "test_empty_key")
			assert.NoError(t, err)
		})

		// Test nil key
		t.Run("nil key", func(t *testing.T) {
			err := vm.DoString(newTestContext(), `
				local crypto = require("crypto")
				local ok, err = pcall(function()
					return crypto.hmac.sha256(nil, "data")
				end)
				assert(not ok, "Expected error for nil key")
			`, "test_nil_key")
			assert.NoError(t, err)
		})

		// Test nil data
		t.Run("nil data", func(t *testing.T) {
			err := vm.DoString(newTestContext(), `
				local crypto = require("crypto")
				local ok, err = pcall(function()
					return crypto.hmac.sha256("key", nil)
				end)
				assert(not ok, "Expected error for nil data")
			`, "test_nil_data")
			assert.NoError(t, err)
		})
	})

	// Test correct HMAC calculation
	t.Run("HMAC calculation", func(t *testing.T) {
		mod := NewCryptoModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		testCases := []struct {
			name     string
			key      string
			data     string
			hashType string
		}{
			{
				name:     "simple sha256",
				key:      "secret-key",
				data:     "hello world",
				hashType: "sha256",
			},
			{
				name:     "simple sha512",
				key:      "secret-key",
				data:     "hello world",
				hashType: "sha512",
			},
			{
				name:     "empty data sha256",
				key:      "secret-key",
				data:     "",
				hashType: "sha256",
			},
			{
				name:     "empty data sha512",
				key:      "secret-key",
				data:     "",
				hashType: "sha512",
			},
			{
				name:     "unicode key and data sha256",
				key:      "密钥",
				data:     "你好，世界",
				hashType: "sha256",
			},
			{
				name:     "unicode key and data sha512",
				key:      "密钥",
				data:     "你好，世界",
				hashType: "sha512",
			},
			{
				name:     "long key sha256",
				key:      string(make([]byte, 1024)),
				data:     "data",
				hashType: "sha256",
			},
			{
				name:     "long data sha512",
				key:      "key",
				data:     string(make([]byte, 1024)),
				hashType: "sha512",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				script := `
					local crypto = require("crypto")
					function test(key, data)
						local result = {}
						local digest, err = crypto.hmac.` + tc.hashType + `(key, data)
						if err then
							result.success = false
							result.error = err
							return result
						end
						result.success = true
						result.digest = digest
						return result
					end
					return test
				`
				err = vm.Import(script, "test", "test")
				require.NoError(t, err)

				result, err := vm.Execute(newTestContext(), "test", lua.LString(tc.key), lua.LString(tc.data))
				require.NoError(t, err)

				resultTable, ok := result.(*lua.LTable)
				require.True(t, ok, "Expected result to be a table")

				success := resultTable.RawGetString("success")
				assert.Equal(t, lua.LTrue, success, "Expected operation to succeed")

				digest := resultTable.RawGetString("digest").String()
				expected := calculateHMAC(tc.key, tc.data, tc.hashType)
				assert.Equal(t, expected, digest, "HMAC mismatch for %s with key %q and data %q", tc.hashType, tc.key, tc.data)
			})
		}
	})

	// Test HMAC error handling in Lua
	t.Run("error handling in Lua", func(t *testing.T) {
		mod := NewCryptoModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
        local crypto = require("crypto")
        function test()
            local result = {}
            
            -- Test automatic type conversion (numbers are converted to strings)
            local numericKey, err1 = crypto.hmac.sha256(123, "data")
            local numericData, err2 = crypto.hmac.sha256("key", 456)
            
            -- Test with empty key (should error)
            local emptyKey, emptyKeyErr = crypto.hmac.sha256("", "data")
            
            -- Test with valid inputs
            local validHmac, validErr = crypto.hmac.sha256("key", "data")
            
            result.numeric_key_success = (err1 == nil)
            result.numeric_data_success = (err2 == nil)
            result.empty_key_error = (emptyKeyErr ~= nil)
            result.valid_hmac_success = (validErr == nil and type(validHmac) == "string")
            
            return result
        end
        return test
    `

		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		result, err := vm.Execute(newTestContext(), "test")
		require.NoError(t, err)

		resultTable, ok := result.(*lua.LTable)
		require.True(t, ok, "Expected result to be a table")

		// Numbers are automatically converted to strings in gopher-lua's CheckString
		assert.Equal(t, lua.LTrue, resultTable.RawGetString("numeric_key_success"),
			"Expected numeric key test to succeed due to auto-conversion")
		assert.Equal(t, lua.LTrue, resultTable.RawGetString("numeric_data_success"),
			"Expected numeric data test to succeed due to auto-conversion")

		// Empty key should still cause an error
		assert.Equal(t, lua.LTrue, resultTable.RawGetString("empty_key_error"),
			"Expected empty key test to fail")

		// Valid parameters should succeed
		assert.Equal(t, lua.LTrue, resultTable.RawGetString("valid_hmac_success"),
			"Expected valid parameters test to succeed")
	})
}
