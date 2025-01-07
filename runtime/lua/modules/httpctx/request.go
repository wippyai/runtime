package httpctx

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/service/http"
	"github.com/ponyruntime/pony/internal/closer"
	"github.com/ponyruntime/pony/runtime/lua/modules/json"
	"github.com/ponyruntime/pony/runtime/lua/modules/stream"
	"github.com/yuin/gopher-lua"
	"io"
	basehttp "net/http"
	"strings"
	"time"
)

// Request represents a Lua userdata object wrapping http.Request
type Request struct {
	request *basehttp.Request
	config  RequestConfig
}

// RequestConfig holds initialization options for requests
type RequestConfig struct {
	Timeout int64 // request timeout in milliseconds
	MaxBody int64 // maximum body size in bytes
}

// checkRequest gets and verifies Request userdata from Lua state
func checkRequest(l *lua.LState, n int) (*Request, error) {
	ud := l.CheckUserData(n)
	if ud == nil {
		return nil, fmt.Errorf("argument %d must be a Request", n)
	}

	if req, ok := ud.Value.(*Request); ok {
		return req, nil
	}
	return nil, fmt.Errorf("argument %d must be a Request, got %T", n, ud.Value)
}

func parseRequestOptions(l *lua.LState, idx int) RequestConfig {
	cfg := RequestConfig{}

	if l.GetTop() < idx {
		return cfg
	}

	opts := l.CheckTable(idx)
	if opts == nil {
		return cfg
	}

	timeoutLV := l.GetField(opts, "timeout")
	if timeout, ok := timeoutLV.(lua.LNumber); ok {
		cfg.Timeout = int64(timeout)
	}

	maxBodyLV := l.GetField(opts, "max_body")
	if maxBody, ok := maxBodyLV.(lua.LNumber); ok {
		cfg.MaxBody = int64(maxBody)
	}

	return cfg
}

// Declare request methods map
var requestMethods = map[string]lua.LGFunction{
	"method":          requestMethod,
	"path":            requestPath,
	"query":           requestQuery,
	"header":          requestHeader,
	"content_type":    requestContentType,
	"content_length":  requestContentLength,
	"host":            requestHost,
	"remote_addr":     requestRemoteAddr,
	"body":            requestBody,
	"body_json":       requestBodyJSON,
	"has_body":        requestHasBody,
	"accepts":         requestAccepts,
	"is_content_type": requestIsContentType,
	"stream_body":     requestStreamBody,
}

// requestMethod returns the HTTP method
func requestMethod(l *lua.LState) int {
	req, err := checkRequest(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	l.Push(lua.LString(req.request.Method))
	l.Push(lua.LNil)
	return 2
}

// requestPath returns the request path
func requestPath(l *lua.LState) int {
	req, err := checkRequest(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	l.Push(lua.LString(req.request.URL.Path))
	l.Push(lua.LNil)
	return 2
}

// requestQuery returns query parameters
func requestQuery(l *lua.LState) int {
	req, err := checkRequest(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	key := l.CheckString(2)
	if key == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("query key cannot be empty"))
		return 2
	}

	values := req.request.URL.Query()[key]
	if len(values) == 0 {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		return 2
	}

	l.Push(lua.LString(values[0]))
	l.Push(lua.LNil)
	return 2
}

// requestHeader returns a request header value
func requestHeader(l *lua.LState) int {
	req, err := checkRequest(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	key := l.CheckString(2)
	if key == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("header key cannot be empty"))
		return 2
	}

	// Use req.request.Header to get all values for the key.
	values := req.request.Header[key]
	if len(values) == 0 {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		return 2
	}

	// Join the values with a comma and a space.
	value := strings.Join(values, ", ")

	l.Push(lua.LString(value))
	l.Push(lua.LNil)
	return 2
}

// requestContentType returns the Content-type header
func requestContentType(l *lua.LState) int {
	req, err := checkRequest(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	contentType := req.request.Header.Get("Content-Type")
	if contentType == "" {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		return 2
	}

	l.Push(lua.LString(contentType))
	l.Push(lua.LNil)
	return 2
}

// requestContentLength returns the Content-Length as number
func requestContentLength(l *lua.LState) int {
	req, err := checkRequest(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	l.Push(lua.LNumber(req.request.ContentLength))
	l.Push(lua.LNil)
	return 2
}

// requestIsContentType checks if request has specific content type
func requestIsContentType(l *lua.LState) int {
	req, err := checkRequest(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	expectedType := l.CheckString(2)
	if expectedType == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("content type cannot be empty"))
		return 2
	}

	actualType := req.request.Header.Get("Content-Type")
	isMatch := strings.HasPrefix(actualType, expectedType)

	l.Push(lua.LBool(isMatch))
	l.Push(lua.LNil)
	return 2
}

// requestHost returns the request host
func requestHost(l *lua.LState) int {
	req, err := checkRequest(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	l.Push(lua.LString(req.request.Host))
	l.Push(lua.LNil)
	return 2
}

// requestRemoteAddr returns the remote address
func requestRemoteAddr(l *lua.LState) int {
	req, err := checkRequest(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	l.Push(lua.LString(req.request.RemoteAddr))
	l.Push(lua.LNil)
	return 2
}

// requestBody returns the raw request body
func requestBody(l *lua.LState) int {
	req, err := checkRequest(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	if req.request.Body == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no body"))
		return 2
	}

	if req.config.MaxBody > 0 && req.request.ContentLength > req.config.MaxBody {
		l.Push(lua.LNil)
		l.Push(lua.LString("request body too large"))
		return 2
	}

	var body []byte
	var readErr error

	if req.config.Timeout > 0 {
		ctx, cancel := context.WithTimeout(
			req.request.Context(),
			time.Duration(req.config.Timeout)*time.Second,
		)
		defer cancel()

		bodyChan := make(chan []byte)
		errChan := make(chan error)

		go func() {
			b, err := io.ReadAll(req.request.Body)
			if err != nil {
				errChan <- err
				return
			}
			bodyChan <- b
		}()

		select {
		case body = <-bodyChan:
		case readErr = <-errChan:
		case <-ctx.Done():
			readErr = fmt.Errorf("request timeout after %dms", req.config.Timeout)
		}
	} else {
		body, readErr = io.ReadAll(req.request.Body)
	}

	defer func() { _ = req.request.Body.Close() }()

	if readErr != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to read body: %v", readErr)))
		return 2
	}

	l.Push(lua.LString(body))
	l.Push(lua.LNil)
	return 2
}

// requestStreamBody returns an iterator for streaming the request body
func requestStreamBody(l *lua.LState) int {
	req, err := checkRequest(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	if req.request.Body == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no body"))
		return 2
	}

	cleanup := closer.FromContext(req.request.Context())
	if cleanup == nil {
		ctx, c := closer.WithContext(req.request.Context())
		req.request = req.request.WithContext(ctx)
		cleanup = c
	}
	cleanup.Add(req.request.Body.Close)

	var bufferSize int64 = 32 * 1024 // Default 32KB buffer

	if l.GetTop() > 1 {
		opts := l.CheckTable(2)
		if opts != nil {
			if bs := opts.RawGetString("buffer_size"); !lua.LVIsFalse(bs) {
				if n, ok := bs.(lua.LNumber); ok {
					bufferSize = int64(n)
				}
			}
		}
	}

	s, err := stream.NewStream(req.request.Context(), req.request.Body, stream.NewStreamConfig(
		bufferSize,
	))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to create stream: %v", err)))
		return 2
	}

	luaStream := &stream.LuaStream{Stream: s}
	ud := l.NewUserData()
	ud.Value = luaStream

	l.SetMetatable(ud, l.GetTypeMetatable("Stream"))

	l.Push(ud)
	l.Call(0, 1)
	l.Push(lua.LNil)
	return 2
}

// requestBodyJSON returns the body parsed as JSON
func requestBodyJSON(l *lua.LState) int {
	req, err := checkRequest(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	if req.request.Body == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no body"))
		return 2
	}

	if req.config.MaxBody > 0 && req.request.ContentLength > req.config.MaxBody {
		l.Push(lua.LNil)
		l.Push(lua.LString("request body too large"))
		return 2
	}

	body, readErr := io.ReadAll(req.request.Body)
	defer req.request.Body.Close()

	if readErr != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to read body: %v", readErr)))
		return 2
	}

	// Parse JSON into Lua value
	luaValue, err := json.Decode(l, body)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("invalid JSON: %v", err)))
		return 2
	}

	l.Push(luaValue)
	l.Push(lua.LNil)
	return 2
}

// requestHasBody checks if request has a body
func requestHasBody(l *lua.LState) int {
	req, err := checkRequest(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	hasBody := req.request.Body != nil && req.request.ContentLength > 0
	l.Push(lua.LBool(hasBody))
	l.Push(lua.LNil)
	return 2
}

// requestAccepts checks if request accepts a content type
func requestAccepts(l *lua.LState) int {
	req, err := checkRequest(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	contentType := l.CheckString(2)
	if contentType == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("content type cannot be empty"))
		return 2
	}

	accept := req.request.Header.Get("Accept")
	if accept == "" {
		l.Push(lua.LBool(false))
		l.Push(lua.LNil)
		return 2
	}

	accepts := strings.Split(accept, ",")
	for _, a := range accepts {
		a = strings.TrimSpace(a)
		if a == contentType || a == "*/*" {
			l.Push(lua.LBool(true))
			l.Push(lua.LNil)
			return 2
		}
	}

	l.Push(lua.LBool(false))
	l.Push(lua.LNil)
	return 2
}

// requestToString implements the __tostring metamethod for Request
func requestToString(l *lua.LState) int {
	req, err := checkRequest(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	l.Push(lua.LString(fmt.Sprintf("http.Request{method=%s, path=%s}",
		req.request.Method, req.request.URL.Path)))
	return 1
}

// newRequest creates a new Request from the context
func newRequest(l *lua.LState) int {
	// Parse configuration if provided
	var cfg RequestConfig
	if l.GetTop() > 0 {
		cfg = parseRequestOptions(l, 1)
	}

	// Get HTTP context from Lua state context
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context available"))
		return 2
	}

	// Get HTTP request context
	val := ctx.Value(http.RequestCtx)
	if val == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no HTTP request context found"))
		return 2
	}

	reqCtx, ok := val.(*http.RequestContext)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("invalid HTTP request context type"))
		return 2
	}

	// Create request userdata with config
	ud := l.NewUserData()
	ud.Value = &Request{
		request: reqCtx.Request(),
		config:  cfg,
	}

	l.SetMetatable(ud, l.GetTypeMetatable("Request"))
	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}

// registerRequest registers the Request type and its methods
func registerRequest(l *lua.LState, mod *lua.LTable) {
	mt := l.NewTypeMetatable("Request")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), requestMethods))
	l.SetField(mt, "__tostring", l.NewFunction(requestToString))

	// Register constructor
	l.SetField(mod, "request", l.NewFunction(newRequest))
}
