package http

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ponyruntime/pony/api/service/http"
	"github.com/ponyruntime/pony/runtime/lua/engine"
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
		ctx := context.WithValue(context.Background(), http.RequestCtx, reqCtx)

		mod := NewHTTPContextModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		if err != nil {
			t.Fatalf("Failed to create VM: %v", err)
		}
		defer vm.Close()

		script := `
			local http = require("http")
			local req = http.request()
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
			local http = require("http")
			local req = http.request()
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
			local http = require("http")
			local req = http.request()
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
			local http = require("http")
			local req = http.request()
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
		// Spawn a reader that returns an error after a few reads
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
			local http = require("http")
			local req = http.request()
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
