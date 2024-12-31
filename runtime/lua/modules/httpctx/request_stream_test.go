package httpctx

import (
	"context"
	"github.com/ponyruntime/pony/api/service/http"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"go.uber.org/zap"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRequest_StreamBody_Simple(t *testing.T) {
	logger := zap.NewNop()

	t.Run("stream simple text body", func(t *testing.T) {
		bodyContent := "Hello, Stream!"
		body := strings.NewReader(bodyContent)
		req := httptest.NewRequest("POST", "/test", body)
		recorder := httptest.NewRecorder()
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := context.WithValue(context.Background(), http.RequestCtx, reqCtx)

		mod := NewHTTPContextModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		if err != nil {
			t.Fatalf("Failed to create VM: %v", err)
		}
		defer vm.Close()

		script := `
			local httpctx = require("httpctx")
			local req = httpctx.request()
			local chunks = {}
			for chunk in req:stream_body() do
				if chunk == nil then
					break
				end
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
	t.Run("stream_body with default options", func(t *testing.T) {
		body := strings.NewReader(bodyContent)
		req := httptest.NewRequest("POST", "/test", body)
		recorder := httptest.NewRecorder()
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := context.WithValue(context.Background(), http.RequestCtx, reqCtx)

		mod := NewHTTPContextModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		if err != nil {
			t.Fatalf("Failed to create VM: %v", err)
		}
		defer vm.Close()

		script := `
			local httpctx = require("httpctx")
			local req = httpctx.request()
			local chunks = {}
			local iter, err = req:stream_body()
			if err ~= nil then
				error("stream_body failed: " .. err)
			end

			for chunk in iter do
				if chunk == nil then
					break
				end
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

	t.Run("stream_body with custom buffer_size", func(t *testing.T) {
		body := strings.NewReader(bodyContent)
		req := httptest.NewRequest("POST", "/test", body)
		recorder := httptest.NewRecorder()
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := context.WithValue(context.Background(), http.RequestCtx, reqCtx)

		mod := NewHTTPContextModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		if err != nil {
			t.Fatalf("Failed to create VM: %v", err)
		}
		defer vm.Close()

		script := `
			local httpctx = require("httpctx")
			local req = httpctx.request()
			local chunks = {}
			local bufferSize = 10
			local iter, err = req:stream_body({buffer_size = bufferSize})
			if err ~= nil then
				error("stream_body failed: " .. err)
			end

			for chunk in iter do
				if chunk == nil then
					break
				end
				table.insert(chunks, chunk)
				if #chunk > bufferSize then
					error("chunk size should not exceed buffer size")
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

	t.Run("stream_body with custom max_size", func(t *testing.T) {
		body := strings.NewReader(bodyContent)
		req := httptest.NewRequest("POST", "/test", body)
		recorder := httptest.NewRecorder()
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := context.WithValue(context.Background(), http.RequestCtx, reqCtx)

		mod := NewHTTPContextModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		if err != nil {
			t.Fatalf("Failed to create VM: %v", err)
		}
		defer vm.Close()

		script := `
			local httpctx = require("httpctx")
			local req = httpctx.request()
			local chunks = {}
			local maxSize = 20
			local iter, err = req:stream_body({max_size = maxSize})
			if err ~= nil then
				error("stream_body failed: " .. err)
			end

			for chunk in iter do
				if chunk == nil then
					break
				end
				table.insert(chunks, chunk)
			end

			local streamedBody = table.concat(chunks)
			if #streamedBody > maxSize then
				error("streamed body size should not exceed max size, but in this case it should fail before")
			end
		`
		err = vm.DoString(ctx, script, "test")
		if err != nil {
			t.Fatalf("Lua script execution failed: %v", err)
		}
	})

	t.Run("stream_body with custom timeout", func(t *testing.T) {
		// Create a slow reader that will cause a timeout after sending some data
		slowReader := &delayReader{
			delay: 500 * time.Millisecond, // Shorter than the timeout but long enough to allow some data
			data:  []byte(bodyContent),
		}

		req := httptest.NewRequest("POST", "/test", slowReader)
		recorder := httptest.NewRecorder()
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := context.WithValue(context.Background(), http.RequestCtx, reqCtx)

		mod := NewHTTPContextModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		if err != nil {
			t.Fatalf("Failed to create VM: %v", err)
		}
		defer vm.Close()

		script := `
			local httpctx = require("httpctx")
			local req = httpctx.request({timeout = 2000})
			local chunks = {}
			local iter, err = req:stream_body({timeout = 1000}) -- Timeout after 1 second
			if err ~= nil then
				error("stream_body failed: " .. err)
			end

			for chunk in iter do
				if chunk == nil then
					error("timeout error should be returned")
					break
				end
				table.insert(chunks, chunk)
			end
		`
		err = vm.DoString(ctx, script, "test")
		if err != nil {
			t.Fatalf("Lua script execution failed: %v", err)
		}
	})

	t.Run("stream_body with all options combined", func(t *testing.T) {
		body := strings.NewReader(bodyContent)
		req := httptest.NewRequest("POST", "/test", body)
		recorder := httptest.NewRecorder()
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := context.WithValue(context.Background(), http.RequestCtx, reqCtx)

		mod := NewHTTPContextModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		if err != nil {
			t.Fatalf("Failed to create VM: %v", err)
		}
		defer vm.Close()

		script := `
			local httpctx = require("httpctx")
			local req = httpctx.request({timeout = 5000, max_body = 100})
			local chunks = {}
			local bufferSize = 15
			local maxSize = 50
			local iter, err = req:stream_body({buffer_size = bufferSize, max_size = maxSize, timeout = 2000})
			if err ~= nil then
				error("stream_body failed: " .. err)
			end

			for chunk in iter do
				if chunk == nil then
					break
				end
				table.insert(chunks, chunk)
				if #chunk > bufferSize then
					error("chunk size should not exceed buffer size")
				end
			end
			local streamedBody = table.concat(chunks)
			if #streamedBody > maxSize then
				error("streamed body size should not exceed max size, but should fail here before")
			end
		`
		err = vm.DoString(ctx, script, "test")
		if err != nil {
			t.Fatalf("Lua script execution failed: %v", err)
		}
	})

	t.Run("empty stream_body", func(t *testing.T) {
		body := strings.NewReader("") // Empty body
		req := httptest.NewRequest("POST", "/test", body)
		recorder := httptest.NewRecorder()
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := context.WithValue(context.Background(), http.RequestCtx, reqCtx)

		mod := NewHTTPContextModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		if err != nil {
			t.Fatalf("Failed to create VM: %v", err)
		}
		defer vm.Close()

		script := `
			local httpctx = require("httpctx")
			local req = httpctx.request()
			local iter, err = req:stream_body()
			if err ~= nil then
				error("stream_body failed: " .. err)
			end

			for chunk in iter do
				error("should not enter loop for empty body")
			end
		`
		err = vm.DoString(ctx, script, "test")
		if err != nil {
			t.Fatalf("Lua script execution failed: %v", err)
		}
	})

	t.Run("error in stream_body chunk read", func(t *testing.T) {
		// Create a reader that returns an error after a few reads
		errorReader := &errorReader{
			data:  []byte(bodyContent),
			pos:   0,
			errAt: len(bodyContent) / 2, // Error after reading half the data
		}

		req := httptest.NewRequest("POST", "/test", errorReader)
		recorder := httptest.NewRecorder()
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := context.WithValue(context.Background(), http.RequestCtx, reqCtx)

		mod := NewHTTPContextModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		if err != nil {
			t.Fatalf("Failed to create VM: %v", err)
		}
		defer vm.Close()

		script := `
			local httpctx = require("httpctx")
			local req = httpctx.request()
			local chunks = {}
			local iter, err = req:stream_body()
			if err ~= nil then
				error("stream_body failed: " .. err)
			end

			for chunk in iter do
				if chunk == nil then
					error("should not be nil when error")
					break
				end
				table.insert(chunks, chunk)
			end
		`
		err = vm.DoString(ctx, script, "test")
		if err != nil {
			t.Fatalf("Lua script execution failed: %v", err)
		}
	})
}
