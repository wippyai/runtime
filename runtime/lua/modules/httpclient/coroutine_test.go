package httpclient

import (
	"bytes"
	"context"
	ctxapi "github.com/wippyai/runtime/api/context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/coroutine"
	timemod "github.com/wippyai/runtime/runtime/lua/modules/time"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func newTestContext() context.Context {
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	return ctx
}

func TestAsyncHTTP(t *testing.T) {
	t.Run("async http requests", func(t *testing.T) {
		log := zap.NewNop()

		// Spawn a mock client that simulates network delay
		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				// Simulate network delay
				time.Sleep(50 * time.Millisecond)

				// Return response based on the URL path
				var responseBody string
				switch req.URL.Path {
				case "/fast":
					responseBody = `{"id": "fast"}`
				case "/slow":
					time.Sleep(100 * time.Millisecond) // Additional delay
					responseBody = `{"id": "slow"}`
				case "/slower":
					time.Sleep(150 * time.Millisecond) // Even more delay
					responseBody = `{"id": "slower"}`
				}

				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString(responseBody)),
					Header: http.Header{
						"Content-Type": []string{"application/json"},
					},
					Request: req,
				}, nil
			},
		}

		// Spawn base VM with HTTP module
		vm, err := engine.NewCVM(
			log,
			engine.WithPreloaded("http_client", NewHTTPClientModule(log, mockClient).Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		// Spawn wrapped VM with async runner
		wrapped := engine.NewRunner(
			vm,
			engine.WithLayer(coroutine.NewCoroutineLayer()),
		)

		// Imports test script with coroutines
		err = vm.Import(`
	       function test_http_requests()
	           local results = {}
	
	           -- Spawn first coroutine (fast request)
	           coroutine.spawn(function()
	               local response = http_client.get("https://api.example.com/fast")
	               results.fast = {
	                   status = response.status_code,
	                   body = response.body
	               }
	           end)
	
	           -- Spawn second coroutine (slow request)
	           coroutine.spawn(function()
	               local response = http_client.get("https://api.example.com/slow")
	               results.slow = {
	                   status = response.status_code,
	                   body = response.body
	               }
	           end)
	
	           -- Spawn slowest request in main flow
	           local response = http_client.get("https://api.example.com/slower")
	           results.slower = {
	               status = response.status_code,
	               body = response.body
	           }
	
	           return results
	       end
	   `, "test", "test_http_requests")
		require.NoError(t, err)

		// execute test and verify results
		start := time.Now()
		result, err := wrapped.Execute(newTestContext(), "test_http_requests")
		duration := time.Since(start)
		require.NoError(t, err)

		// Verify results
		resultTable := result.(*lua.LTable)

		// Check fast request results
		fastResult := resultTable.RawGetString("fast").(*lua.LTable)
		assert.Equal(t, lua.LNumber(200), fastResult.RawGetString("status"))
		assert.Equal(t, lua.LString(`{"id": "fast"}`), fastResult.RawGetString("body"))

		// Check slow request results
		slowResult := resultTable.RawGetString("slow").(*lua.LTable)
		assert.Equal(t, lua.LNumber(200), slowResult.RawGetString("status"))
		assert.Equal(t, lua.LString(`{"id": "slow"}`), slowResult.RawGetString("body"))

		// Check slower request results
		slowerResult := resultTable.RawGetString("slower").(*lua.LTable)
		assert.Equal(t, lua.LNumber(200), slowerResult.RawGetString("status"))
		assert.Equal(t, lua.LString(`{"id": "slower"}`), slowerResult.RawGetString("body"))

		// Verify execution time - should be closer to 200ms than 350ms
		// since requests run concurrently
		assert.Less(t, duration, 250*time.Millisecond,
			"requests should complete in parallel, took %v", duration)
	})

	t.Run("async http request with timeout", func(t *testing.T) {
		log := zap.NewNop()

		// Spawn a mock client that simulates a slow response
		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				select {
				case <-req.Context().Done():
					return nil, req.Context().Err()
				case <-time.After(200 * time.Millisecond):
					return &http.Response{
						StatusCode: 200,
						Body:       io.NopCloser(bytes.NewBufferString(`{"status": "ok"}`)),
						Request:    req,
					}, nil
				}
			},
		}

		// Spawn base VM with HTTP module
		vm, err := engine.NewCVM(
			log,
			engine.WithPreloaded("http_client", NewHTTPClientModule(log, mockClient).Loader),
			engine.WithPreloaded("time", timemod.NewTimeModule().Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		// Spawn wrapped VM with async runner
		wrapped := engine.NewRunner(
			vm,
			engine.WithLayer(coroutine.NewCoroutineLayer()),
		)

		// Imports test script
		err = vm.Import(`
            function test_timeout()
                local result
                local error_msg
                print("hey")
                -- Spawn request with short timeout
                coroutine.spawn(function()
                    local response, err = http_client.get("https://api.example.com/slow", {
                        timeout = "100ms"
                    })
                    result = response
                    error_msg = err
                end)

                -- wait a bit to ensure request completes
                time.sleep(time.parse_duration("150ms"))	
                return {result, error_msg}
            end
        `, "test", "test_timeout")
		require.NoError(t, err)

		// execute test
		results, err := wrapped.Execute(newTestContext(), "test_timeout")
		require.NoError(t, err)

		// Unpack results
		resultTable := results.(*lua.LTable)
		response := resultTable.RawGetInt(1)
		errorMsg := resultTable.RawGetInt(2)

		// Verify timeout behavior
		assert.Equal(t, lua.LNil, response, "response should be nil due to timeout")
		assert.Contains(t, errorMsg.String(), "context deadline exceeded", "should get timeout error")
	})
}
