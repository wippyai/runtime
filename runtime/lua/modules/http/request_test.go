package http

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	ctxapi "github.com/wippyai/runtime/api/context"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/service/http"
	"github.com/wippyai/runtime/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

type delayReader struct {
	delay time.Duration
	data  []byte
	pos   int
}

func (r *delayReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, errors.New("timeout")
	}
	time.Sleep(r.delay)
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return
}

func (r *delayReader) Close() error {
	return nil
}

// errorReader is a custom io.Reader that returns an error after a certain number of bytes have been read.
type errorReader struct {
	data  []byte
	pos   int
	errAt int
}

func (r *errorReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}

	if r.pos >= r.errAt {
		return 0, fmt.Errorf("simulated error at position %d", r.pos)
	}

	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func (r *errorReader) Close() error {
	return nil // Nothing to close in this example
}

func lprint(l *lua.LState) int {
	// print msg
	msg := l.CheckString(1)
	println(msg)
	return 0
}

func TestRequest_Creation(t *testing.T) {
	logger := zap.NewNop()

	t.Run("create request with default options", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		recorder := httptest.NewRecorder()
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
			local req = http.request()
			assert(req ~= nil, "request should not be nil")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("create request with timeout option", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		recorder := httptest.NewRecorder()
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
			local req = http.request({timeout = 5000})
			assert(req ~= nil, "request should not be nil")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("create request with max_body option", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		recorder := httptest.NewRecorder()
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
			local req = http.request({max_body = 1024})
			assert(req ~= nil, "request should not be nil")
		`, "test")
		assert.NoError(t, err)
	})
}

func TestRequest_BasicInfo(t *testing.T) {
	logger := zap.NewNop()

	t.Run("get request method", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/test", nil)
		recorder := httptest.NewRecorder()
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
			local req = http.request()
			local method = req:method()
			assert(method == "POST", "incorrect request method")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("get request path", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/users", nil)
		recorder := httptest.NewRecorder()
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
			local req = http.request()
			local path = req:path()
			assert(path == "/api/users", "incorrect request path")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("get request host", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Host = "example.com"
		recorder := httptest.NewRecorder()
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
			local req = http.request()
			local host = req:host()
			assert(host == "example.com", "incorrect request host")
		`, "test")
		assert.NoError(t, err)
	})
}

func TestRequest_Query(t *testing.T) {
	logger := zap.NewNop()

	t.Run("get existing query parameter", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test?name=john&age=25", nil)
		recorder := httptest.NewRecorder()
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
			local req = http.request()
			local name = req:query("name")
			assert(name == "john", "incorrect query value for name")
			local age = req:query("age")
			assert(age == "25", "incorrect query value for age")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("get non-existent query parameter", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		recorder := httptest.NewRecorder()
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
			local req = http.request()
			local value = req:query("nonexistent")
			assert(value == nil, "non-existent query parameter should return nil")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("empty query parameter key", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		recorder := httptest.NewRecorder()
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
			local req = http.request()
			local value, err = req:query("")
			assert(value == nil, "empty query key should return nil")
			assert(err ~= nil, "empty query key should return error")
		`, "test")
		assert.NoError(t, err)
	})
}

func TestRequest_Body(t *testing.T) {
	logger := zap.NewNop()

	t.Run("read text body", func(t *testing.T) {
		body := strings.NewReader("Hello, World!")
		req := httptest.NewRequest("POST", "/test", body)
		recorder := httptest.NewRecorder()
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
			local req = http.request()
			local body = req:body()
			assert(body == "Hello, World!", "incorrect body content")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("read JSON body", func(t *testing.T) {
		body := strings.NewReader(`{"name": "john", "age": 25}`)
		req := httptest.NewRequest("POST", "/test", body)
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
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
			local req = http.request()
			local data = req:body_json()
			assert(data.name == "john", "incorrect JSON name field")
			assert(data.age == 25, "incorrect JSON age field")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("check body presence", func(t *testing.T) {
		body := strings.NewReader("test")
		req := httptest.NewRequest("POST", "/test", body)
		recorder := httptest.NewRecorder()
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
			local req = http.request()
			local has_body = req:has_body()
			assert(has_body == true, "should have body")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("get content length", func(t *testing.T) {
		body := strings.NewReader("12345")
		req := httptest.NewRequest("POST", "/test", body)
		recorder := httptest.NewRecorder()
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
			local req = http.request()
			local length = req:content_length()
			assert(length == 5, "incorrect content length")
		`, "test")
		assert.NoError(t, err)
	})
}

func TestRequest_ContentType(t *testing.T) {
	logger := zap.NewNop()

	t.Run("check exact content type match", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/test", nil)
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
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
			local req = http.request()
			local is_json = req:is_content_type(http.CONTENT.JSON)
			assert(is_json == true, "should match JSON content type")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("check content type with charset", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/test", nil)
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		recorder := httptest.NewRecorder()
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
			local req = http.request()
			local is_json = req:is_content_type(http.CONTENT.JSON)
			assert(is_json == true, "should match JSON content type with charset")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("check accepts header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Accept", "application/json, text/html")
		recorder := httptest.NewRecorder()
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
			local req = http.request()
			local accepts_json = req:accepts(http.CONTENT.JSON)
			assert(accepts_json == true, "should accept JSON")
			local accepts_text = req:accepts(http.CONTENT.TEXT)
			assert(accepts_text == false, "should not accept plain text")
		`, "test")
		assert.NoError(t, err)
	})
}

func TestRequest_Headers(t *testing.T) {
	logger := zap.NewNop()

	t.Run("get existing header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-Custom", "test-value")
		recorder := httptest.NewRecorder()
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
			local req = http.request()
			local value = req:header("X-Custom")
			assert(value == "test-value", "incorrect header value")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("get non-existent header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		recorder := httptest.NewRecorder()
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
			local req = http.request()
			local value = req:header("X-NonExistent")
			assert(value == nil, "non-existent header should return nil")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("get content type", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
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
			local req = http.request()
			local ctype = req:content_type()
			assert(ctype == "application/json", "incorrect content type")
		`, "test")
		assert.NoError(t, err)
	})
}

func TestRequest_BodyReadingErrors(t *testing.T) {
	logger := zap.NewNop()

	t.Run("timeout during body read", func(t *testing.T) {
		// Spawn a slow reader that will cause a timeout
		slowReader := &delayReader{
			delay: 2 * time.Second, // Longer than the timeout
			data:  []byte("Hello, World!"),
		}

		req := httptest.NewRequest("POST", "/test", slowReader)
		recorder := httptest.NewRecorder()
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
			local req = http.request({timeout = 1000}) -- 1 second timeout
			local body, err = req:body()
			assert(body == nil, "body should be nil on timeout")
			assert(string.find(err, "timeout") ~= nil, "error should contain timeout message")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("max_body exceeded during body read", func(t *testing.T) {
		body := strings.NewReader("This is a long body")
		req := httptest.NewRequest("POST", "/test", body)
		recorder := httptest.NewRecorder()
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
			local req = http.request({max_body = 5}) -- Max body size of 5 bytes
			local body, err = req:body()
			assert(body == nil, "body should be nil when max_body is exceeded")
			assert(string.find(err, "too large") ~= nil, "error should indicate body too large")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("timeout during body_json read", func(t *testing.T) {
		// Spawn a slow reader
		slowReader := &delayReader{
			delay: 2 * time.Second,
			data:  []byte(`{"message": "Hello"}`),
		}

		req := httptest.NewRequest("POST", "/test", slowReader)
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
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
			local req = http.request({timeout = 1000}) -- 1 second timeout
			local data, err = req:body_json()
			assert(data == nil, "data should be nil on timeout")
			assert(string.find(err, "timeout") ~= nil, "error should contain timeout message")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("max_body exceeded during body_json read", func(t *testing.T) {
		body := strings.NewReader(`{"message": "This is a long body"}`)
		req := httptest.NewRequest("POST", "/test", body)
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		fc := ctxapi.FrameFromContext(ctx)
		_ = fc.Set(http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(
			logger,
			engine.WithLoader(mod.Name(), mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local http = require("http")
			local req = http.request({max_body = 10}) -- Max body size of 10 bytes
			local data, err = req:body_json()
			assert(data == nil, "data should be nil when max_body is exceeded")
			assert(string.find(err, "too large") ~= nil, "error should indicate body too large")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("invalid JSON in body_json", func(t *testing.T) {
		body := strings.NewReader(`{"message": "Hello", }`) // Trailing comma is invalid JSON
		req := httptest.NewRequest("POST", "/test", body)
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
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
			local req = http.request()
			local data, err = req:body_json()
			assert(data == nil, "data should be nil on invalid JSON")
			assert(string.find(err, "invalid JSON") ~= nil, "error should indicate invalid JSON")
		`, "test")
		assert.NoError(t, err)
	})
}

func TestRequest_InvalidInput(t *testing.T) {
	logger := zap.NewNop()

	t.Run("invalid header key in header()", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		recorder := httptest.NewRecorder()
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
			local req = http.request()
			local value, err = req:header(nil)
		`, "test")
		assert.Error(t, err)
	})

	t.Run("invalid content type in is_content_type()", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		recorder := httptest.NewRecorder()
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
			local req = http.request()
			local value, err = req:is_content_type(nil)
		`, "test")
		assert.Error(t, err)
	})

	t.Run("invalid content type in accepts()", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		recorder := httptest.NewRecorder()
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
			local req = http.request()
			local value, err = req:accepts(nil)
		`, "test")
		assert.Error(t, err)
	})
}

func TestRequest_EdgeCases(t *testing.T) {
	logger := zap.NewNop()

	t.Run("empty body", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/test", nil)
		recorder := httptest.NewRecorder()
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
			local req = http.request()
			local body, err = req:body()

			assert(body == "", "body should be '' for empty body")
			assert(err == nil, "no errors for no body")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("no content-type header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		recorder := httptest.NewRecorder()
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
			local req = http.request()
			local is_json = req:is_content_type("application/json")
			assert(is_json == false, "is_content_type should return false when no content-type header")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("multiple query parameters with the same key", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test?key=value1&key=value2", nil)
		recorder := httptest.NewRecorder()
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
			local req = http.request()
			local value = req:query("key")
			assert(value == "value1", "query should return the first value for multiple keys")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("multiple headers with the same key", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Add("X-Custom", "value1")
		req.Header.Add("X-Custom", "value2")
		recorder := httptest.NewRecorder()
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
			local req = http.request()
			local value = req:header("X-Custom")

			-- net/http returns the values joined by a comma and space
			assert(value == "value1, value2", "header should return all values for multiple headers with the same key")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("special characters in query parameters and headers", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test?name=John%20Doe&city=San%20Francisco", nil)
		req.Header.Set("X-Special", "value!@#$%^&*()")
		recorder := httptest.NewRecorder()
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
			local req = http.request()
			local name = req:query("name")
			local city = req:query("city")
			local special = req:header("X-Special")
			assert(name == "John Doe", "query should handle URL-encoded characters")
			assert(city == "San Francisco", "query should handle URL-encoded characters")
			assert(special == "value!@#$%^&*()", "header should handle special characters")
		`, "test")
		assert.NoError(t, err)
	})
}

func TestRequest_ContextCancellation(t *testing.T) {
	logger := zap.NewNop()

	t.Run("cancellation of parent context during body read", func(t *testing.T) {
		// Spawn a slow reader that will be canceled
		slowReader := &delayReader{
			delay: 1 * time.Second, // Longer than cancellation timeout
			data:  []byte("Hello, World! This will be canceled."),
		}

		req := httptest.NewRequest("POST", "/test", slowReader)
		recorder := httptest.NewRecorder()
		reqCtx := http.NewRequestContext(req, recorder)

		// Spawn a parent context with a short timeout
		parentCtx, cancel := context.WithTimeout(ctxapi.NewRootContext(), 500*time.Millisecond)
		defer cancel()

		// Attach the parent context to the request context via FrameContext
		ctx, _ := ctxapi.OpenFrameContext(parentCtx)
		fc := ctxapi.FrameFromContext(ctx)
		_ = fc.Set(http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local http = require("http")
			local req = http.request({timeout = 2000}) -- Request timeout longer than parent context timeout
			local body, err = req:body()
			assert(body == nil, "body should be nil on cancellation")
			assert(string.find(err, "context canceled") ~= nil, "error should contain context canceled message")
		`, "test")
		assert.Error(t, err)
		assert.ErrorContains(t, err, "context deadline exceeded")
	})
}

func TestRequest_RemoteAddr(t *testing.T) {
	logger := zap.NewNop()

	t.Run("get remote address", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		recorder := httptest.NewRecorder()
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
			local req = http.request()
			local addr = req:remote_addr()
			assert(addr == "192.168.1.1:12345", "incorrect remote address")
		`, "test")
		assert.NoError(t, err)
	})
}

func TestRequest_ContextErrors(t *testing.T) {
	logger := zap.NewNop()

	t.Run("no HTTP request context found", func(t *testing.T) {
		ctx := ctxapi.NewRootContext() // Context without HTTP request

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local http = require("http")
			local req, err = http.request()
			assert(req == nil, "request should be nil when no HTTP request context is found")
			assert(err == "no HTTP request context found", "incorrect error message")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("invalid HTTP request context type", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		fc := ctxapi.FrameFromContext(ctx)
		_ = fc.Set(http.RequestCtx, "invalid")

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local http = require("http")
			local req, err = http.request()
			assert(req == nil, "request should be nil when invalid HTTP request context type")
			assert(err == "no HTTP request context found", "incorrect error message")
		`, "test")
		assert.NoError(t, err)
	})
}

func TestRequest_ToString(t *testing.T) {
	logger := zap.NewNop()

	t.Run("convert request to string", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/resource/123", nil)
		recorder := httptest.NewRecorder()
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
			local req = http.request()
			local str = tostring(req)
			assert(str == "http.Request{method=PUT, path=/resource/123}", "incorrect string representation")
		`, "test")
		assert.NoError(t, err)
	})
}

func TestRequest_JSONWithEmptyBody(t *testing.T) {
	logger := zap.NewNop()

	t.Run("parse empty body as JSON", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/test", nil)
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		reqCtx := http.NewRequestContext(req, recorder)
		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		fc := ctxapi.FrameFromContext(ctx)
		_ = fc.Set(http.RequestCtx, reqCtx)

		mod := NewHTTPAPIModule(logger)
		vm, err := engine.NewVM(
			logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("print", lprint),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local http = require("http")
			local req = http.request()
			local data, err = req:body_json()
			assert(data == nil, "data should be nil for empty JSON body")
			assert(err ~= nil, "expected error")
		`, "test")
		assert.NoError(t, err)
	})
}

func TestRequest_Multipart(t *testing.T) {
	logger := zap.NewNop()

	t.Run("request multipart", func(t *testing.T) {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)

		// Add form field to test form values (which will be ignored by our implementation)
		err := mw.WriteField("field1", "value1")
		assert.NoError(t, err)

		// Add file parts
		part1, err := mw.CreateFormFile("file", "test1.txt")
		assert.NoError(t, err)
		_, err = io.Copy(part1, strings.NewReader("Hello World!"))
		assert.NoError(t, err)

		part2, err := mw.CreateFormFile("file", "test2.txt")
		assert.NoError(t, err)
		_, err = io.Copy(part2, strings.NewReader("Another file content"))
		assert.NoError(t, err)

		// Add a file with a different field name
		otherPart, err := mw.CreateFormFile("other_file", "other.txt")
		assert.NoError(t, err)
		_, err = io.Copy(otherPart, strings.NewReader("Other file content"))
		assert.NoError(t, err)

		err = mw.Close()
		assert.NoError(t, err)

		// Create request with the prepared body
		req := httptest.NewRequest("POST", "/upload", &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())

		recorder := httptest.NewRecorder()
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
          
          -- Get the request object
          local req = http.request()
          
          -- Parse the multipart form
          local form, err = req:parse_multipart()
          assert(err == nil, "Failed to parse multipart form: " .. (err or ""))
          assert(form ~= nil, "Expected form to be non-nil")
          
          -- Form values should not be present
          assert(form.values == nil, "Expected form values to be absent, but got: " .. tostring(form.values))
          
          -- Test files
          assert(form.files ~= nil, "Expected form files to be present")
          assert(form.files.file ~= nil, "Expected file field to be present")
          
          local fileCount = #form.files.file
          assert(fileCount == 2, "Expected 2 files in 'file' field, but got: " .. fileCount)
          
          -- Test first file
          local file1 = form.files.file[1]
          local file1Name = file1:name()
          assert(file1Name == "test1.txt", "Expected file1 name to be test1.txt, but got: " .. file1Name)
          
          local file1Size = file1:size()
          assert(file1Size == 12, "Expected file1 size to be 12, but got: " .. file1Size)
          
          -- Test streaming
          local stream1, err1 = file1:stream()
          assert(err1 == nil, "Failed to create stream for file1: " .. (err1 or ""))
          
          local content1 = stream1:read()
          assert(content1 == "Hello World!", "Expected file1 content to be 'Hello World!', but got: '" .. content1 .. "'")
          
          -- Test second file
          local file2 = form.files.file[2]
          local file2Name = file2:name()
          assert(file2Name == "test2.txt", "Expected file2 name to be test2.txt, but got: " .. file2Name)
          
          local file2Size = file2:size()
          assert(file2Size == 20, "Expected file2 size to be 20, but got: " .. file2Size)
          
          local stream2, err2 = file2:stream()
          assert(err2 == nil, "Failed to create stream for file2: " .. (err2 or ""))
          
          local content2 = stream2:read()
          assert(content2 == "Another file content", "Expected file2 content to be 'Another file content', but got: '" .. content2 .. "'")
          
          -- Test other file field
          assert(form.files.other_file ~= nil, "Expected other_file field to be present")
          
          local otherFileCount = #form.files.other_file
          assert(otherFileCount == 1, "Expected 1 file in 'other_file' field, but got: " .. otherFileCount)
          
          local otherFile = form.files.other_file[1]
          local otherFileName = otherFile:name()
          assert(otherFileName == "other.txt", "Expected other file name to be other.txt, but got: " .. otherFileName)
          
          local otherFileSize = otherFile:size()
          assert(otherFileSize == 18, "Expected other file size to be 18, but got: " .. otherFileSize)
          
          local otherStream, otherErr = otherFile:stream()
          assert(otherErr == nil, "Failed to create stream for other file: " .. (otherErr or ""))
          
          local otherContent = otherStream:read()
          assert(otherContent == "Other file content", "Expected other file content to be 'Other file content', but got: '" .. otherContent .. "'")
          
          -- Test partial streaming
          local stream3, err3 = file1:stream()
          assert(err3 == nil, "Failed to create another stream for file1: " .. (err3 or ""))
          
          local partial = stream3:read(5)
          assert(partial == "Hello", "Expected partial content to be 'Hello', but got: '" .. partial .. "'")
       `, "test")

		assert.NoError(t, err, "Lua script execution failed")
	})

	t.Run("request multipart with custom max memory", func(t *testing.T) {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)

		// Create a larger file
		part, err := mw.CreateFormFile("large_file", "large.txt")
		assert.NoError(t, err)

		// Create large content (1MB)
		largeContent := make([]byte, 1024*1024)
		for i := range largeContent {
			largeContent[i] = byte(i % 256)
		}

		_, err = part.Write(largeContent)
		assert.NoError(t, err)

		err = mw.Close()
		assert.NoError(t, err)

		// Create request with the prepared body
		req := httptest.NewRequest("POST", "/upload", &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())

		recorder := httptest.NewRecorder()
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
			local req = http.request()
			
			-- Parse multipart form with custom max memory (2MB)
			local form, err = req:parse_multipart(2 * 1024 * 1024)
			assert(err == nil, "Failed to parse multipart form: " .. (err or ""))
			assert(form ~= nil, "Expected form to be non-nil")
			
			-- Form values should not be present
			assert(form.values == nil, "Expected form values to be absent, but got: " .. tostring(form.values))
			
			-- Verify large file
			assert(form.files.large_file ~= nil, "Expected large_file field to be present")
			
			local largeFile = form.files.large_file[1]
			local largeFileSize = largeFile:size()
			assert(largeFileSize == 1024 * 1024, "Expected large file size to be 1MB, but got: " .. largeFileSize)
		`, "test")

		assert.NoError(t, err, "Lua script execution failed")
	})

	t.Run("request non-multipart content type", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/upload", strings.NewReader("plain text"))
		req.Header.Set("Content-Type", "text/plain")

		recorder := httptest.NewRecorder()
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
			local req = http.request()
			
			local form, err = req:parse_multipart()
			assert(form == nil, "Expected form to be nil for non-multipart content")
			assert(err == "content type is not multipart/form-data", "Expected error to be 'content type is not multipart/form-data', but got: '" .. (err or "") .. "'")
		`, "test")

		assert.NoError(t, err, "Lua script execution failed")
	})
}
