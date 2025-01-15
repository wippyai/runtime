package http

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/ponyruntime/pony/runtime/lua/engine"

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
			assert(response.headers["Content-Type"] == "application/json", "Content-type mismatch")
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

// mockReadCloser implements io.ReadCloser for testing
type mockReadCloser struct {
	reader    *bytes.Reader
	mu        sync.Mutex
	closed    bool
	delay     time.Duration
	errAfter  int
	bytesRead int
	injectErr error
}

func newMockReadCloser(data []byte, delay time.Duration) *mockReadCloser {
	return &mockReadCloser{
		reader:    bytes.NewReader(data),
		delay:     delay,
		injectErr: errors.New("mock error"),
	}
}

func (m *mockReadCloser) Read(p []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return 0, io.ErrClosedPipe
	}

	if m.delay > 0 {
		time.Sleep(m.delay)
	}

	if m.errAfter > 0 && m.bytesRead >= m.errAfter {
		return 0, m.injectErr
	}

	n, err = m.reader.Read(p)
	m.bytesRead += n
	return n, err
}

func (m *mockReadCloser) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.closed = true
	return nil
}

func TestStreamedResponseBodyHandling(t *testing.T) {
	logger := zap.NewNop()

	t.Run("read by chunk", func(t *testing.T) {
		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				body := []byte("chunk1chunk2chunk3")
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewReader(body)),
					Request:    req,
				}, nil
			},
		}

		mod := NewHTTPModule(mockClient, logger)
		vm, err := engine.NewVM(
			logger,
			engine.WithLoader(mod.Name(), mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
		local http = require("http")

		local response = http.get("https://api.example.com/test", { stream = { buffer_size = 6 } })
		assert(response ~= nil, "Response should not be nil")
		assert(response.stream ~= nil, "Response stream should not be nil")
		assert(response.body == nil, "Response body should be nil")
		assert(response.body_size == -1, "Body size should be -1 when streaming")

		local s = response.stream
		local expected = {"chunk1", "chunk2", "chunk3"}
		local idx = 1

		for chunk in s() do
			assert(chunk == expected[idx], string.format("chunk %d mismatch", idx))
			idx = idx + 1
		end

		-- Verify we got all expected chunks
		assert(idx - 1 == #expected, "wrong number of iterations")

		-- Try one more iteration to ensure proper EOF handling
		local iter = s()
		local final = iter()
		assert(final == nil, "expected nil after all chunks read")
	`

		err = vm.DoString(context.Background(), script, "test")
		assert.NoError(t, err)
	})

	t.Run("timeout during read", func(t *testing.T) {
		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				// Simulate a slow response that will cause a timeout
				body := []byte("data")
				reader := newMockReadCloser(body, 200*time.Millisecond)
				return &http.Response{
					StatusCode: 200,
					Body:       reader,
					Request:    req,
				}, nil
			},
		}

		mod := NewHTTPModule(mockClient, logger)
		vm, err := engine.NewVM(
			logger,
			engine.WithLoader(mod.Name(), mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
		local http = require("http")

		local response = http.get("https://api.example.com/test", { timeout = "100ms", stream = {} })
		assert(response ~= nil, "Response should not be nil")

		local s = response.stream

		local chunk, err = s:read()
		assert(chunk == nil, "Chunk should be nil due to timeout")
		assert(string.find(err, "context deadline"), "Error should indicate a timeout or canceled context")
	`

		err = vm.DoString(context.Background(), script, "test")
		assert.NoError(t, err)
	})

	t.Run("error during read", func(t *testing.T) {
		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				// Simulate an error after reading some data
				body := []byte("chunk1chunk2")
				reader := newMockReadCloser(body, 0)
				reader.errAfter = 6 // Inject error after 6 bytes
				return &http.Response{
					StatusCode: 200,
					Body:       reader,
					Request:    req,
				}, nil
			},
		}

		mod := NewHTTPModule(mockClient, logger)
		vm, err := engine.NewVM(
			logger,
			engine.WithLoader(mod.Name(), mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
		local http = require("http")

		local response = http.get("https://api.example.com/test", { stream = { buffer_size = 6 } })
		assert(response ~= nil, "Response should not be nil")

		local s = response.stream
		local chunk, err = s:read()
		assert(chunk == "chunk1", "First chunk should be read successfully")
		assert(err == nil, "Error should be nil for the first chunk")

		chunk, err = s:read()
		assert(chunk == nil, "Chunk should be nil due to error")
		assert(string.find(err, "mock error"), "Error should indicate the injected error")
	`

		err = vm.DoString(context.Background(), script, "test")
		assert.NoError(t, err)
	})

	t.Run("close stream", func(t *testing.T) {
		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				body := []byte("data")
				reader := newMockReadCloser(body, 0)
				return &http.Response{
					StatusCode: 200,
					Body:       reader,
					Request:    req,
				}, nil
			},
		}

		mod := NewHTTPModule(mockClient, logger)
		vm, err := engine.NewVM(
			logger,
			engine.WithLoader(mod.Name(), mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
		local http = require("http")

		local response = http.get("https://api.example.com/test", { stream = {} })
		assert(response ~= nil, "Response should not be nil")

		local s = response.stream
		s:close()

		local chunk, err = s:read()
		assert(chunk == nil, "Chunk should be nil after closing")
		assert(string.find(err, "closed"), "Error should indicate stream is closed")
	`

		err = vm.DoString(context.Background(), script, "test")
		assert.NoError(t, err)
	})

	t.Run("buffer size and chunk sizes", func(t *testing.T) {
		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				body := []byte("chunk1chunk2chunk3") // 18 bytes total
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewReader(body)),
					Request:    req,
				}, nil
			},
		}

		mod := NewHTTPModule(mockClient, logger)
		vm, err := engine.NewVM(
			logger,
			engine.WithLoader(mod.Name(), mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
    local http = require("http")

    local response = http.get("https://api.example.com/test", { stream = { buffer_size = 5 } })
    assert(response ~= nil, "Response should not be nil")

    local s = response.stream
    local expected_chunks = {"chunk", "1chun", "k2chu", "nk3"}
    local idx = 1

    for chunk in s() do
        assert(chunk == expected_chunks[idx], string.format("Chunk %d mismatch: expected '%s', got '%s'", idx, expected_chunks[idx], chunk))
        idx = idx + 1
    end

    assert(idx - 1 == #expected_chunks, "Wrong number of chunks received")
    `

		err = vm.DoString(context.Background(), script, "test")
		assert.NoError(t, err)
	})
}
