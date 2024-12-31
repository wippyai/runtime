package http

import (
	"bytes"
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestHTTPResponse(t *testing.T) {
	logger := zap.NewNop()

	t.Run("response headers handling", func(t *testing.T) {
		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString(`{"test":"data"}`)),
					Header: http.Header{
						"Content-Type":     []string{"application/json"},
						"X-Custom-Header":  []string{"custom-value"},
						"X-Empty-Header":   []string{},
						"X-Multi-Header":   []string{"value1", "value2"},
						"X-Special-Chars":  []string{"value with spaces & symbols"},
						"X-Unicode-Header": []string{"こんにちは"},
					},
					Request: req,
				}, nil
			},
		}

		mod := NewHTTPModule(mockClient, logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local http = require("http")
			local response = http.get("https://api.example.com/test")
			assert(response ~= nil, "Response should not be nil")
			assert(response.headers ~= nil, "Headers should not be nil")
			assert(response.headers["Content-Type"] == "application/json", "Content-Type mismatch")
			assert(response.headers["X-Custom-Header"] == "custom-value", "Custom header mismatch")
			assert(response.headers["X-Multi-Header"] == "value1", "Multi-value header mismatch")
			assert(response.headers["X-Special-Chars"] == "value with spaces & symbols", "Special chars header mismatch")
			assert(response.headers["X-Unicode-Header"] == "こんにちは", "Unicode header mismatch")
			assert(response.headers["Non-Existent"] == nil, "Non-existent header should be nil")
		`

		err = vm.DoString(nil, script, "test")
		assert.NoError(t, err)
	})

	t.Run("response cookies handling", func(t *testing.T) {
		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				resp := &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString(`{}`)),
					Request:    req,
				}
				resp.Header = make(http.Header)
				// Create cookies with proper encoding
				cookies := []*http.Cookie{
					{Name: "session", Value: "abc123", Path: "/"},
					{Name: "theme", Value: "dark", Path: "/"},
					{Name: "empty", Value: "", Path: "/"},

					{Name: "special", Value: "value with spaces & symbols", Path: "/"},
				}
				for _, cookie := range cookies {
					resp.Header.Add("Set-Cookie", cookie.String())
				}
				return resp, nil
			},
		}

		mod := NewHTTPModule(mockClient, logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local http = require("http")
			local response = http.get("https://api.example.com/test")
			assert(response ~= nil, "Response should not be nil")
			assert(response.cookies ~= nil, "Cookies should not be nil")
			assert(response.cookies["session"] == "abc123", "Session cookie mismatch: " .. tostring(response.cookies["session"]))
			assert(response.cookies["theme"] == "dark", "Theme cookie mismatch: " .. tostring(response.cookies["theme"]))
			assert(response.cookies["empty"] == "", "Empty cookie mismatch: " .. tostring(response.cookies["empty"]))
			assert(response.cookies["special"] == "value with spaces & symbols", "Special chars cookie mismatch")
			assert(response.cookies["non-existent"] == nil, "Non-existent cookie should be nil")
		`

		err = vm.DoString(nil, script, "test")
		assert.NoError(t, err)
	})

	t.Run("response body size handling", func(t *testing.T) {
		testCases := []struct {
			name     string
			body     string
			expected int
		}{
			{
				name:     "empty body",
				body:     "",
				expected: 0,
			},
			{
				name:     "small body",
				body:     "test",
				expected: 4,
			},
			{
				name:     "json body",
				body:     `{"key":"value"}`,
				expected: 15,
			},
			{
				name:     "unicode body",
				body:     "Hello, 世界",
				expected: 13,
			},
			{
				name:     "body with special chars",
				body:     "line1\nline2\r\ntab\there",
				expected: 21, // Corrected size accounting for all special chars
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mockClient := &mockHTTPClient{
					doFunc: func(req *http.Request) (*http.Response, error) {
						body := []byte(tc.body)
						return &http.Response{
							StatusCode: 200,
							Body:       io.NopCloser(bytes.NewReader(body)),
							Request:    req,
						}, nil
					},
				}

				mod := NewHTTPModule(mockClient, logger)
				vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
				require.NoError(t, err)
				defer vm.Close()

				script := fmt.Sprintf(`
					local http = require("http")
					local response = http.get("https://api.example.com/test")
					assert(response ~= nil, "Response should not be nil")
					assert(response.body_size == %d, string.format("Body size mismatch: expected %%d, got %%d", %d, response.body_size))
				`, tc.expected, tc.expected)

				err = vm.DoString(nil, script, "test")
				assert.NoError(t, err)
			})
		}
	})

	t.Run("response URL handling", func(t *testing.T) {
		testCases := []struct {
			name           string
			requestURL     string
			responseURL    string
			expectedFinal  string
			followRedirect bool
		}{
			{
				name:          "basic URL",
				requestURL:    "https://api.example.com/test",
				expectedFinal: "https://api.example.com/test",
			},
			{
				name:          "URL with query parameters",
				requestURL:    "https://api.example.com/test?key=value&other=123",
				expectedFinal: "https://api.example.com/test?key=value&other=123",
			},
			{
				name:          "URL with special characters",
				requestURL:    "https://api.example.com/test path/with space",
				expectedFinal: "https://api.example.com/test%20path/with%20space",
			},
			{
				name:           "URL with redirect",
				requestURL:     "https://api.example.com/old",
				responseURL:    "https://api.example.com/new",
				expectedFinal:  "https://api.example.com/new",
				followRedirect: true,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mockClient := &mockHTTPClient{
					doFunc: func(req *http.Request) (*http.Response, error) {
						resp := &http.Response{
							StatusCode: 200,
							Body:       io.NopCloser(bytes.NewBufferString(`{}`)),
							Request:    req,
						}
						if tc.followRedirect && tc.responseURL != "" {
							redirectURL, _ := req.URL.Parse(tc.responseURL)
							resp.Request.URL = redirectURL
						}
						return resp, nil
					},
				}

				mod := NewHTTPModule(mockClient, logger)
				vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
				require.NoError(t, err)
				defer vm.Close()

				script := fmt.Sprintf(`
					local http = require("http")
					local response = http.get("%s")
					assert(response ~= nil, "Response should not be nil")
					assert(response.url == "%s", string.format("URL mismatch: expected %%s, got %%s", "%s", response.url))
				`, tc.requestURL, tc.expectedFinal, tc.expectedFinal)

				err = vm.DoString(nil, script, "test")
				assert.NoError(t, err)
			})
		}
	})

	t.Run("response error cases", func(t *testing.T) {
		testCases := []struct {
			name        string
			setupMock   func() (*http.Response, error)
			luaScript   string
			shouldError bool
		}{
			{
				name: "nil headers",
				setupMock: func() (*http.Response, error) {
					return &http.Response{
						StatusCode: 200,
						Body:       io.NopCloser(bytes.NewBufferString(`{}`)),
						Header:     nil,
						Request:    &http.Request{},
					}, nil
				},
				luaScript: `
					local http = require("http")
					local response = http.get("https://api.example.com/test")
					local headers = response.headers
					assert(headers == nil, "Headers should be nil when response headers are nil")
				`,
				shouldError: false,
			},
			{
				name: "nil response",
				setupMock: func() (*http.Response, error) {
					return nil, fmt.Errorf("mock error")
				},
				luaScript: `
					local http = require("http")
					local response, err = http.get("https://api.example.com/test")
					assert(response == nil, "Response should be nil")
					assert(err ~= nil, "Error should not be nil")
					assert(string.find(err, "mock error") ~= nil, "Error message mismatch")
				`,
				shouldError: false,
			},
			{
				name: "nil request in response",
				setupMock: func() (*http.Response, error) {
					resp := &http.Response{
						StatusCode: 200,
						Body:       io.NopCloser(bytes.NewBufferString(`{}`)),
						Header:     make(http.Header),
						Request:    nil,
					}
					return resp, nil
				},
				luaScript: `
					local http = require("http")
					local response = http.get("https://api.example.com/test")
					assert(response ~= nil, "Response should not be nil")
					local success, err = pcall(function() return response.url end)
					assert(not success, "Should error when accessing url on nil request")
				`,
				shouldError: false,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mockClient := &mockHTTPClient{
					doFunc: func(req *http.Request) (*http.Response, error) {
						return tc.setupMock()
					},
				}

				mod := NewHTTPModule(mockClient, logger)
				vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
				require.NoError(t, err)
				defer vm.Close()

				err = vm.DoString(nil, tc.luaScript, "test")
				if tc.shouldError {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
				}
			})
		}
	})
}
