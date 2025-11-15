package httpclient

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"go.uber.org/zap"
)

func TestHTTPErrorMetadata(t *testing.T) {
	logger := zap.NewNop()

	t.Run("network error has metadata", func(t *testing.T) {
		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("connection refused")
			},
		}

		mod := NewHTTPClientModule(logger, mockClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local http = require("http_client")
			local response, err = http.get("https://api.example.com/test")

			assert(response == nil, "expected nil response")
			assert(err ~= nil, "expected error")

			-- Check error metadata
			assert(err:kind() == "Unavailable", "expected Unavailable kind, got: " .. err:kind())
			assert(err:retryable() == true, "expected retryable")

			local details = err:details()
			assert(details ~= nil, "expected details")
			assert(details.url == "https://api.example.com/test", "expected URL in details")
			assert(details.method == "GET", "expected method in details")
		`, "test")

		assert.NoError(t, err)
	})

	t.Run("IO error has metadata", func(t *testing.T) {
		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: 200,
					Body:       &errorReader{err: errors.New("read error")},
					Header:     http.Header{},
					Request:    req,
				}, nil
			},
		}

		mod := NewHTTPClientModule(logger, mockClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local http = require("http_client")
			local response, err = http.get("https://api.example.com/test")

			assert(response == nil, "expected nil response")
			assert(err ~= nil, "expected error")

			-- Check error metadata
			assert(err:kind() == "Internal", "expected Internal kind, got: " .. err:kind())
			assert(err:retryable() == false, "expected non-retryable")

			local details = err:details()
			assert(details ~= nil, "expected details")
			assert(details.operation == "read body", "expected operation in details")
		`, "test")

		assert.NoError(t, err)
	})

	t.Run("error metadata backward compatible", func(t *testing.T) {
		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("connection timeout")
			},
		}

		mod := NewHTTPClientModule(logger, mockClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local http = require("http_client")
			local response, err = http.get("https://api.example.com/test")

			assert(response == nil, "expected nil response")
			assert(err ~= nil, "expected error")

			-- Old-style error checking should still work
			assert(tostring(err):find("connection timeout") ~= nil, "expected error message")
		`, "test")

		assert.NoError(t, err)
	})
}

// errorReader is an io.ReadCloser that returns an error on Read
type errorReader struct {
	err error
}

func (e *errorReader) Read(p []byte) (int, error) {
	return 0, e.err
}

func (e *errorReader) Close() error {
	return nil
}
