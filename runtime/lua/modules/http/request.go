package http

import (
	"context"
	"fmt"
	"io"
	basehttp "net/http"
	"strings"
	"time"

	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/value"

	"github.com/wippyai/runtime/api/service/http"
	"github.com/wippyai/runtime/runtime/lua/modules/json"
	"github.com/wippyai/runtime/runtime/lua/modules/stream"
	lua "github.com/yuin/gopher-lua"
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
func checkRequest(l *lua.LState, n int) (*Request, error) { //nolint:unparam // ok for now
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

// requestQuery returns query parameters
func requestQueryAll(l *lua.LState) int {
	req, err := checkRequest(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	table := l.CreateTable(0, len(req.request.URL.Query()))
	for key, values := range req.request.URL.Query() {
		if len(values) == 0 {
			continue
		}

		table.RawSetString(key, lua.LString(strings.Join(values, ",")))
	}

	l.Push(table)
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

	l.Push(lua.LString(strings.Join(values, ", ")))
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
			select {
			case bodyChan <- b:
			case <-ctx.Done():
			}
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

// requestStream returns an iterator for streaming the request body
func requestStream(l *lua.LState) int {
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

	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no unit of work available"))
		return 2
	}

	req.request = req.request.WithContext(uw.Context())

	s, err := stream.NewStream(req.request.Context(), req.request.Body)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to create stream: %v", err)))
		return 2
	}

	// we expect that the stream is closed by the end of UoW, after all you can only use it functions
	// or user can close it directly
	luaStream := stream.NewLuaStream(uw, s, nil)

	ud := l.NewUserData()
	ud.Value = luaStream
	ud.Metatable = value.GetTypeMetatable(l, "Stream")

	l.Push(ud)
	return 1
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
	defer func() {
		err := req.request.Body.Close()
		//nolint:revive,staticcheck // ignore for now
		if err != nil {
			// suppressed for now
		}
	}()

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

	// Get HTTP request context from FrameContext
	reqCtx, ok := http.GetRequestContext(ctx)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("no HTTP request context found"))
		return 2
	}

	// Spawn request userdata with config
	ud := l.NewUserData()
	ud.Value = &Request{
		request: reqCtx.Request(),
		config:  cfg,
	}
	ud.Metatable = value.GetTypeMetatable(l, "Request")

	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}

// requestParseMultipart parses multipart form data from the request
func requestParseMultipart(l *lua.LState) int {
	req, err := checkRequest(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	// Check if the content type is multipart/form-data
	contentType := req.request.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "multipart/form-data") {
		l.Push(lua.LNil)
		l.Push(lua.LString("content type is not multipart/form-data"))
		return 2
	}

	// Parse multipart form with default max memory
	maxMemory := int64(32 << 20) // 32MB default
	if l.GetTop() > 1 {
		maxMemoryOpt := l.CheckInt64(2)
		if maxMemoryOpt > 0 {
			maxMemory = maxMemoryOpt
		}
	}

	if err := req.request.ParseMultipartForm(maxMemory); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to parse multipart form: %v", err)))
		return 2
	}

	// Create result table
	result := l.CreateTable(0, 1)

	// Add file objects
	files := l.CreateTable(0, len(req.request.MultipartForm.File))
	for key, fileHeaders := range req.request.MultipartForm.File {
		filesList := l.CreateTable(len(fileHeaders), 0)
		for i, fileHeader := range fileHeaders {
			// Create MultipartFile userdata
			ud := l.NewUserData()
			ud.Value = &MultipartFile{
				fileHeader: fileHeader,
				request:    req.request,
			}
			ud.Metatable = value.GetTypeMetatable(l, "MultipartFile")

			filesList.RawSetInt(i+1, ud)
		}
		files.RawSetString(key, filesList)
	}
	result.RawSetString("files", files)

	l.Push(result)
	l.Push(lua.LNil)
	return 2
}

// requestParam returns a route parameter value
func requestParam(l *lua.LState) int {
	req, err := checkRequest(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	paramName := l.CheckString(2)
	if paramName == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("parameter name cannot be empty"))
		return 2
	}

	// Get route info from FrameContext
	routeInfo, ok := http.GetRouteInfo(req.request.Context())
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("no route parameters found in request context"))
		return 2
	}

	// Get parameter value
	paramValue, exists := routeInfo.Params[paramName]
	if !exists {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		return 2
	}

	l.Push(lua.LString(paramValue))
	l.Push(lua.LNil)
	return 2
}

// requestParam returns a route parameter value
func requestParams(l *lua.LState) int {
	req, err := checkRequest(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	// Get route info from FrameContext
	routeInfo, ok := http.GetRouteInfo(req.request.Context())
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("no route parameters found in request context"))
		return 2
	}

	params := l.CreateTable(0, len(routeInfo.Params))
	for key, v := range routeInfo.Params {
		params.RawSetString(key, lua.LString(v))
	}

	l.Push(params)
	return 1
}
