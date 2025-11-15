package http

import (
	ctxapi "github.com/wippyai/runtime/api/context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wippyai/runtime/api/service/http"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"go.uber.org/zap"
)

func TestRequest_StreamBody_Simple(t *testing.T) {
	logger := zap.NewNop()

	t.Run("stream simple text body", func(t *testing.T) {
		bodyContent := "Hello, Stream!"
		body := strings.NewReader(bodyContent)
		req := httptest.NewRequest("POST", "/test", body)
		recorder := httptest.NewRecorder()

		reqCtx := http.NewRequestContext(req, recorder)
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		fc := ctxapi.FrameFromContext(ctx)
		_ = fc.Set(http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		if err != nil {
			t.Fatalf("Failed to create VM: %v", err)
		}
		defer vm.Close()

		script := `
			local http = require("http")
			local req = http.request()
			local chunks = {}

			local stream, err = req:stream()

			while true do
				local chunk = stream:read()
				if chunk == nil then break end	
				table.insert(chunks, chunk)
			end

			local streamedBody = table.concat(chunks)
			if streamedBody ~= "` + bodyContent + `" then
				error("streamed body should match original body")
			end
		`
		err = vm.DoString(ctx, script, "test")
		if err != nil {
			t.Fatalf("Lua script execution failed: %v", err)
		}
	})
}

func TestRequest_StreamBody(t *testing.T) {
	logger := zap.NewNop()
	bodyContent := "This is a long body that should be streamed in chunks."

	t.Run("stream with default behavior", func(t *testing.T) {
		body := strings.NewReader(bodyContent)
		req := httptest.NewRequest("POST", "/test", body)
		recorder := httptest.NewRecorder()
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		fc := ctxapi.FrameFromContext(ctx)
		_ = fc.Set(http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		if err != nil {
			t.Fatalf("Failed to create VM: %v", err)
		}
		defer vm.Close()

		script := `
			local http = require("http")
			local req = http.request()
			local chunks = {}
			local stream, err = req:stream()
			if err ~= nil then
				error("stream failed: " .. err)
			end

			while true do
				local chunk = stream:read()
				if chunk == nil then break end
				table.insert(chunks, chunk)
			end
			
			local streamedBody = table.concat(chunks)
			if streamedBody ~= "` + bodyContent + `" then
				error("streamed body should match original body")
			end
		`
		err = vm.DoString(ctx, script, "test")
		if err != nil {
			t.Fatalf("Lua script execution failed: %v", err)
		}
	})

	t.Run("stream with custom read size", func(t *testing.T) {
		body := strings.NewReader(bodyContent)
		req := httptest.NewRequest("POST", "/test", body)
		recorder := httptest.NewRecorder()
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		fc := ctxapi.FrameFromContext(ctx)
		_ = fc.Set(http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		if err != nil {
			t.Fatalf("Failed to create VM: %v", err)
		}
		defer vm.Close()

		script := `
			local http = require("http")
			local req = http.request()
			local chunks = {}
			local readSize = 10
			local stream, err = req:stream()
			if err ~= nil then
				error("stream failed: " .. err)
			end

			while true do
				local chunk = stream:read(readSize)
				if chunk == nil then break end
				table.insert(chunks, chunk)
				if #chunk > readSize then
					error("chunk size should not exceed read size")
				end
			end
			
			local streamedBody = table.concat(chunks)
			if streamedBody ~= "` + bodyContent + `" then
				error("streamed body should match original body")
			end
		`
		err = vm.DoString(ctx, script, "test")
		if err != nil {
			t.Fatalf("Lua script execution failed: %v", err)
		}
	})

	t.Run("empty stream", func(t *testing.T) {
		body := strings.NewReader("") // Empty body
		req := httptest.NewRequest("POST", "/test", body)
		recorder := httptest.NewRecorder()
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		fc := ctxapi.FrameFromContext(ctx)
		_ = fc.Set(http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		if err != nil {
			t.Fatalf("Failed to create VM: %v", err)
		}
		defer vm.Close()

		script := `
			local http = require("http")
			local req = http.request()
			local stream, err = req:stream()
			if err ~= nil then
				error("stream failed: " .. err)
			end

			local chunk = stream:read()
			if chunk ~= nil then
				error("should receive nil for empty body")
			end
		`
		err = vm.DoString(ctx, script, "test")
		if err != nil {
			t.Fatalf("Lua script execution failed: %v", err)
		}
	})

	t.Run("error in stream chunk read", func(t *testing.T) {
		// Spawn a reader that returns an error after a few reads
		errorReader := &errorReader{
			data:  []byte(bodyContent),
			pos:   0,
			errAt: len(bodyContent) / 2, // Error after reading half the data
		}

		req := httptest.NewRequest("POST", "/test", errorReader)
		recorder := httptest.NewRecorder()
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		fc := ctxapi.FrameFromContext(ctx)
		_ = fc.Set(http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		if err != nil {
			t.Fatalf("Failed to create VM: %v", err)
		}
		defer vm.Close()

		script := `
			local http = require("http")
			local req = http.request()
			local chunks = {}
			local stream, err = req:stream()
			if err ~= nil then
				error("stream failed: " .. err)
			end

			local hasError = false
			while true do
				local chunk, err = stream:read(5)
				if err ~= nil then
					-- Expected error
					hasError = true
					break
				end
				if chunk == nil then
					break
				end
				table.insert(chunks, chunk)
			end
			
			if not hasError then
				error("should have encountered an error during read")
			end
		`
		err = vm.DoString(ctx, script, "test")
		if err != nil {
			t.Fatalf("Lua script execution failed: %v", err)
		}
	})
}
