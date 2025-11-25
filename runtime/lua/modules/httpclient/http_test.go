package httpclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"go.uber.org/zap"
)

// mockHTTPClient implements Client interface for testing
type mockHTTPClient struct {
	doFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return m.doFunc(req)
}

func TestHTTPModule(t *testing.T) {
	logger := zap.NewNop()

	t.Run("GET request with mock client", func(t *testing.T) {
		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				assert.Equal(t, "GET", req.Method)
				assert.Equal(t, "https://api.example.com/test", req.URL.String())

				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString(`{"message": "success"}`)),
					Header: http.Header{
						"Content-Type": []string{"application/json"},
					},
					Request: req,
				}, nil
			},
		}

		mod := NewHTTPClientModule(logger, mockClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local http = require("http_client")
			local response = http.get("https://api.example.com/test", {
				headers = {
					["User-Agent"] = "Test Client"
				}
			})
			
			assert(response.status_code == 200)
			assert(response.headers["Content-Type"] == "application/json")
			assert(response.body == '{"message": "success"}')
		`, "test")

		assert.NoError(t, err)
	})

	t.Run("POST request with mock client", func(t *testing.T) {
		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				assert.Equal(t, "POST", req.Method)
				assert.Equal(t, "https://api.example.com/data", req.URL.String())

				body, err := io.ReadAll(req.Body)
				assert.NoError(t, err)
				assert.Equal(t, `{"key": "value"}`, string(body))

				return &http.Response{
					StatusCode: 201,
					Body:       io.NopCloser(bytes.NewBufferString(`{"id": "123"}`)),
					Header: http.Header{
						"Content-type": []string{"application/json"},
					},
					Request: req,
				}, nil
			},
		}

		mod := NewHTTPClientModule(logger, mockClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local http = require("http_client")
			local response = http.post(
				"https://api.example.com/data",
				{
					headers = {
						["Content-type"] = "application/json"
					},
					body = '{"key": "value"}'
				}
			)
			
			assert(response.status_code == 201)
			assert(response.body == '{"id": "123"}')
		`, "test")

		assert.NoError(t, err)
	})

	t.Run("request with basic auth", func(t *testing.T) {
		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				username, password, ok := req.BasicAuth()
				assert.True(t, ok)
				assert.Equal(t, "testuser", username)
				assert.Equal(t, "testpass", password)

				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString(`{"authenticated": true}`)),
					Request:    req,
				}, nil
			},
		}

		mod := NewHTTPClientModule(logger, mockClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local http = require("http_client")
			local response = http.get("https://api.example.com/secure", {
				auth = {
					user = "testuser",
					pass = "testpass"
				}
			})
			assert(response.status_code == 200)
		`, "test")

		assert.NoError(t, err)
	})

	t.Run("request with cookies", func(t *testing.T) {
		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				cookies := req.Cookies()
				assert.Len(t, cookies, 2)

				// Spawn a map of cookie name to value for order-independent comparison
				cookieMap := make(map[string]string)
				for _, cookie := range cookies {
					cookieMap[cookie.Name] = cookie.Value
				}

				assert.Equal(t, "abc123", cookieMap["session"])
				assert.Equal(t, "dark", cookieMap["theme"])

				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString(`{"success": true}`)),
					Request:    req,
				}, nil
			},
		}

		mod := NewHTTPClientModule(logger, mockClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local http = require("http_client")
			local response = http.get("https://api.example.com/withcookies", {
				cookies = {
					session = "abc123",
					theme = "dark"
				}
			})
			assert(response.status_code == 200)
		`, "test")

		assert.NoError(t, err)
	})

	t.Run("error handling", func(t *testing.T) {
		mockClient := &mockHTTPClient{
			doFunc: func(*http.Request) (*http.Response, error) {
				return nil, errors.New("network error")
			},
		}

		mod := NewHTTPClientModule(logger, mockClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local http = require("http_client")
			local response, err = http.get("https://api.example.com/error")
			assert(response == nil)
			assert(tostring(err):find("network error") ~= nil)
		`, "test")

		assert.NoError(t, err)
	})

	t.Run("request batch", func(t *testing.T) {
		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				var responseBody string
				switch req.URL.Path {
				case "/one":
					responseBody = `{"id": "1"}`
				case "/two":
					responseBody = `{"id": "2"}`
				}

				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString(responseBody)),
					Request:    req,
				}, nil
			},
		}

		mod := NewHTTPClientModule(logger, mockClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local http = require("http_client")
			local responses = http.request_batch({
				{"GET", "https://api.example.com/one"},
				{"GET", "https://api.example.com/two"}
			})
			assert(#responses == 2)
			assert(responses[1].body == '{"id": "1"}')
			assert(responses[2].body == '{"id": "2"}')
		`, "test")

		assert.NoError(t, err)
	})

	t.Run("URL component encoding/decoding", func(t *testing.T) {
		mod := NewHTTPClientModule(logger, nil) // client not needed for this test
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local http = require("http_client")
			
			-- Test encoding
			local encoded = http.encode_uri("hello world & more")
			assert(encoded == "hello+world+%26+more")
			
			-- Test decoding
			local decoded = http.decode_uri("hello+world+%26+more")
			assert(decoded == "hello world & more")
		`, "test")

		assert.NoError(t, err)
	})

	t.Run("request with query parameters", func(t *testing.T) {
		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				assert.Equal(t, "param1=value1&param2=value2", req.URL.RawQuery)
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString(`{"success": true}`)),
					Request:    req,
				}, nil
			},
		}

		mod := NewHTTPClientModule(logger, mockClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local http = require("http_client")
			local response = http.get("https://api.example.com/query", {
				query = "param1=value1&param2=value2"
			})
			assert(response.status_code == 200)
		`, "test")

		assert.NoError(t, err)
	})
}

func TestHTTPModuleValidation(t *testing.T) {
	logger := zap.NewNop()
	mockClient := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewBufferString(`{}`)),
				Request:    req,
			}, nil
		},
	}

	mod := NewHTTPClientModule(logger, mockClient)

	t.Run("validation errors", func(t *testing.T) {
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		testCases := []struct {
			name     string
			code     string
			errorMsg string
		}{
			{
				name: "get with empty URL",
				code: `
					local http = require("http_client")
					local response = http.get("")
				`,
				errorMsg: "URL cannot be empty",
			},
			{
				name: "get with non-string URL",
				code: `
					local http = require("http_client")
					local response = http.get(123)
				`,
				errorMsg: "URL must be a string",
			},
			{
				name: "post with invalid auth table",
				code: `
					local http = require("http_client")
					local response = http.post("https://example.com", {
						auth = {
							user = false,
							pass = "test"
						}
					})
				`,
				errorMsg: "auth table must contain non-nil user and pass fields",
			},
			{
				name: "request_batch with empty table",
				code: `
					local http = require("http_client")
					local responses = http.request_batch({})
				`,
				errorMsg: "requests table cannot be empty",
			},
			{
				name: "request_batch with invalid request entry",
				code: `
					local http = require("http_client")
					local responses = http.request_batch({
						"not a table"
					})
				`,
				errorMsg: "request must be a table",
			},
			{
				name: "request with empty method",
				code: `
					local http = require("http_client")
					local response = http.request("", "https://example.com")
				`,
				errorMsg: "method cannot be empty",
			},
			{
				name: "request with non-string method",
				code: `
					local http = require("http_client")
					local response = http.request(123, "https://example.com")
				`,
				errorMsg: "invalid method",
			},
			{
				name: "encode_uri with no arguments",
				code: `
					local http = require("http_client")
					local encoded = http.encode_uri()
				`,
				errorMsg: "encode_uri requires exactly 1 string argument",
			},
			{
				name: "decode_uri with non-string argument",
				code: `
					local http = require("http_client")
					local decoded = http.decode_uri(123)
				`,
				errorMsg: "string expected",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				err = vm.DoString(newTestContext(), tc.code, "test")
				assert.Error(t, err)
			})
		}
	})
}

func TestHTTPModuleTimeouts(t *testing.T) {
	logger := zap.NewNop()

	t.Run("request timeout from options", func(t *testing.T) {
		requestStarted := make(chan struct{})   // Signal when request starts
		requestCompleted := make(chan struct{}) // Signal when request completes

		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				// Signal that request has started
				close(requestStarted)

				// wait until either canceled or test signals completion
				select {
				case <-req.Context().Done():
					return nil, req.Context().Err()
				case <-requestCompleted:
					return &http.Response{
						StatusCode: 200,
						Body:       io.NopCloser(bytes.NewBufferString(`{"status":"ok"}`)),
						Request:    req,
					}, nil
				}
			},
		}

		mod := NewHTTPClientModule(logger, mockClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Launch request in goroutine
		var wg sync.WaitGroup
		wg.Add(1)

		go func() {
			defer wg.Done()
			err := vm.DoString(newTestContext(), `
				local http = require("http_client")
				local response, err = http.get("https://api.example.com/test", {
					timeout = "100ms"  -- Very short timeout
				})
				assert(response == nil)
				assert(tostring(err):find("context deadline exceeded") ~= nil)
			`, "test")
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		}()

		// wait for request to start
		<-requestStarted

		// Give enough time for timeout to occur
		time.Sleep(200 * time.Millisecond)

		// At this point, the request should have timed out
		// Signal request to complete (though it should already be timed out)
		close(requestCompleted)

		// wait for test goroutine to complete
		wg.Wait()
	})

	t.Run("request timeout with explicit duration", func(t *testing.T) {
		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				// Verify the context has the expected timeout
				deadline, hasDeadline := req.Context().Deadline()
				assert.True(t, hasDeadline, "Context should have a deadline")

				// The deadline should be approximately 2 seconds from now
				expectedDeadline := time.Now().Add(2 * time.Second)
				assert.WithinDuration(t, expectedDeadline, deadline, 100*time.Millisecond)

				return nil, context.DeadlineExceeded
			},
		}

		mod := NewHTTPClientModule(logger, mockClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local http = require("http_client")
			local response, err = http.get("https://api.example.com/test", {
				timeout = 2  -- 2 second timeout
			})
			assert(response == nil)
			assert(tostring(err):find("context deadline exceeded") ~= nil)
		`, "test")

		assert.NoError(t, err)
	})

	t.Run("request with context cancellation", func(t *testing.T) {
		requestStarted := make(chan struct{})

		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				// Signal that request has started
				close(requestStarted)

				select {
				case <-req.Context().Done():
					// Return the standard error for canceled context
					return nil, context.Canceled
				case <-time.After(5 * time.Second): // Safety timeout
					return nil, errors.New("test timeout")
				}
			},
		}

		mod := NewHTTPClientModule(logger, mockClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Spawn a cancellable context
		baseCtx := ctxapi.NewRootContext()
		baseCtx, _ = ctxapi.OpenFrameContext(baseCtx)
		ctx, cancel := context.WithCancel(baseCtx)
		defer cancel()

		// Run the test in a goroutine
		done := make(chan error, 1)
		go func() {
			err := vm.DoString(ctx, `
                local http_client = require("http_client")
                local response, err = http_client.get("https://api.example.com/test")
                -- Just check if response is nil and we got any error back
                assert(response == nil)
                assert(err ~= nil)
                -- Don't check specific error text since it might vary
            `, "test")
			done <- err
		}()

		// wait for request to start
		select {
		case <-requestStarted:
			// Request started, now cancel it
			cancel()
		case <-time.After(time.Second):
			t.Fatal("Request didn't start in time")
		}

		// wait for completion
		select {
		case err := <-done:
			assert.Error(t, err)
		case <-time.After(time.Second):
			t.Fatal("Test didn't complete in time")
		}
	})

	t.Run("batch request timeout", func(t *testing.T) {
		var requestCount int32
		requestStarted := make(chan struct{})

		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				count := atomic.AddInt32(&requestCount, 1)
				if count == 1 {
					close(requestStarted)
				}

				if req.URL.Path == "/slow" {
					// wait for context timeout for slow request
					<-req.Context().Done()
					return nil, context.DeadlineExceeded
				}

				// Fast requests return immediately
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString(`{"status":"ok"}`)),
					Request:    req,
				}, nil
			},
		}

		mod := NewHTTPClientModule(logger, mockClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Launch request in goroutine
		done := make(chan error, 1)
		go func() {
			err := vm.DoString(newTestContext(), `
                local http = require("http_client")
                local responses, errors = http.request_batch({
                    {"GET", "https://api.example.com/fast"},
                    {"GET", "https://api.example.com/slow", {timeout = "100ms"}},
                    {"GET", "https://api.example.com/fast2"}
                })
                
                -- Assert fast requests succeeded
                assert(responses[1] ~= nil, "Fast request 1 should succeed")
                assert(responses[1].status_code == 200)
                assert(responses[3] ~= nil, "Fast request 3 should succeed")
                assert(responses[3].status_code == 200)
                
                -- Assert slow request timed out
                assert(responses[2] == nil, "Slow request should timeout")
                assert(errors[2] ~= nil, "Should have timeout error for slow request")
                assert(errors[2]:find("deadline exceeded") ~= nil, "Error should mention deadline exceeded")
            `, "test")
			done <- err
		}()

		// wait for first request to start
		select {
		case <-requestStarted:
			// continue
		case <-time.After(time.Second):
			t.Fatal("Requests didn't start in time")
		}

		// wait for completion
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Test failed: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Test didn't complete in time")
		}
	})

	t.Run("default timeout behavior", func(t *testing.T) {
		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				// Verify the context has the default timeout
				deadline, hasDeadline := req.Context().Deadline()
				assert.True(t, hasDeadline, "Context should have a deadline")

				// The deadline should be approximately 30 seconds from now (default timeout)
				expectedDeadline := time.Now().Add(90 * time.Second)
				assert.WithinDuration(t, expectedDeadline, deadline, 100*time.Millisecond)

				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString(`{"status":"ok"}`)),
					Request:    req,
				}, nil
			},
		}

		mod := NewHTTPClientModule(logger, mockClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local http = require("http_client")
			local response = http.get("https://api.example.com/test")
			assert(response ~= nil)
			assert(response.status_code == 200)
		`, "test")

		assert.NoError(t, err)
	})
}
