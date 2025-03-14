package httpclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRequestInsidePcall(t *testing.T) {
	t.Run("async http requests", func(t *testing.T) {
		log := zap.NewNop()

		// Spawn a mock client that simulates network delay
		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				fmt.Println("DOING CALL")

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

		wrapped.GetCVM().State().SetGlobal(
			"print",
			wrapped.GetCVM().State().NewFunction(func(L *lua.LState) int {
				fmt.Println(">>", L.Get(1), "<<")
				return 0
			}),
		)

		// Imports test script with coroutines
		err = vm.Import(`
	       function test_http_requests()
			   function http_request()
				   local response, err = http_client.get("https://api.example.com/slower")
				   return response
			   end

	           -- Spawn slowest request in main flow
	           local response, err = pcall(http_request)	
	           return response
	       end
	   `, "test", "test_http_requests")
		require.NoError(t, err)

		// execute test and verify results
		result, err := wrapped.Execute(context.Background(), "test_http_requests")
		require.NoError(t, err)
		require.NotNil(t, result)
	})
}
