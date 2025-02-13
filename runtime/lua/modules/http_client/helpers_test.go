package http_client

import (
	"context"
	"net/http"
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func TestEncodeURI(t *testing.T) {
	tests := []struct {
		name        string
		input       lua.LValue
		expected    string
		expectError bool
	}{
		{
			name:     "simple string",
			input:    lua.LString("hello world"),
			expected: "hello+world",
		},
		{
			name:     "complex string",
			input:    lua.LString("test?name=John&age=25"),
			expected: "test%3Fname%3DJohn%26age%3D25",
		},
		{
			name:     "special characters",
			input:    lua.LString("!@#$%^&*()"),
			expected: "%21%40%23%24%25%5E%26%2A%28%29",
		},
		{
			name:     "unicode characters",
			input:    lua.LString("你好世界"),
			expected: "%E4%BD%A0%E5%A5%BD%E4%B8%96%E7%95%8C",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			// Push the test input onto the stack
			l.Push(tt.input)

			// Call encodeURI
			n := encodeURI(l)

			if tt.expectError {
				assert.Equal(t, 0, n, "should return 0 on error")
			} else {
				assert.Equal(t, 1, n, "should return 1 value")
				result := l.ToString(-1)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestDecodeURI(t *testing.T) {
	tests := []struct {
		name        string
		input       lua.LValue
		expected    string
		expectError bool
	}{
		{
			name:     "simple encoded string",
			input:    lua.LString("hello+world"),
			expected: "hello world",
		},
		{
			name:     "complex encoded string",
			input:    lua.LString("test%3Fname%3DJohn%26age%3D25"),
			expected: "test?name=John&age=25",
		},
		{
			name:     "encoded special characters",
			input:    lua.LString("%21%40%23%24%25%5E%26%2A%28%29"),
			expected: "!@#$%^&*()",
		},
		{
			name:     "encoded unicode characters",
			input:    lua.LString("%E4%BD%A0%E5%A5%BD%E4%B8%96%E7%95%8C"),
			expected: "你好世界",
		},
		{
			name:        "invalid encoded string",
			input:       lua.LString("%invalid"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			// Push the test input onto the stack
			l.Push(tt.input)

			// Call decodeURI
			n := decodeURI(l)

			if tt.expectError {
				if n == 0 {
					// Argument error
					assert.Equal(t, 0, n)
				} else {
					// Processing error (returns nil, error)
					assert.Equal(t, 2, n)
					assert.Equal(t, lua.LNil, l.Get(-2))
					assert.NotEqual(t, "", l.ToString(-1))
				}
			} else {
				assert.Equal(t, 1, n)
				result := l.ToString(-1)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestGetMethodFromArgs(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		expectError bool
	}{
		{"valid GET", "GET", false},
		{"valid POST", "POST", false},
		{"valid PUT", "PUT", false},
		{"valid DELETE", "DELETE", false},
		{"valid HEAD", "HEAD", false},
		{"valid PATCH", "PATCH", false},
		{"lowercase get", "GET", false},
		{"empty method", "", true},
		{"invalid method", "INVALID", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			l.Push(lua.LString(tt.method))
			method, err := getMethodFromArgs(l, 1)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.method, method)
			}
		})
	}
}

func TestGetURLFromArgs(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		pushType    lua.LValue
		expectError bool
	}{
		{"valid URL", "http://example.com", lua.LString("http://example.com"), false},
		{"empty URL", "", lua.LString(""), true},
		{"wrong type", "", lua.LNumber(123), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			l.Push(tt.pushType)
			url, err := getURLFromArgs(l, 1)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.url, url)
			}
		})
	}
}

func TestHelperFunctionsInVM(t *testing.T) {
	logger := zap.NewNop()

	t.Run("encodeURI in VM", func(t *testing.T) {
		// Spawn a new VM for this test
		vm, err := engine.NewVM(logger,
			engine.WithLoader("http_client", NewHTTPClientModule(logger, nil).Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			-- Store the module in a local variable
			local http = require("http_client")

			-- Test basic encoding
			assert(http.encode_uri("hello world") == "hello+world", "Basic encoding failed")
			
			-- Test special characters
			assert(http.encode_uri("!@#$%^&*()") == "%21%40%23%24%25%5E%26%2A%28%29", "Special characters encoding failed")
			
			-- Test URL components
			assert(http.encode_uri("test?name=John&age=25") == "test%3Fname%3DJohn%26age%3D25", "URL components encoding failed")
			
			-- Test Unicode
			assert(http.encode_uri("你好世界") == "%E4%BD%A0%E5%A5%BD%E4%B8%96%E7%95%8C", "Unicode encoding failed")
			
			-- Test error cases
			local success, err = pcall(function() http.encode_uri() end)
			assert(not success, "Should fail with no arguments")
			assert(err:find("exactly 1 string argument") ~= nil, "Wrong error message for no arguments")
			
			local success, err = pcall(function() http.encode_uri(123) end)
			assert(not success, "Should fail with non-string argument")
			assert(err:find("string expected") ~= nil, "Wrong error message for non-string argument")
			
			local success, err = pcall(function() http.encode_uri("test", "extra") end)
			assert(not success, "Should fail with extra arguments")
			assert(err:find("exactly 1 string argument") ~= nil, "Wrong error message for extra arguments")
		`, "test_encode_uri")

		assert.NoError(t, err, "VM test for encodeURI failed")
	})

	t.Run("decodeURI in VM", func(t *testing.T) {
		vm, err := engine.NewVM(logger,
			engine.WithLoader("http_client", NewHTTPClientModule(logger, nil).Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local http = require("http_client")

			-- Test basic decoding
			assert(http.decode_uri("hello+world") == "hello world", "Basic decoding failed")
			
			-- Test special characters
			assert(http.decode_uri("%21%40%23%24%25%5E%26%2A%28%29") == "!@#$%^&*()", "Special characters decoding failed")
			
			-- Test URL components
			assert(http.decode_uri("test%3Fname%3DJohn%26age%3D25") == "test?name=John&age=25", "URL components decoding failed")
			
			-- Test Unicode
			assert(http.decode_uri("%E4%BD%A0%E5%A5%BD%E4%B8%96%E7%95%8C") == "你好世界", "Unicode decoding failed")

			-- Test error cases
			local success, err = pcall(function() http.decode_uri() end)
			assert(not success, "Should fail with no arguments")
			assert(err:find("exactly 1 string argument") ~= nil, "Wrong error message for no arguments")
			
			local success, err = pcall(function() http.decode_uri(123) end)
			assert(not success, "Should fail with non-string argument")
			assert(err:find("string expected") ~= nil, "Wrong error message for non-string argument")
			
			-- Test invalid encoded string
			local result, err = http.decode_uri("%invalid")
			assert(result == nil, "Should return nil for invalid encoded string")
			assert(err ~= nil, "Should return error message for invalid encoded string")
			
			-- Test multiple arguments
			local success, err = pcall(function() http.decode_uri("test", "extra") end)
			assert(not success, "Should fail with extra arguments")
			assert(err:find("exactly 1 string argument") ~= nil, "Wrong error message for extra arguments")
		`, "test_decode_uri")

		assert.NoError(t, err, "VM test for decodeURI failed")
	})

	t.Run("method validation in VM", func(t *testing.T) {
		vm, err := engine.NewVM(logger,
			engine.WithLoader("http_client", NewHTTPClientModule(logger, http.DefaultClient).Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local http = require("http_client")

			-- Test valid methods
			local function test_method(method)
				local success = pcall(function()
					http.request(method, "https://example.com")
				end)
				return success
			end

			-- Valid methods should not throw errors
			assert(test_method("GET"), "GET method should be valid")
			assert(test_method("POST"), "POST method should be valid")
			assert(test_method("PUT"), "PUT method should be valid")
			assert(test_method("DELETE"), "DELETE method should be valid")
			assert(test_method("HEAD"), "HEAD method should be valid")
			assert(test_method("PATCH"), "PATCH method should be valid")

			-- Test case insensitivity
			assert(test_method("get"), "Lowercase GET should be valid")
			assert(test_method("Post"), "Mixed case POST should be valid")

			-- Test invalid methods
			local success, err = pcall(function() 
				http.request("INVALID", "https://example.com")
			end)
			assert(not success, "Invalid method should fail")
			assert(err:find("invalid method") ~= nil, "Wrong error message for invalid method")

			-- Test empty method
			local success, err = pcall(function()
				http.request("", "https://example.com")
			end)
			assert(not success, "Empty method should fail")
			assert(err:find("method cannot be empty") ~= nil, "Wrong error message for empty method")
		`, "test_method_validation")

		assert.NoError(t, err, "VM test for method validation failed")
	})

	t.Run("URL validation in VM", func(t *testing.T) {
		vm, err := engine.NewVM(logger,
			engine.WithLoader("http_client", NewHTTPClientModule(logger, http.DefaultClient).Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local http = require("http_client")

			-- Test valid URL
			local success = pcall(function()
				http.get("https://example.com")
			end)
			assert(success, "Valid URL should not throw error")

			-- Test empty URL
			local success, err = pcall(function()
				http.get("")
			end)
			assert(not success, "Empty URL should fail")
			assert(err:find("URL cannot be empty") ~= nil, "Wrong error message for empty URL")

			-- Test invalid URL type
			local success, err = pcall(function()
				http.get(123)
			end)
			assert(not success, "Non-string URL should fail")
			assert(err:find("URL must be a string") ~= nil, "Wrong error message for non-string URL")
		`, "test_url_validation")

		assert.NoError(t, err, "VM test for URL validation failed")
	})
}
