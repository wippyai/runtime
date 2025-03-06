package http

import (
	"context"
	"encoding/json"
	httpbase "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ponyruntime/pony/api/service/http"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestHttpHandler_Integration(t *testing.T) {
	logger := zap.NewNop()

	t.Run("handle JSON request and response", func(t *testing.T) {
		// Spawn a JSON request
		reqBody := `{"name": "Alice", "age": 30}`
		req := httptest.NewRequest("POST", "/api/users?role=admin", strings.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Request-ID", "test-123")

		recorder := httptest.NewRecorder()
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := context.WithValue(context.Background(), http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Lua script that handles both request and response
		script := `
			local http = require("http")
			
			-- Spawn request and response objects
			local req = http.request()
			local res = http.response()
			
			-- Verify request properties
			assert(req:method() == "POST", "incorrect method")
			assert(req:path() == "/api/users", "incorrect path")
			assert(req:query("role") == "admin", "incorrect query param")
			assert(req:header("X-Request-Id") == "test-123", "incorrect header") -- Headers are canonicalized
			assert(req:is_content_type(http.CONTENT.JSON), "should be JSON content type")
			
			-- Parse and verify request body
			local body = req:body_json()
			assert(body.name == "Alice", "incorrect name in request")
			assert(body.age == 30, "incorrect age in request")
			
			-- Prepare and send response
			-- Headers must be set before status code
			res:set_header("Content-type", http.CONTENT.JSON)
			res:set_header("X-Response-Id", "resp-456")
			
			-- Set status last before writing
			res:set_status(http.STATUS.CREATED)
			
			-- Write JSON response
			res:write_json({
				id = "user123",
				name = body.name,
				age = body.age,
				role = req:query("role"),
				status = "created"
			})
		    res:flush()
		`

		err = vm.DoString(ctx, script, "test")
		assert.NoError(t, err)

		// Verify response
		assert.Equal(t, httpbase.StatusCreated, recorder.Code)
		assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
		assert.Equal(t, "resp-456", recorder.Header().Get("X-Response-ID"))

		// Parse and verify response body
		var response map[string]interface{}
		err = json.Unmarshal(recorder.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "user123", response["id"])
		assert.Equal(t, "Alice", response["name"])
		assert.Equal(t, float64(30), response["age"])
		assert.Equal(t, "admin", response["role"])
		assert.Equal(t, "created", response["status"])
	})

	t.Run("handle streaming request and chunked response", func(t *testing.T) {
		reqBody := strings.NewReader("AAAAABBBBBCCCCC")
		req := httptest.NewRequest("POST", "/api/stream", reqBody)
		req.Header.Set("Content-Type", "text/plain")
		req.Header.Set("Accept", "text/plain")

		recorder := httptest.NewRecorder()
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := context.WithValue(context.Background(), http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local http = require("http")
			
			local req = http.request()
			local res = http.response()
			
			-- Set up chunked transfer encoding
			res:set_transfer(http.TRANSFER.CHUNKED)
			res:set_content_type(http.CONTENT.TEXT)
			
			-- Process request body in chunks
			local chunks = {}
			local stream, err = req:stream()
			assert(err == nil, "should have no error, got " .. tostring(err))

			while true do
				local chunk = stream:read(5)	
				
				if chunk == nil then break end
				table.insert(chunks, chunk)
				res:write("Chunk: " .. chunk .. "\n")
			end

			-- Verify we got all chunks
			assert(#chunks == 3, "should receive 3 chunks")
		`

		err = vm.DoString(ctx, script, "test")
		assert.NoError(t, err)

		// Verify response
		assert.Equal(t, httpbase.StatusOK, recorder.Code)
		assert.Equal(t, "text/plain", recorder.Header().Get("Content-Type"))
		assert.Equal(t, "chunked", recorder.Header().Get("Transfer-Encoding"))

		expected := "Chunk: AAAAA\nChunk: BBBBB\nChunk: CCCCC\n"
		assert.Equal(t, expected, recorder.Body.String())
	})

	t.Run("handle error cases", func(t *testing.T) {
		// Spawn an invalid JSON request
		reqBody := `{"name": "Alice", age: 30}` // Invalid JSON (missing quotes)
		req := httptest.NewRequest("POST", "/api/users", strings.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")

		recorder := httptest.NewRecorder()
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := context.WithValue(context.Background(), http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local http = require("http")
			
			local req = http.request()
			local res = http.response()
			
			-- Try to parse invalid JSON
			local body, err = req:body_json()
			assert(body == nil, "body should be nil for invalid JSON")
			assert(err ~= nil, "should have error for invalid JSON")
			
			-- send error response
			res:set_content_type(http.CONTENT.JSON)
			res:set_status(http.STATUS.BAD_REQUEST)
			res:write_json({
				error = http.ERROR.PARSE_FAILED,
				message = "Invalid JSON in request body"
			})
		`

		err = vm.DoString(ctx, script, "test")
		assert.NoError(t, err)

		// Verify error response
		assert.Equal(t, httpbase.StatusBadRequest, recorder.Code)
		assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))

		var response map[string]interface{}
		err = json.Unmarshal(recorder.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "PARSE_FAILED", response["error"])
		assert.Equal(t, "Invalid JSON in request body", response["message"])
	})

	t.Run("handle server-sent events", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/events", nil)
		req.Header.Set("Accept", "text/event-stream")

		recorder := httptest.NewRecorder()
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := context.WithValue(context.Background(), http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local http = require("http")
			
			local req = http.request()
			local res = http.response()
			
			-- Set up SSE
			res:set_transfer(http.TRANSFER.SSE)
			
			-- send multiple events
			res:write_event({
				name = "start",
				data = {status = "starting"}
			})
			
			res:write_event({
				name = "progress",
				data = {percent = 50}
			})
			
			res:write_event({
				name = "complete",
				data = {status = "done"}
			})
		`

		err = vm.DoString(ctx, script, "test")
		assert.NoError(t, err)

		assert.Equal(t, httpbase.StatusOK, recorder.Code)
		assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
		assert.Equal(t, "no-cache", recorder.Header().Get("Cache-Control"))
		assert.Equal(t, "keep-alive", recorder.Header().Get("Connection"))

		body := recorder.Body.String()
		assert.Contains(t, body, `event: start`)
		assert.Contains(t, body, `data: {"status":"starting"}`)
		assert.Contains(t, body, `event: progress`)
		assert.Contains(t, body, `data: {"percent":50}`)
		assert.Contains(t, body, `event: complete`)
		assert.Contains(t, body, `data: {"status":"done"}`)
	})
}
