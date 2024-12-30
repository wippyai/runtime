package httpctx

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ponyruntime/go-lua"
	"github.com/ponyruntime/pony/api/service/http"
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

	// Get timeout option
	timeoutLV := l.GetField(opts, "timeout")
	if timeout, ok := timeoutLV.(lua.LNumber); ok {
		cfg.Timeout = int64(timeout)
	}

	// Get max_body option
	maxBodyLV := l.GetField(opts, "max_body")
	if maxBody, ok := maxBodyLV.(lua.LNumber); ok {
		cfg.MaxBody = int64(maxBody)
	}

	return cfg
}

// Declare request methods map
var requestMethods = map[string]lua.LGFunction{
	"method":         requestMethod,
	"path":           requestPath,
	"query":          requestQuery,
	"header":         requestHeader,
	"content_type":   requestContentType,
	"content_length": requestContentLength,
	"host":           requestHost,
	"remote_addr":    requestRemoteAddr,
	"body":           requestBody,
	"body_json":      requestBodyJSON,
	"has_body":       requestHasBody,
	"accepts":        requestAccepts,
}

// requestMethod returns the HTTP method
func requestMethod(l *lua.LState) int {
	req, err := checkRequest(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	l.Push(lua.LString(req.request.Method))
	return 1
}

// requestPath returns the request path
func requestPath(l *lua.LState) int {
	req, err := checkRequest(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	l.Push(lua.LString(req.request.URL.Path))
	return 1
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
		l.ArgError(2, "query key cannot be empty")
		return 0
	}

	values := req.request.URL.Query()[key]
	if len(values) == 0 {
		l.Push(lua.LNil)
		return 1
	}

	l.Push(lua.LString(values[0]))
	return 1
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
		l.ArgError(2, "header key cannot be empty")
		return 0
	}

	value := req.request.Header.Get(key)
	if value == "" {
		l.Push(lua.LNil)
		return 1
	}

	l.Push(lua.LString(value))
	return 1
}

// requestContentType returns the Content-Type header
func requestContentType(l *lua.LState) int {
	req, err := checkRequest(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	contentType := req.request.Header.Get("Content-Type")
	if contentType == "" {
		l.Push(lua.LNil)
		return 1
	}

	l.Push(lua.LString(contentType))
	return 1
}

// requestContentLength returns the Content-Length as number
func requestContentLength(l *lua.LState) int {
	req, err := checkRequest(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	l.Push(lua.LNumber(req.request.ContentLength))
	return 1
}

// requestHost returns the request host
func requestHost(l *lua.LState) int {
	req, err := checkRequest(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	l.Push(lua.LString(req.request.Host))
	return 1
}

// requestRemoteAddr returns the remote address
func requestRemoteAddr(l *lua.LState) int {
	req, err := checkRequest(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	l.Push(lua.LString(req.request.RemoteAddr))
	return 1
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

	// Check max body size if configured
	if req.config.MaxBody > 0 && req.request.ContentLength > req.config.MaxBody {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("body size %d exceeds maximum allowed size %d",
			req.request.ContentLength, req.config.MaxBody)))
		return 2
	}

	// If timeout is set, use a context with timeout
	var body []byte
	var readErr error

	if req.config.Timeout > 0 {
		ctx, cancel := context.WithTimeout(req.request.Context(),
			time.Duration(req.config.Timeout)*time.Millisecond)
		defer cancel()

		// Create a channel for the read operation
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

		// Wait for either completion or timeout
		select {
		case body = <-bodyChan:
		case readErr = <-errChan:
		case <-ctx.Done():
			readErr = fmt.Errorf("request timeout after %dms", req.config.Timeout)
		}
	} else {
		body, readErr = io.ReadAll(req.request.Body)
	}

	defer req.request.Body.Close()

	if readErr != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to read body: %v", readErr)))
		return 2
	}

	l.Push(lua.LString(string(body)))
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

	body, readErr := io.ReadAll(req.request.Body)
	defer req.request.Body.Close()

	if readErr != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to read body: %v", readErr)))
		return 2
	}

	// Validate JSON
	var js json.RawMessage
	if jsonErr := json.Unmarshal(body, &js); jsonErr != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("invalid JSON: %v", jsonErr)))
		return 2
	}

	l.Push(lua.LString(string(body)))
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
	return 1
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
		l.ArgError(2, "content type cannot be empty")
		return 0
	}

	accept := req.request.Header.Get("Accept")
	if accept == "" {
		l.Push(lua.LBool(false))
		return 1
	}

	// Check if the Accept header contains the content type
	accepts := strings.Split(accept, ",")
	for _, a := range accepts {
		a = strings.TrimSpace(a)
		if a == contentType || a == "*/*" {
			l.Push(lua.LBool(true))
			return 1
		}
	}

	l.Push(lua.LBool(false))
	return 1
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
		l.ArgError(1, "no context available")
		return 0
	}

	// Get HTTP request context
	val := ctx.Value(http.RequestCtx)
	if val == nil {
		l.ArgError(1, "no HTTP request context found")
		return 0
	}

	reqCtx, ok := val.(*http.RequestContext)
	if !ok {
		l.ArgError(1, "invalid HTTP request context type")
		return 0
	}

	// Create request userdata with config
	ud := l.NewUserData()
	ud.Value = &Request{
		request: reqCtx.Request(),
		config:  cfg,
	}

	l.SetMetatable(ud, l.GetTypeMetatable("Request"))
	l.Push(ud)
	return 1
}

// registerRequest registers the Request type and its methods
func registerRequest(l *lua.LState, mod *lua.LTable) {
	mt := l.NewTypeMetatable("Request")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), requestMethods))
	l.SetField(mt, "__tostring", l.NewFunction(requestToString))

	// Register constructor
	l.SetField(mod, "request", l.NewFunction(newRequest))
}
