package http

import (
	"encoding/json"
	basehttp "net/http"
	"net/http/httptest"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/service/http"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"go.uber.org/zap"
)

// mockResponseWriter wraps httptest.ResponseRecorder to track header sent state
type mockResponseWriter struct {
	*httptest.ResponseRecorder
	headersSent bool
}

func newMockResponseWriter() *mockResponseWriter {
	return &mockResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
		headersSent:      false,
	}
}

func (m *mockResponseWriter) WriteHeader(code int) {
	m.headersSent = true
	m.ResponseRecorder.WriteHeader(code)
}

func (m *mockResponseWriter) Write(b []byte) (int, error) {
	m.headersSent = true
	return m.ResponseRecorder.Write(b)
}

func TestResponse_Basic(t *testing.T) {
	logger := zap.NewNop()

	t.Run("create new response", func(t *testing.T) {
		recorder := newMockResponseWriter()
		req := httptest.NewRequest("GET", "/test", nil)
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		fc := ctxapi.FrameFromContext(ctx)
		_ = fc.Set(http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		l := vm.State()
		l.SetContext(ctx)

		err = vm.DoString(ctx, `
			local http = require("http")
			local res = http.response()
			assert(res ~= nil, "response should not be nil")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("set status code", func(t *testing.T) {
		recorder := newMockResponseWriter()
		req := httptest.NewRequest("GET", "/test", nil)
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		fc := ctxapi.FrameFromContext(ctx)
		_ = fc.Set(http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local http = require("http")
			local res = http.response()
			res:set_status(http.STATUS.CREATED)
		`, "test")
		assert.NoError(t, err)
		assert.Equal(t, basehttp.StatusCreated, recorder.Code)
	})

	t.Run("set headers", func(t *testing.T) {
		recorder := newMockResponseWriter()
		req := httptest.NewRequest("GET", "/test", nil)
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		fc := ctxapi.FrameFromContext(ctx)
		_ = fc.Set(http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local http = require("http")
			local res = http.response()
			res:set_header("X-Test", "value")
			res:set_content_type(http.CONTENT.JSON)
		`, "test")
		assert.NoError(t, err)
		assert.Equal(t, "value", recorder.Header().Get("X-Test"))
		assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	})

	t.Run("write response body", func(t *testing.T) {
		recorder := newMockResponseWriter()
		req := httptest.NewRequest("GET", "/test", nil)
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		fc := ctxapi.FrameFromContext(ctx)
		_ = fc.Set(http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local http = require("http")
			local res = http.response()
			res:write("Hello, World!")
		`, "test")
		assert.NoError(t, err)
		assert.Equal(t, "Hello, World!", recorder.Body.String())
	})

	t.Run("write JSON response", func(t *testing.T) {
		recorder := newMockResponseWriter()
		req := httptest.NewRequest("GET", "/test", nil)
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		fc := ctxapi.FrameFromContext(ctx)
		_ = fc.Set(http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local http = require("http")
			local res = http.response()
			res:write_json({
				message = "Hello, JSON!",
				nested = {
					number = 42,
					bool = true,
					array = {1, 2, 3}
				}
			})
		`, "test")
		assert.NoError(t, err)

		// Verify it's valid JSON
		var result map[string]interface{}
		err = json.Unmarshal(recorder.Body.Bytes(), &result)
		assert.NoError(t, err)
		assert.Equal(t, "Hello, JSON!", result["message"])

		// Check nested structure
		nested, ok := result["nested"].(map[string]interface{})
		assert.True(t, ok, "should have nested object")
		assert.Equal(t, float64(42), nested["number"])
		assert.Equal(t, true, nested["bool"])

		// Check array
		array, ok := nested["array"].([]interface{})
		assert.True(t, ok, "should have array")
		assert.Equal(t, []interface{}{float64(1), float64(2), float64(3)}, array)

		assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	})
}

func TestResponse_ServerSentEvents(t *testing.T) {
	logger := zap.NewNop()

	t.Run("setup SSE response", func(t *testing.T) {
		recorder := newMockResponseWriter()
		req := httptest.NewRequest("GET", "/test", nil)
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		fc := ctxapi.FrameFromContext(ctx)
		_ = fc.Set(http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local http = require("http")
			local res = http.response()
			res:set_transfer(http.TRANSFER.SSE)
		`, "test")
		assert.NoError(t, err)
		assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
		assert.Equal(t, "no-cache", recorder.Header().Get("Cache-Control"))
		assert.Equal(t, "keep-alive", recorder.Header().Get("Connection"))
	})

	t.Run("write SSE event", func(t *testing.T) {
		recorder := newMockResponseWriter()
		req := httptest.NewRequest("GET", "/test", nil)
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		fc := ctxapi.FrameFromContext(ctx)
		_ = fc.Set(http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local http = require("http")
			local res = http.response()
			res:set_transfer(http.TRANSFER.SSE)
			res:write_event({
				name = "update",
				data = {progress = 50}
			})
		`, "test")
		assert.NoError(t, err)

		body := recorder.Body.String()
		assert.Contains(t, body, "event: update\n")
		assert.Contains(t, body, `data: {"progress":50}`)
		assert.Contains(t, body, "\n\n")
	})
}

func TestResponse_TransferEncoding(t *testing.T) {
	logger := zap.NewNop()

	t.Run("chunked transfer encoding", func(t *testing.T) {
		recorder := newMockResponseWriter()
		req := httptest.NewRequest("GET", "/test", nil)
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		fc := ctxapi.FrameFromContext(ctx)
		_ = fc.Set(http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local http = require("http")
			local res = http.response()
			res:set_transfer(http.TRANSFER.CHUNKED)
			res:write("chunk1")
			res:write("chunk2")
			res:write("chunk3")
		`, "test")
		assert.NoError(t, err)
		assert.Equal(t, "chunked", recorder.Header().Get("Transfer-Encoding"))
		assert.Equal(t, "chunk1chunk2chunk3", recorder.Body.String())
	})

	t.Run("change transfer mode after headers", func(t *testing.T) {
		recorder := newMockResponseWriter()
		req := httptest.NewRequest("GET", "/test", nil)
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		fc := ctxapi.FrameFromContext(ctx)
		_ = fc.Set(http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local http = require("http")
			local res = http.response()
			res:write("test")
			local err = res:set_transfer(http.TRANSFER.CHUNKED)
			assert(err ~= nil, "should error when changing transfer mode after write")
		`, "test")
		assert.Error(t, err)
	})
}

func TestResponse_ContentTypeHandling(t *testing.T) {
	logger := zap.NewNop()

	t.Run("set content type after headers", func(t *testing.T) {
		recorder := newMockResponseWriter()
		req := httptest.NewRequest("GET", "/test", nil)
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		fc := ctxapi.FrameFromContext(ctx)
		_ = fc.Set(http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local http = require("http")
			local res = http.response()
			res:write("test")
			local err = res:set_content_type(http.CONTENT.JSON)
			assert(err ~= nil, "should error when setting content type after write")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("empty content type", func(t *testing.T) {
		recorder := newMockResponseWriter()
		req := httptest.NewRequest("GET", "/test", nil)
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		fc := ctxapi.FrameFromContext(ctx)
		_ = fc.Set(http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local http = require("http")
			local res = http.response()
			local status, err = pcall(function()
				res:set_content_type("")
			end)
			assert(not status, "should fail with empty content type")
		`, "test")
		assert.NoError(t, err)
	})
}

func TestResponse_JSONHandling(t *testing.T) {
	logger := zap.NewNop()

	t.Run("write complex JSON response", func(t *testing.T) {
		recorder := newMockResponseWriter()
		req := httptest.NewRequest("GET", "/test", nil)
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		fc := ctxapi.FrameFromContext(ctx)
		_ = fc.Set(http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local http = require("http")
			local res = http.response()
			local data = {
				message = "Hello, JSON!",
				nested = {
					number = 42,
					boolean = true,
					array = {1, 2, 3}
				},
				tags = {"first", "second", "third"},
				metadata = {
					created = 1234567890,
					status = "active"
				}
			}
			local err = res:write_json(data)
			assert(err == nil, "writing valid table should succeed")
		`, "test")
		assert.NoError(t, err)

		// Verify it's valid JSON
		var result map[string]interface{}
		err = json.Unmarshal(recorder.Body.Bytes(), &result)
		assert.NoError(t, err)
		assert.Equal(t, "Hello, JSON!", result["message"])

		// Check nested structure
		nested, ok := result["nested"].(map[string]interface{})
		assert.True(t, ok, "should have nested object")
		assert.Equal(t, float64(42), nested["number"])
		assert.Equal(t, true, nested["boolean"])

		// Check array
		array, ok := nested["array"].([]interface{})
		assert.True(t, ok, "should have array")
		assert.Equal(t, []interface{}{float64(1), float64(2), float64(3)}, array)

		// Check tags array
		tags, ok := result["tags"].([]interface{})
		assert.True(t, ok, "should have tags array")
		assert.Equal(t, []interface{}{"first", "second", "third"}, tags)

		// Check metadata
		metadata, ok := result["metadata"].(map[string]interface{})
		assert.True(t, ok, "should have metadata object")
		assert.Equal(t, float64(1234567890), metadata["created"])
		assert.Equal(t, "active", metadata["status"])

		// Verify content type
		assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	})
}

func TestResponse_ErrorCases(t *testing.T) {
	logger := zap.NewNop()

	t.Run("set headers after write", func(t *testing.T) {
		recorder := newMockResponseWriter()
		req := httptest.NewRequest("GET", "/test", nil)
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		fc := ctxapi.FrameFromContext(ctx)
		_ = fc.Set(http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local http = require("http")
			local res = http.response()
			res:write("test")
			local err = res:set_header("X-Test", "value")
			assert(err ~= nil, "should error when setting header after write")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("set status after write", func(t *testing.T) {
		recorder := newMockResponseWriter()
		req := httptest.NewRequest("GET", "/test", nil)
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		fc := ctxapi.FrameFromContext(ctx)
		_ = fc.Set(http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local http = require("http")
			local res = http.response()
			res:write("test")
			local err = res:set_status(http.STATUS.OK)
			assert(err ~= nil, "should error when setting status after write")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("invalid status code", func(t *testing.T) {
		recorder := newMockResponseWriter()
		req := httptest.NewRequest("GET", "/test", nil)
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		fc := ctxapi.FrameFromContext(ctx)
		_ = fc.Set(http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local http = require("http")
			local res = http.response()
			local status, err = pcall(function()
				res:set_status(999)
			end)
			assert(not status, "should fail with invalid status code")
		`, "test")
		assert.NoError(t, err)
	})
}

func TestResponse_Flush(t *testing.T) {
	logger := zap.NewNop()

	t.Run("successful flush", func(t *testing.T) {
		recorder := newMockResponseWriter()
		req := httptest.NewRequest("GET", "/test", nil)
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		fc := ctxapi.FrameFromContext(ctx)
		_ = fc.Set(http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local http = require("http")
			local res = http.response()
			res:write("chunk1")
			res:flush()
			res:write("chunk2")
			res:flush()
		`, "test")
		assert.NoError(t, err)
		assert.Equal(t, "chunk1chunk2", recorder.Body.String())
		assert.True(t, recorder.headersSent)
	})

	t.Run("flush with invalid response", func(t *testing.T) {
		recorder := newMockResponseWriter()
		req := httptest.NewRequest("GET", "/test", nil)
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		fc := ctxapi.FrameFromContext(ctx)
		_ = fc.Set(http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Test with invalid response userdata
		err = vm.DoString(ctx, `
			local http = require("http")
			local res = newproxy() -- Spawn invalid userdata
			local status, err = pcall(function()
				res:flush()
			end)
			assert(not status, "flush should fail with invalid response object")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("flush in chunked transfer mode", func(t *testing.T) {
		recorder := newMockResponseWriter()
		req := httptest.NewRequest("GET", "/test", nil)
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		fc := ctxapi.FrameFromContext(ctx)
		_ = fc.Set(http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local http = require("http")
			local res = http.response()
			res:set_transfer(http.TRANSFER.CHUNKED)
			res:write("chunk1")
			res:flush()
			res:write("chunk2")
			res:flush()
		`, "test")
		assert.NoError(t, err)
		assert.Equal(t, "chunked", recorder.Header().Get("Transfer-Encoding"))
		assert.Equal(t, "chunk1chunk2", recorder.Body.String())
		assert.True(t, recorder.headersSent)
	})
}
