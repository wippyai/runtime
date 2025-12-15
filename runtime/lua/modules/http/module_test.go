package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/runtime/resource"
	httpservice "github.com/wippyai/runtime/api/service/http"
	streammod "github.com/wippyai/runtime/runtime/lua/modules/stream"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

func newTestContext() (context.Context, ctxapi.FrameContext) {
	ctx := ctxapi.NewRootContext()
	ctx, fc := ctxapi.OpenFrameContext(ctx)
	return ctx, fc
}

func bind(l *lua.LState) {
	mod, _ := Module.Build()
	l.SetGlobal("http", mod)
}

func TestConstants(t *testing.T) {
	t.Run("METHOD constants", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		err := l.DoString(`
			assert(http.METHOD.GET == "GET", "incorrect GET method")
			assert(http.METHOD.POST == "POST", "incorrect POST method")
			assert(http.METHOD.PUT == "PUT", "incorrect PUT method")
			assert(http.METHOD.DELETE == "DELETE", "incorrect DELETE method")
			assert(http.METHOD.PATCH == "PATCH", "incorrect PATCH method")
			assert(http.METHOD.HEAD == "HEAD", "incorrect HEAD method")
			assert(http.METHOD.OPTIONS == "OPTIONS", "incorrect OPTIONS method")
		`)
		assert.NoError(t, err)
	})

	t.Run("STATUS constants", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		err := l.DoString(`
			assert(http.STATUS.OK == 200, "incorrect OK status")
			assert(http.STATUS.CREATED == 201, "incorrect CREATED status")
			assert(http.STATUS.ACCEPTED == 202, "incorrect ACCEPTED status")
			assert(http.STATUS.NO_CONTENT == 204, "incorrect NO_CONTENT status")
			assert(http.STATUS.PARTIAL_CONTENT == 206, "incorrect PARTIAL_CONTENT status")
			assert(http.STATUS.MOVED_PERMANENTLY == 301, "incorrect MOVED_PERMANENTLY status")
			assert(http.STATUS.FOUND == 302, "incorrect FOUND status")
			assert(http.STATUS.BAD_REQUEST == 400, "incorrect BAD_REQUEST status")
			assert(http.STATUS.UNAUTHORIZED == 401, "incorrect UNAUTHORIZED status")
			assert(http.STATUS.FORBIDDEN == 403, "incorrect FORBIDDEN status")
			assert(http.STATUS.NOT_FOUND == 404, "incorrect NOT_FOUND status")
			assert(http.STATUS.INTERNAL_ERROR == 500, "incorrect INTERNAL_ERROR status")
		`)
		assert.NoError(t, err)
	})

	t.Run("CONTENT constants", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		err := l.DoString(`
			assert(http.CONTENT.JSON == "application/json", "incorrect JSON content type")
			assert(http.CONTENT.FORM == "application/x-www-form-urlencoded", "incorrect FORM content type")
			assert(http.CONTENT.MULTIPART == "multipart/form-data", "incorrect MULTIPART content type")
			assert(http.CONTENT.TEXT == "text/plain", "incorrect TEXT content type")
			assert(http.CONTENT.STREAM == "application/octet-stream", "incorrect STREAM content type")
		`)
		assert.NoError(t, err)
	})

	t.Run("TRANSFER constants", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		err := l.DoString(`
			assert(http.TRANSFER.CHUNKED == "chunked", "incorrect CHUNKED transfer type")
			assert(http.TRANSFER.SSE == "sse", "incorrect SSE transfer type")
		`)
		assert.NoError(t, err)
	})

	t.Run("ERROR constants", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		err := l.DoString(`
			assert(http.ERROR.PARSE_FAILED == "PARSE_FAILED", "incorrect PARSE_FAILED error")
			assert(http.ERROR.INVALID_STATE == "INVALID_STATE", "incorrect INVALID_STATE error")
			assert(http.ERROR.WRITE_FAILED == "WRITE_FAILED", "incorrect WRITE_FAILED error")
			assert(http.ERROR.STREAM_ERROR == "STREAM_ERROR", "incorrect STREAM_ERROR error")
		`)
		assert.NoError(t, err)
	})
}

func TestRequest_Basic(t *testing.T) {
	t.Run("get request without context returns error", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, _ := newTestContext()
		l.SetContext(ctx)

		err := l.DoString(`
			local req, err = http.request()
			assert(req == nil, "request should be nil without HTTP context")
			assert(err ~= nil, "should return error without HTTP context")
		`)
		assert.NoError(t, err)
	})

	t.Run("get request method", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		req := httptest.NewRequest("POST", "/test", nil)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local req, err = http.request()
			assert(err == nil, "should not error")
			assert(req ~= nil, "request should not be nil")
			local method = req:method()
			assert(method == "POST", "method should be POST, got: " .. tostring(method))
		`)
		assert.NoError(t, err)
	})

	t.Run("get request path", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		req := httptest.NewRequest("GET", "/api/users/123", nil)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local req = http.request()
			local path = req:path()
			assert(path == "/api/users/123", "path should be /api/users/123, got: " .. tostring(path))
		`)
		assert.NoError(t, err)
	})

	t.Run("get query parameter", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		req := httptest.NewRequest("GET", "/test?name=john&age=30", nil)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local req = http.request()
			assert(req:query("name") == "john", "query name should be john")
			assert(req:query("age") == "30", "query age should be 30")
			assert(req:query("missing") == nil, "missing query should be nil")
		`)
		assert.NoError(t, err)
	})

	t.Run("get all query parameters", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		req := httptest.NewRequest("GET", "/test?a=1&b=2&c=3", nil)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local req = http.request()
			local params = req:query_params()
			assert(params.a == "1", "param a should be 1")
			assert(params.b == "2", "param b should be 2")
			assert(params.c == "3", "param c should be 3")
		`)
		assert.NoError(t, err)
	})

	t.Run("get request header", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-Custom-Header", "custom-value")
		req.Header.Set("Authorization", "Bearer token123")
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local req = http.request()
			assert(req:header("X-Custom-Header") == "custom-value", "header should be custom-value")
			assert(req:header("Authorization") == "Bearer token123", "auth header should match")
			assert(req:header("Non-Existent") == nil, "missing header should be nil")
		`)
		assert.NoError(t, err)
	})

	t.Run("get content type and length", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		body := strings.NewReader(`{"test": true}`)
		req := httptest.NewRequest("POST", "/test", body)
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local req = http.request()
			assert(req:content_type() == "application/json", "content type should be application/json")
			assert(req:content_length() == 14, "content length should be 14")
		`)
		assert.NoError(t, err)
	})

	t.Run("get host", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		req := httptest.NewRequest("GET", "/test", nil)
		req.Host = "example.com"
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local req = http.request()
			assert(req:host() == "example.com", "host should be example.com")
		`)
		assert.NoError(t, err)
	})

	t.Run("has_body check", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		body := strings.NewReader("test body")
		req := httptest.NewRequest("POST", "/test", body)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local req = http.request()
			assert(req:has_body() == true, "should have body")
		`)
		assert.NoError(t, err)
	})

	t.Run("accepts check", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Accept", "application/json, text/html")
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local req = http.request()
			assert(req:accepts("application/json") == true, "should accept json")
			assert(req:accepts("text/html") == true, "should accept html")
			assert(req:accepts("application/xml") == false, "should not accept xml")
		`)
		assert.NoError(t, err)
	})

	t.Run("is_content_type check", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		req := httptest.NewRequest("POST", "/test", nil)
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local req = http.request()
			assert(req:is_content_type("application/json") == true, "should match json")
			assert(req:is_content_type("text/plain") == false, "should not match text")
		`)
		assert.NoError(t, err)
	})
}

func TestRequest_Body(t *testing.T) {
	t.Run("read body as string", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		body := strings.NewReader("Hello, World!")
		req := httptest.NewRequest("POST", "/test", body)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local req = http.request()
			local body, err = req:body()
			assert(err == nil, "should not error")
			assert(body == "Hello, World!", "body should match")
		`)
		assert.NoError(t, err)
	})

	t.Run("read body as JSON", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		body := strings.NewReader(`{"name":"test","value":42}`)
		req := httptest.NewRequest("POST", "/test", body)
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local req = http.request()
			local data, err = req:body_json()
			assert(err == nil, "should not error")
			assert(data.name == "test", "name should be test")
			assert(data.value == 42, "value should be 42")
		`)
		assert.NoError(t, err)
	})

	t.Run("body_json with invalid JSON", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		body := strings.NewReader("not valid json")
		req := httptest.NewRequest("POST", "/test", body)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local req = http.request()
			local data, err = req:body_json()
			assert(data == nil, "data should be nil for invalid JSON")
			assert(err ~= nil, "should return error for invalid JSON")
		`)
		assert.NoError(t, err)
	})
}

func bindWithStream(l *lua.LState) {
	httpMod, _ := Module.Build()
	l.SetGlobal("http", httpMod)
	streamMod, _ := streammod.Module.Build()
	l.SetGlobal("stream", streamMod)
}

func TestRequest_Stream(t *testing.T) {
	t.Run("get body as stream", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bindWithStream(l)

		ctx, fc := newTestContext()
		store := resource.NewStore()
		defer func() { _ = store.Close() }()
		_ = resource.SetStore(ctx, store)

		body := strings.NewReader("streaming body content")
		req := httptest.NewRequest("POST", "/test", body)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local req = http.request()
			local stream, err = req:stream()
			assert(err == nil, "should not error: " .. tostring(err))
			assert(stream ~= nil, "stream should not be nil")
		`)
		assert.NoError(t, err)
	})

	t.Run("stream without resource table returns error", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		body := strings.NewReader("test")
		req := httptest.NewRequest("POST", "/test", body)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local req = http.request()
			local stream, err = req:stream()
			assert(stream == nil, "stream should be nil without resource table")
			assert(err ~= nil, "should return error")
		`)
		assert.NoError(t, err)
	})
}

func TestRequest_Multipart(t *testing.T) {
	t.Run("parse multipart form", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bindWithStream(l)

		ctx, fc := newTestContext()
		store := resource.NewStore()
		defer func() { _ = store.Close() }()
		_ = resource.SetStore(ctx, store)

		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		_ = writer.WriteField("name", "test")
		_ = writer.WriteField("value", "123")
		fileWriter, _ := writer.CreateFormFile("file", "test.txt")
		_, _ = fileWriter.Write([]byte("file content"))
		_ = writer.Close()

		req := httptest.NewRequest("POST", "/upload", &buf)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local req = http.request()
			local form, err = req:parse_multipart()
			assert(err == nil, "should not error: " .. tostring(err))
			assert(form ~= nil, "form should not be nil")
			assert(form.values.name == "test", "name should be test")
			assert(form.values.value == "123", "value should be 123")
			assert(form.files.file ~= nil, "file should exist")
			assert(#form.files.file == 1, "should have one file")
			local file = form.files.file[1]
			assert(file:name() == "test.txt", "filename should be test.txt")
			assert(file:size() == 12, "file size should be 12")
		`)
		assert.NoError(t, err)
	})

	t.Run("parse multipart with wrong content type", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		req := httptest.NewRequest("POST", "/upload", strings.NewReader("test"))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local req = http.request()
			local form, err = req:parse_multipart()
			assert(form == nil, "form should be nil")
			assert(err ~= nil, "should return error")
		`)
		assert.NoError(t, err)
	})
}

func TestResponse_Basic(t *testing.T) {
	t.Run("create response", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		req := httptest.NewRequest("GET", "/test", nil)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local res, err = http.response()
			assert(err == nil, "should not error")
			assert(res ~= nil, "response should not be nil")
		`)
		assert.NoError(t, err)
	})

	t.Run("set status code", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		req := httptest.NewRequest("GET", "/test", nil)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local res = http.response()
			res:set_status(http.STATUS.CREATED)
		`)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusCreated, recorder.Code)
	})

	t.Run("set headers", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		req := httptest.NewRequest("GET", "/test", nil)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local res = http.response()
			res:set_header("X-Custom", "value")
			res:set_content_type(http.CONTENT.JSON)
		`)
		assert.NoError(t, err)
		assert.Equal(t, "value", recorder.Header().Get("X-Custom"))
		assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	})

	t.Run("write body", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		req := httptest.NewRequest("GET", "/test", nil)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local res = http.response()
			res:write("Hello, World!")
		`)
		assert.NoError(t, err)
		assert.Equal(t, "Hello, World!", recorder.Body.String())
	})

	t.Run("write JSON", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		req := httptest.NewRequest("GET", "/test", nil)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local res = http.response()
			res:write_json({message = "Hello", value = 42, nested = {a = 1, b = 2}})
		`)
		assert.NoError(t, err)
		assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))

		var result map[string]interface{}
		err = json.Unmarshal(recorder.Body.Bytes(), &result)
		assert.NoError(t, err)
		assert.Equal(t, "Hello", result["message"])
		assert.Equal(t, float64(42), result["value"])
	})

	t.Run("flush", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		req := httptest.NewRequest("GET", "/test", nil)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local res = http.response()
			res:write("chunk1")
			res:flush()
			res:write("chunk2")
			res:flush()
		`)
		assert.NoError(t, err)
		assert.Equal(t, "chunk1chunk2", recorder.Body.String())
	})
}

func TestResponse_SSE(t *testing.T) {
	t.Run("setup SSE mode", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		req := httptest.NewRequest("GET", "/test", nil)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local res = http.response()
			res:set_transfer(http.TRANSFER.SSE)
		`)
		assert.NoError(t, err)
		assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
		assert.Equal(t, "no-cache", recorder.Header().Get("Cache-Control"))
		assert.Equal(t, "keep-alive", recorder.Header().Get("Connection"))
	})

	t.Run("write SSE event", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		req := httptest.NewRequest("GET", "/test", nil)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local res = http.response()
			res:write_event({name = "update", data = {progress = 50}})
		`)
		assert.NoError(t, err)
		body := recorder.Body.String()
		assert.Contains(t, body, "event: update\n")
		assert.Contains(t, body, `"progress":50`)
	})

	t.Run("write multiple SSE events", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		req := httptest.NewRequest("GET", "/test", nil)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local res = http.response()
			res:write_event({name = "start", data = {msg = "Starting"}})
			res:write_event({name = "progress", data = {value = 50}})
			res:write_event({name = "end", data = {msg = "Done"}})
		`)
		assert.NoError(t, err)
		body := recorder.Body.String()
		assert.Contains(t, body, "event: start\n")
		assert.Contains(t, body, "event: progress\n")
		assert.Contains(t, body, "event: end\n")
	})
}

func TestResponse_Chunked(t *testing.T) {
	t.Run("setup chunked mode", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		req := httptest.NewRequest("GET", "/test", nil)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local res = http.response()
			res:set_transfer(http.TRANSFER.CHUNKED)
			res:write("chunk1")
			res:write("chunk2")
		`)
		assert.NoError(t, err)
		assert.Equal(t, "chunked", recorder.Header().Get("Transfer-Encoding"))
		assert.Equal(t, "chunk1chunk2", recorder.Body.String())
	})
}

func TestResponse_ErrorCases(t *testing.T) {
	t.Run("set header after write", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		req := httptest.NewRequest("GET", "/test", nil)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local res = http.response()
			res:write("test")
			local err = res:set_header("X-Test", "value")
			assert(err ~= nil, "should error when setting header after write")
		`)
		assert.NoError(t, err)
	})

	t.Run("set status after write", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		req := httptest.NewRequest("GET", "/test", nil)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local res = http.response()
			res:write("test")
			local err = res:set_status(http.STATUS.OK)
			assert(err ~= nil, "should error when setting status after write")
		`)
		assert.NoError(t, err)
	})

	t.Run("set content type after write", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		req := httptest.NewRequest("GET", "/test", nil)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local res = http.response()
			res:write("test")
			local err = res:set_content_type(http.CONTENT.JSON)
			assert(err ~= nil, "should error when setting content type after write")
		`)
		assert.NoError(t, err)
	})

	t.Run("set transfer mode after write", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		req := httptest.NewRequest("GET", "/test", nil)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local res = http.response()
			res:write("test")
			local err = res:set_transfer(http.TRANSFER.CHUNKED)
			assert(err ~= nil, "should error when setting transfer after write")
		`)
		assert.NoError(t, err)
	})

	t.Run("response without context", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, _ := newTestContext()
		l.SetContext(ctx)

		err := l.DoString(`
			local res, err = http.response()
			assert(res == nil, "response should be nil without HTTP context")
			assert(err ~= nil, "should return error without HTTP context")
		`)
		assert.NoError(t, err)
	})
}

func TestRequest_ToString(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bind(l)

	ctx, fc := newTestContext()
	req := httptest.NewRequest("POST", "/api/test", nil)
	recorder := httptest.NewRecorder()
	reqCtx := httpservice.NewRequestContext(req, recorder)
	_ = fc.Set(httpservice.RequestKey(), reqCtx)
	l.SetContext(ctx)

	err := l.DoString(`
		local req = http.request()
		local str = tostring(req)
		assert(str:find("POST") ~= nil, "should contain method")
		assert(str:find("/api/test") ~= nil, "should contain path")
	`)
	assert.NoError(t, err)
}

func TestResponse_ToString(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bind(l)

	ctx, fc := newTestContext()
	req := httptest.NewRequest("GET", "/test", nil)
	recorder := httptest.NewRecorder()
	reqCtx := httpservice.NewRequestContext(req, recorder)
	_ = fc.Set(httpservice.RequestKey(), reqCtx)
	l.SetContext(ctx)

	err := l.DoString(`
		local res = http.response()
		local str = tostring(res)
		assert(str:find("http.Response") ~= nil, "should contain type name")
	`)
	assert.NoError(t, err)
}

func TestRequest_RemoteAddr(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bind(l)

	ctx, fc := newTestContext()
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	recorder := httptest.NewRecorder()
	reqCtx := httpservice.NewRequestContext(req, recorder)
	_ = fc.Set(httpservice.RequestKey(), reqCtx)
	l.SetContext(ctx)

	err := l.DoString(`
		local req = http.request()
		local addr = req:remote_addr()
		assert(addr == "192.168.1.1:12345", "remote addr should match")
	`)
	assert.NoError(t, err)
}

func TestRequest_BodyNoBody(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bind(l)

	ctx, fc := newTestContext()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Body = nil
	recorder := httptest.NewRecorder()
	reqCtx := httpservice.NewRequestContext(req, recorder)
	_ = fc.Set(httpservice.RequestKey(), reqCtx)
	l.SetContext(ctx)

	err := l.DoString(`
		local req = http.request()
		local body, err = req:body()
		assert(body == nil, "body should be nil")
		assert(err ~= nil, "should return error")
	`)
	assert.NoError(t, err)
}

func TestRequest_AcceptsWildcard(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bind(l)

	ctx, fc := newTestContext()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept", "*/*")
	recorder := httptest.NewRecorder()
	reqCtx := httpservice.NewRequestContext(req, recorder)
	_ = fc.Set(httpservice.RequestKey(), reqCtx)
	l.SetContext(ctx)

	err := l.DoString(`
		local req = http.request()
		assert(req:accepts("application/json") == true, "should accept any type with wildcard")
		assert(req:accepts("text/html") == true, "should accept any type with wildcard")
	`)
	assert.NoError(t, err)
}

func TestRequest_AcceptsEmpty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bind(l)

	ctx, fc := newTestContext()
	req := httptest.NewRequest("GET", "/test", nil)
	recorder := httptest.NewRecorder()
	reqCtx := httpservice.NewRequestContext(req, recorder)
	_ = fc.Set(httpservice.RequestKey(), reqCtx)
	l.SetContext(ctx)

	err := l.DoString(`
		local req = http.request()
		assert(req:accepts("application/json") == false, "should not accept when no Accept header")
	`)
	assert.NoError(t, err)
}

func TestMultipartFile_Stream(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindWithStream(l)

	ctx, fc := newTestContext()
	store := resource.NewStore()
	defer func() { _ = store.Close() }()
	_ = resource.SetStore(ctx, store)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	fileWriter, _ := writer.CreateFormFile("file", "test.txt")
	_, _ = io.WriteString(fileWriter, "file content here")
	_ = writer.Close()

	req := httptest.NewRequest("POST", "/upload", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	reqCtx := httpservice.NewRequestContext(req, recorder)
	require.NoError(t, fc.Set(httpservice.RequestKey(), reqCtx))
	l.SetContext(ctx)

	err := l.DoString(`
		local req = http.request()
		local form = req:parse_multipart()
		local file = form.files.file[1]
		local stream, err = file:stream()
		assert(err == nil, "should not error: " .. tostring(err))
		assert(stream ~= nil, "stream should not be nil")
	`)
	assert.NoError(t, err)
}

func TestMultipartFile_Header(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindWithStream(l)

	ctx, fc := newTestContext()
	store := resource.NewStore()
	defer func() { _ = store.Close() }()
	_ = resource.SetStore(ctx, store)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="file"; filename="image.jpg"`)
	h.Set("Content-Type", "image/jpeg")
	fileWriter, _ := writer.CreatePart(h)
	_, _ = io.WriteString(fileWriter, "fake image data")
	_ = writer.Close()

	req := httptest.NewRequest("POST", "/upload", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	reqCtx := httpservice.NewRequestContext(req, recorder)
	require.NoError(t, fc.Set(httpservice.RequestKey(), reqCtx))
	l.SetContext(ctx)

	err := l.DoString(`
		local req = http.request()
		local form = req:parse_multipart()
		local file = form.files.file[1]

		local ct = file:header("Content-Type")
		assert(ct == "image/jpeg", "content type should be image/jpeg, got: " .. tostring(ct))

		local ct_lower = file:header("content-type")
		assert(ct_lower == "image/jpeg", "header lookup should be case-insensitive")

		local missing = file:header("X-Custom")
		assert(missing == nil, "missing header should be nil")
	`)
	assert.NoError(t, err)
}

func TestRequest_MaxBody(t *testing.T) {
	t.Run("max_body limit exceeded", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		bodyContent := "This is a test body that is too large"
		req, _ := http.NewRequestWithContext(context.Background(), "POST", "/test", strings.NewReader(bodyContent))
		req.ContentLength = int64(len(bodyContent))
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local req = http.request({max_body = 10})
			local body, err = req:body()
			assert(body == nil, "body should be nil when max_body exceeded")
			assert(err ~= nil, "should return error when max_body exceeded")
		`)
		assert.NoError(t, err)
	})

	t.Run("max_body limit not exceeded", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		body := strings.NewReader("Small")
		req := httptest.NewRequest("POST", "/test", body)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local req = http.request({max_body = 100})
			local body, err = req:body()
			assert(err == nil, "should not error when max_body not exceeded")
			assert(body == "Small", "body should match")
		`)
		assert.NoError(t, err)
	})

	t.Run("max_body limit exceeded for JSON", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		bodyContent := `{"key":"value that is very long"}`
		req, _ := http.NewRequestWithContext(context.Background(), "POST", "/test", strings.NewReader(bodyContent))
		req.ContentLength = int64(len(bodyContent))
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local req = http.request({max_body = 10})
			local data, err = req:body_json()
			assert(data == nil, "data should be nil when max_body exceeded")
			assert(err ~= nil, "should return error when max_body exceeded")
		`)
		assert.NoError(t, err)
	})
}

func TestRequest_Timeout(t *testing.T) {
	t.Run("timeout during body read", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		pr, pw := io.Pipe()
		req, _ := http.NewRequest("POST", "/test", pr)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		go func() {
			time.Sleep(200 * time.Millisecond)
			_, _ = pw.Write([]byte("delayed data"))
			_ = pw.Close()
		}()

		err := l.DoString(`
			local req = http.request({timeout = 50})
			local body, err = req:body()
			assert(body == nil, "body should be nil on timeout")
			assert(err ~= nil, "should return error on timeout")
		`)
		assert.NoError(t, err)
	})

	t.Run("no timeout when data arrives in time", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		body := strings.NewReader("Quick response")
		req := httptest.NewRequest("POST", "/test", body)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local req = http.request({timeout = 5000})
			local body, err = req:body()
			assert(err == nil, "should not error when timeout not exceeded")
			assert(body == "Quick response", "body should match")
		`)
		assert.NoError(t, err)
	})
}

func TestRequest_ConfigOptions(t *testing.T) {
	t.Run("request without options", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		body := strings.NewReader("Test body")
		req := httptest.NewRequest("POST", "/test", body)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local req = http.request()
			local body, err = req:body()
			assert(err == nil, "should not error without options")
			assert(body == "Test body", "body should match")
		`)
		assert.NoError(t, err)
	})

	t.Run("request with both timeout and max_body", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		body := strings.NewReader("Test")
		req := httptest.NewRequest("POST", "/test", body)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local req = http.request({timeout = 5000, max_body = 100})
			local body, err = req:body()
			assert(err == nil, "should not error with both options")
			assert(body == "Test", "body should match")
		`)
		assert.NoError(t, err)
	})
}

func TestModuleImmutability(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bind(l)

	err := l.DoString(`
		local success = pcall(function()
			http.foo = "bar"
		end)
	`)
	assert.NoError(t, err)
}

func TestRequestBodySizeLimit(t *testing.T) {
	t.Run("body exceeds configured max_body", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		largeBody := strings.Repeat("x", 1000)
		req := httptest.NewRequest("POST", "/test", strings.NewReader(largeBody))
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local req = http.request({max_body = 100})
			local body, err = req:body()
			assert(body == nil, "body should be nil when exceeding limit")
			assert(err ~= nil, "should return error when exceeding limit")
		`)
		assert.NoError(t, err)
	})

	t.Run("body within configured max_body", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		body := strings.NewReader("small body")
		req := httptest.NewRequest("POST", "/test", body)
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local req = http.request({max_body = 1000})
			local body, err = req:body()
			assert(err == nil, "should not error when within limit")
			assert(body == "small body", "body should match")
		`)
		assert.NoError(t, err)
	})

	t.Run("body_json exceeds configured max_body", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		largeJSON := `{"data": "` + strings.Repeat("x", 1000) + `"}`
		req := httptest.NewRequest("POST", "/test", strings.NewReader(largeJSON))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local req = http.request({max_body = 100})
			local body, err = req:body_json()
			assert(body == nil, "body should be nil when exceeding limit")
			assert(err ~= nil, "should return error when exceeding limit")
		`)
		assert.NoError(t, err)
	})

	t.Run("default limit prevents extremely large bodies", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		bind(l)

		ctx, fc := newTestContext()
		req := httptest.NewRequest("POST", "/test", nil)
		req.ContentLength = 200 * 1024 * 1024 // 200MB claimed
		recorder := httptest.NewRecorder()
		reqCtx := httpservice.NewRequestContext(req, recorder)
		_ = fc.Set(httpservice.RequestKey(), reqCtx)
		l.SetContext(ctx)

		err := l.DoString(`
			local req = http.request()
			local body, err = req:body()
			assert(body == nil, "body should be nil for oversized content-length")
			assert(err ~= nil, "should return error for oversized content-length")
		`)
		assert.NoError(t, err)
	})
}
