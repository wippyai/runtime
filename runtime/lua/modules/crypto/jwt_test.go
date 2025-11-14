package crypto

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func TestJWTModuleWithVM(t *testing.T) {
	logger := zap.NewNop()

	// Test loading the module and registering the JWT submodule
	t.Run("module loading", func(t *testing.T) {
		mod := NewCryptoModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local crypto = require("crypto")
			assert(type(crypto) == "table")
			assert(type(crypto.jwt) == "table")
			assert(type(crypto.jwt.encode) == "function")
			assert(type(crypto.jwt.verify) == "function")
		`, "test")
		assert.NoError(t, err)
	})

	// Test JWT encoding
	t.Run("JWT encoding", func(t *testing.T) {
		mod := NewCryptoModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Create a script that encodes a JWT
		script := `
			local crypto = require("crypto")
			function test(payload, key, alg)
				local result = {}
				local token, err = crypto.jwt.encode(payload, key, alg)
				if err then
					result.success = false
					result.error = err
					return result
				end
				result.success = true
				result.token = token
				return result
			end
			return test
		`
		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		testCases := []struct {
			name    string
			payload map[string]interface{}
			key     string
			alg     string
		}{
			{
				name: "simple claims with HS256",
				payload: map[string]interface{}{
					"sub":  "1234567890",
					"name": "John Doe",
					"iat":  1516239022,
				},
				key: "secret",
				alg: "HS256",
			},
			{
				name: "claims with HS384",
				payload: map[string]interface{}{
					"sub":   "1234567890",
					"name":  "John Doe",
					"admin": true,
					"iat":   1516239022,
				},
				key: "a-more-secure-key",
				alg: "HS384",
			},
			{
				name: "claims with HS512",
				payload: map[string]interface{}{
					"sub":   "1234567890",
					"name":  "John Doe",
					"admin": false,
					"iat":   1516239022,
					"exp":   time.Now().Add(time.Hour).Unix(),
				},
				key: "a-very-secure-key-that-is-longer",
				alg: "HS512",
			},
			{
				name: "nested claims",
				payload: map[string]interface{}{
					"user": map[string]interface{}{
						"id":    123,
						"name":  "John",
						"roles": []interface{}{"admin", "user"},
					},
					"iat": 1516239022,
				},
				key: "secret",
				alg: "HS256",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				// Convert payload to Lua table
				payloadTable := lua.LTable{}
				convertMapToLuaTable(t, tc.payload, &payloadTable)

				// Serve the function
				result, err := vm.Execute(context.Background(), "test", &payloadTable, lua.LString(tc.key), lua.LString(tc.alg))
				require.NoError(t, err)

				resultTable, ok := result.(*lua.LTable)
				require.True(t, ok, "Expected result to be a table")

				success := resultTable.RawGetString("success")
				assert.Equal(t, lua.LTrue, success, "Expected operation to succeed")

				// Verify the token is a string
				token := resultTable.RawGetString("token").(lua.LString).String()
				assert.NotEmpty(t, token, "Token should not be empty")

				// Validate token format (header.payload.signature)
				parts := strings.Split(token, ".")
				assert.Equal(t, 3, len(parts), "Token should have 3 parts")
				assert.NotEmpty(t, parts[0], "Header should not be empty")
				assert.NotEmpty(t, parts[1], "Value should not be empty")
				assert.NotEmpty(t, parts[2], "Signature should not be empty")

				// Parse the token using Go's JWT library to verify it's valid
				//nolint:revive // ok for now
				parsedToken, err := jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
					// FIXME do we need to use "tc" instead of "token"?
					return []byte(tc.key), nil
				})
				require.NoError(t, err)
				assert.True(t, parsedToken.Valid, "Token should be valid")

				// Verify the algorithm
				assert.Equal(t, tc.alg, parsedToken.Method.Alg())

				// Verify claims
				claims, ok := parsedToken.Claims.(jwt.MapClaims)
				assert.True(t, ok, "Should be able to extract claims")

				// Verify simple top-level claims
				for k, v := range tc.payload {
					// Skip nested maps for this check
					if _, isMap := v.(map[string]interface{}); !isMap {
						// Skip slices too
						if _, isSlice := v.([]interface{}); !isSlice {
							// Fix for number comparison - Lua uses float64 for all numbers
							switch expectedVal := v.(type) {
							case int:
								assert.Equal(t, float64(expectedVal), claims[k], "Claim %s should match", k)
							case int64:
								assert.Equal(t, float64(expectedVal), claims[k], "Claim %s should match", k)
							default:
								assert.Equal(t, v, claims[k], "Claim %s should match", k)
							}
						}
					}
				}
			})
		}
	})

	// Test JWT verification
	t.Run("JWT verification", func(t *testing.T) {
		mod := NewCryptoModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Create a script that verifies a JWT
		script := `
			local crypto = require("crypto")
			function encode_and_verify(payload, key, alg)
				local token, err = crypto.jwt.encode(payload, key, alg)
				assert(err == nil, "Encoding error: " .. tostring(err))
				
				local claims, err = crypto.jwt.verify(token, key, alg)
				if err then
					return nil, err
				end
				return claims
			end
			return encode_and_verify
		`
		err = vm.Import(script, "test", "encode_and_verify")
		require.NoError(t, err)

		testCases := []struct {
			name    string
			payload map[string]interface{}
			key     string
			alg     string
		}{
			{
				name: "simple claims with HS256",
				payload: map[string]interface{}{
					"sub":  "1234567890",
					"name": "John Doe",
				},
				key: "secret",
				alg: "HS256",
			},
			{
				name: "claims with HS384",
				payload: map[string]interface{}{
					"sub":   "1234567890",
					"admin": true,
				},
				key: "a-more-secure-key",
				alg: "HS384",
			},
			{
				name: "claims with HS512",
				payload: map[string]interface{}{
					"sub":   "1234567890",
					"name":  "John Doe",
					"admin": false,
				},
				key: "a-very-secure-key-that-is-longer",
				alg: "HS512",
			},
			{
				name: "nested claims",
				payload: map[string]interface{}{
					"user": map[string]interface{}{
						"id":    123,
						"name":  "John",
						"roles": []interface{}{"admin", "user"},
					},
				},
				key: "secret",
				alg: "HS256",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				// Convert payload to Lua table
				payloadTable := lua.LTable{}
				convertMapToLuaTable(t, tc.payload, &payloadTable)

				// Serve the function
				result, err := vm.Execute(context.Background(), "encode_and_verify", &payloadTable, lua.LString(tc.key), lua.LString(tc.alg))
				require.NoError(t, err)

				// Verify the result is a table
				require.Equal(t, lua.LTTable, result.Type(), "Update should be a table")

				// Helper function to verify claims
				verifyClaimsTable(t, tc.payload, result.(*lua.LTable))
			})
		}
	})

	// Test JWT verification failures
	t.Run("JWT verification failures", func(t *testing.T) {
		mod := NewCryptoModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Create a script for verification
		createScript := `
			local crypto = require("crypto")
			function create_token(payload, key, alg)
				local result = {}
				local token, err = crypto.jwt.encode(payload, key, alg)
				if err then
					result.success = false
					result.error = err
					return result
				end
				result.success = true
				result.token = token
				return result
			end
			return create_token
		`
		err = vm.Import(createScript, "create_script", "create_token")
		require.NoError(t, err)

		verifyScript := `
			local crypto = require("crypto")
			function verify_token(token, key, alg)
				local result = {}
				local claims, err = crypto.jwt.verify(token, key, alg)
				if err then
					result.success = false
					result.error = err
					return result
				end
				result.success = true
				result.claims = claims
				return result
			end
			return verify_token
		`
		err = vm.Import(verifyScript, "verify_script", "verify_token")
		require.NoError(t, err)

		// First create a valid token
		payload := &lua.LTable{}
		payload.RawSetString("sub", lua.LString("1234567890"))
		payload.RawSetString("name", lua.LString("John Doe"))

		key := "secret"
		alg := "HS256"
		wrongKey := "wrong-secret"

		// Create token
		result, err := vm.Execute(context.Background(), "create_token", payload, lua.LString(key), lua.LString(alg))
		require.NoError(t, err)

		resultTable, ok := result.(*lua.LTable)
		require.True(t, ok, "Expected result to be a table")

		success := resultTable.RawGetString("success")
		assert.Equal(t, lua.LTrue, success, "Expected token creation to succeed")

		token := resultTable.RawGetString("token").(lua.LString).String()

		// Test cases
		testCases := []struct {
			name          string
			token         string
			key           string
			alg           string
			expectedError string
		}{
			{
				name:          "wrong key",
				token:         token,
				key:           wrongKey,
				alg:           alg,
				expectedError: "failed to verify token",
			},
			{
				name:          "wrong algorithm",
				token:         token,
				key:           key,
				alg:           "HS384", // Original was HS256
				expectedError: "unexpected signing method",
			},
			{
				name:          "tampered token",
				token:         token + "tampered",
				key:           key,
				alg:           alg,
				expectedError: "failed to verify token",
			},
			{
				name:          "invalid token format",
				token:         "not.a.valid.token",
				key:           key,
				alg:           alg,
				expectedError: "failed to verify token",
			},
			{
				name:          "empty token",
				token:         "",
				key:           key,
				alg:           alg,
				expectedError: "failed to verify token",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				result, err := vm.Execute(context.Background(), "verify_token", lua.LString(tc.token), lua.LString(tc.key), lua.LString(tc.alg))
				require.NoError(t, err)

				resultTable, ok := result.(*lua.LTable)
				require.True(t, ok, "Expected result to be a table")

				success := resultTable.RawGetString("success")
				assert.Equal(t, lua.LFalse, success, "Expected verification to fail")

				errMsg := resultTable.RawGetString("error").String()
				assert.Contains(t, errMsg, tc.expectedError, "Expected specific error message")
			})
		}
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
				results.tests = {}
				
				-- Test encoding with invalid inputs
				local success = pcall(function()
					crypto.jwt.encode(nil, "key")
				end)
				results.tests["nil_payload"] = not success
				
				success = pcall(function()
					crypto.jwt.encode({}, nil)
				end)
				results.tests["nil_key"] = not success
				
				-- Test verification with invalid inputs
				success = pcall(function()
					crypto.jwt.verify(nil, "key")
				end)
				results.tests["nil_token"] = not success
				
				success = pcall(function()
					crypto.jwt.verify("token", nil)
				end)
				results.tests["nil_verify_key"] = not success
				
				-- Test with valid inputs
				local payload = {sub = "1234", name = "Test"}
				local key = "secret"
				
				local token, err = crypto.jwt.encode(payload, key)
				results.tests["valid_encode"] = (err == nil)
				
				local claims, verify_err = crypto.jwt.verify(token, key)
				results.tests["valid_verify"] = (verify_err == nil)
				results.tests["claims_match"] = (claims.sub == "1234" and claims.name == "Test")
				
				-- We don't test invalid algorithm here, as it doesn't throw an error but returns one
				
				results.token_length = #token
				
				return results
			end
			return test
		`
		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		result, err := vm.Execute(context.Background(), "test")
		require.NoError(t, err)

		resultTable, ok := result.(*lua.LTable)
		require.True(t, ok, "Expected result to be a table")

		testsTable := resultTable.RawGetString("tests").(*lua.LTable)
		assert.Equal(t, lua.LTrue, testsTable.RawGetString("nil_payload"), "Expected test with nil payload to fail")
		assert.Equal(t, lua.LTrue, testsTable.RawGetString("nil_key"), "Expected test with nil key to fail")
		assert.Equal(t, lua.LTrue, testsTable.RawGetString("nil_token"), "Expected test with nil token to fail")
		assert.Equal(t, lua.LTrue, testsTable.RawGetString("nil_verify_key"), "Expected test with nil verify key to fail")
		assert.Equal(t, lua.LTrue, testsTable.RawGetString("valid_encode"), "Expected valid encode to succeed")
		assert.Equal(t, lua.LTrue, testsTable.RawGetString("valid_verify"), "Expected valid verify to succeed")
		assert.Equal(t, lua.LTrue, testsTable.RawGetString("claims_match"), "Expected claims to match")

		tokenLength := resultTable.RawGetString("token_length")
		assert.True(t, tokenLength.(lua.LNumber) > 0, "Token length should be positive")
	})
}

// Helper function to convert a Go map to a Lua table
func convertMapToLuaTable(t *testing.T, m map[string]interface{}, table *lua.LTable) {
	for k, v := range m {
		switch val := v.(type) {
		case string:
			table.RawSetString(k, lua.LString(val))
		case int:
			table.RawSetString(k, lua.LNumber(val))
		case int64:
			table.RawSetString(k, lua.LNumber(val))
		case float64:
			table.RawSetString(k, lua.LNumber(val))
		case bool:
			table.RawSetString(k, lua.LBool(val))
		case map[string]interface{}:
			nestedTable := &lua.LTable{}
			convertMapToLuaTable(t, val, nestedTable)
			table.RawSetString(k, nestedTable)
		case []interface{}:
			nestedTable := &lua.LTable{}
			for i, item := range val {
				switch itemVal := item.(type) {
				case string:
					nestedTable.RawSetInt(i+1, lua.LString(itemVal))
				case int:
					nestedTable.RawSetInt(i+1, lua.LNumber(itemVal))
				case float64:
					nestedTable.RawSetInt(i+1, lua.LNumber(itemVal))
				case bool:
					nestedTable.RawSetInt(i+1, lua.LBool(itemVal))
				default:
					t.Logf("Unsupported array item type: %T", item)
				}
			}
			table.RawSetString(k, nestedTable)
		default:
			t.Logf("Unsupported type: %T", v)
		}
	}
}

// Helper function to verify claims in a Lua table against a Go map
func verifyClaimsTable(t *testing.T, expected map[string]interface{}, actual *lua.LTable) {
	for k, v := range expected {
		luaVal := actual.RawGetString(k)
		require.NotNil(t, luaVal, "Expected claim %s to exist", k)

		switch val := v.(type) {
		case string:
			assert.Equal(t, val, luaVal.String(), "String claim %s should match", k)
		case int:
			assert.Equal(t, float64(val), float64(luaVal.(lua.LNumber)), "Int claim %s should match", k)
		case int64:
			assert.Equal(t, float64(val), float64(luaVal.(lua.LNumber)), "Int64 claim %s should match", k)
		case float64:
			assert.Equal(t, val, float64(luaVal.(lua.LNumber)), "Float claim %s should match", k)
		case bool:
			assert.Equal(t, val, bool(luaVal.(lua.LBool)), "Boolean claim %s should match", k)
		case map[string]interface{}:
			assert.Equal(t, lua.LTTable, luaVal.Type(), "Expected nested claim %s to be a table", k)
			verifyClaimsTable(t, val, luaVal.(*lua.LTable))
		case []interface{}:
			assert.Equal(t, lua.LTTable, luaVal.Type(), "Expected array claim %s to be a table", k)
			arrayTable := luaVal.(*lua.LTable)
			for i, item := range val {
				// Lua tables are 1-indexed
				luaItem := arrayTable.RawGetInt(i + 1)
				require.NotNil(t, luaItem, "Expected array item %d to exist", i)

				switch itemVal := item.(type) {
				case string:
					assert.Equal(t, itemVal, luaItem.String(), "String array item should match")
				case int:
					assert.Equal(t, float64(itemVal), float64(luaItem.(lua.LNumber)), "Int array item should match")
				case float64:
					assert.Equal(t, itemVal, float64(luaItem.(lua.LNumber)), "Float array item should match")
				case bool:
					assert.Equal(t, itemVal, bool(luaItem.(lua.LBool)), "Boolean array item should match")
				default:
					t.Logf("Unsupported array item type in verification: %T", item)
				}
			}
		default:
			t.Logf("Unsupported type in verification: %T", v)
		}
	}
}

// generateRSAKeyPair generates a new RSA key pair for testing
func generateRSAKeyPair() (privateKeyPEM, publicKeyPEM string, err error) {
	// Generate private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", err
	}

	// Convert private key to PEM
	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	privateKeyPEM = string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyBytes,
	}))

	// Convert public key to PEM
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", "", err
	}
	publicKeyPEM = string(pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyBytes,
	}))

	return privateKeyPEM, publicKeyPEM, nil
}

func TestJWTModuleRS256(t *testing.T) {
	logger := zap.NewNop()

	// Generate RSA key pair for testing
	privateKeyPEM, publicKeyPEM, err := generateRSAKeyPair()
	require.NoError(t, err, "Failed to generate RSA key pair")

	// Test RS256 encoding
	t.Run("RS256 encoding", func(t *testing.T) {
		mod := NewCryptoModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Create a script that encodes a JWT with RS256
		script := `
			local crypto = require("crypto")
			function test(payload, key, alg)
				local result = {}
				local token, err = crypto.jwt.encode(payload, key, alg)
				if err then
					result.success = false
					result.error = err
					return result
				end
				result.success = true
				result.token = token
				return result
			end
			return test
		`
		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		// Prepare payload with _header field
		payload := &lua.LTable{}
		payload.RawSetString("sub", lua.LString("1234567890"))
		payload.RawSetString("name", lua.LString("John Doe"))
		payload.RawSetString("iat", lua.LNumber(time.Now().Unix()))

		// Add custom header
		headerTable := &lua.LTable{}
		headerTable.RawSetString("kid", lua.LString("test-key-id"))
		payload.RawSetString("_header", headerTable)

		// Encode the token
		result, err := vm.Execute(context.Background(), "test", payload, lua.LString(privateKeyPEM), lua.LString("RS256"))
		require.NoError(t, err)

		resultTable, ok := result.(*lua.LTable)
		require.True(t, ok, "Expected result to be a table")

		success := resultTable.RawGetString("success")
		assert.Equal(t, lua.LTrue, success, "Expected operation to succeed")

		// Verify the token is a string
		token := resultTable.RawGetString("token").(lua.LString).String()
		assert.NotEmpty(t, token, "Token should not be empty")

		// Validate token format (header.payload.signature)
		parts := strings.Split(token, ".")
		assert.Equal(t, 3, len(parts), "Token should have 3 parts")

		// Verify using Go's JWT library
		parsedToken, err := jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
			// Verify the algorithm
			if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
				t.Errorf("Unexpected signing method: %v", token.Header["alg"])
			}

			// Parse the public key
			block, _ := pem.Decode([]byte(publicKeyPEM))
			pubKey, err := x509.ParsePKIXPublicKey(block.Bytes)
			if err != nil {
				return nil, err
			}
			return pubKey.(*rsa.PublicKey), nil
		})
		require.NoError(t, err)
		assert.True(t, parsedToken.Valid, "Token should be valid")

		// Verify custom header is present
		assert.Equal(t, "test-key-id", parsedToken.Header["kid"], "Kid header should match")

		// Verify claims
		claims, ok := parsedToken.Claims.(jwt.MapClaims)
		assert.True(t, ok, "Should be able to extract claims")
		assert.Equal(t, "1234567890", claims["sub"], "Sub claim should match")
		assert.Equal(t, "John Doe", claims["name"], "Name claim should match")

		// Ensure _header is not present in claims
		_, hasHeaderInClaims := claims["_header"]
		assert.False(t, hasHeaderInClaims, "_header should not be in claims")
	})

	// Test RS256 verification
	t.Run("RS256 verification", func(t *testing.T) {
		mod := NewCryptoModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Create scripts for encoding and verifying
		encodeScript := `
			local crypto = require("crypto")
			function encode_jwt(payload, key, alg)
				local token, err = crypto.jwt.encode(payload, key, alg)
				if err then
					return nil, err
				end
				return token
			end
			return encode_jwt
		`
		err = vm.Import(encodeScript, "encode_script", "encode_jwt")
		require.NoError(t, err)

		verifyScript := `
			local crypto = require("crypto")
			function verify_jwt(token, key, alg)
				local claims, err = crypto.jwt.verify(token, key, alg)
				if err then
					return nil, err
				end
				return claims
			end
			return verify_jwt
		`
		err = vm.Import(verifyScript, "verify_script", "verify_jwt")
		require.NoError(t, err)

		// Prepare payload with _header field
		payload := &lua.LTable{}
		payload.RawSetString("sub", lua.LString("1234567890"))
		payload.RawSetString("name", lua.LString("John Doe"))
		payload.RawSetString("admin", lua.LBool(true))

		// Add custom header
		headerTable := &lua.LTable{}
		headerTable.RawSetString("kid", lua.LString("test-key-id"))
		payload.RawSetString("_header", headerTable)

		// Encode the token
		token, err := vm.Execute(context.Background(), "encode_jwt", payload, lua.LString(privateKeyPEM), lua.LString("RS256"))
		require.NoError(t, err)
		tokenStr := token.(lua.LString).String()

		// Verify the token
		claims, err := vm.Execute(context.Background(), "verify_jwt", lua.LString(tokenStr), lua.LString(publicKeyPEM), lua.LString("RS256"))
		require.NoError(t, err)

		// Verify the claims
		claimsTable, ok := claims.(*lua.LTable)
		require.True(t, ok, "Expected claims to be a table")

		// Check specific claims
		assert.Equal(t, "1234567890", claimsTable.RawGetString("sub").String(), "Sub claim should match")
		assert.Equal(t, "John Doe", claimsTable.RawGetString("name").String(), "Name claim should match")
		assert.Equal(t, lua.LTrue, claimsTable.RawGetString("admin"), "Admin claim should match")

		// Ensure _header is not present in claims
		assert.Equal(t, lua.LNil, claimsTable.RawGetString("_header"), "_header should not be in claims")
	})

	// Test error handling for RS256
	t.Run("RS256 error handling", func(t *testing.T) {
		mod := NewCryptoModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Prepare a properly escaped public key by replacing newlines with Lua escape sequences
		escapedPublicKey := strings.ReplaceAll(publicKeyPEM, "\n", "\\n")

		testScript := `
        local crypto = require("crypto")
        function test()
            local results = {}
            
            -- Test with invalid private key
            local token, err = crypto.jwt.encode({sub = "1234"}, "not-a-valid-key", "RS256")
            results.invalid_private_key = (err ~= nil and string.find(err, "invalid RSA private key") ~= nil)
            
            -- Test with invalid public key
            local dummy_token = "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.signature"
            local claims, verify_err = crypto.jwt.verify(dummy_token, "not-a-valid-key", "RS256")
            results.invalid_public_key = (verify_err ~= nil and string.find(verify_err, "failed to verify token") ~= nil)
            
            -- Test algorithm mismatch
            local hmac_token, _ = crypto.jwt.encode({sub = "1234"}, "secret", "HS256")
            local _, alg_err = crypto.jwt.verify(hmac_token, [=[` + escapedPublicKey + `]=], "RS256")
            results.algorithm_mismatch = (alg_err ~= nil and string.find(alg_err, "failed to verify token") ~= nil)
            
            return results
        end
        return test
    `
		err = vm.Import(testScript, "test_script", "test")
		require.NoError(t, err)

		results, err := vm.Execute(context.Background(), "test")
		require.NoError(t, err)

		resultsTable, ok := results.(*lua.LTable)
		require.True(t, ok, "Expected results to be a table")

		// Check that expected errors were detected
		assert.Equal(t, lua.LTrue, resultsTable.RawGetString("invalid_private_key"), "Should detect invalid private key")
		assert.Equal(t, lua.LTrue, resultsTable.RawGetString("invalid_public_key"), "Should detect invalid public key")
		assert.Equal(t, lua.LTrue, resultsTable.RawGetString("algorithm_mismatch"), "Should detect algorithm mismatch")
	})
}
