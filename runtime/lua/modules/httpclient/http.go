package httpclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/security"

	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/modules/stream"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

var (
	// ErrInvalidAuth is returned when authentication credentials are missing or invalid
	ErrInvalidAuth = errors.New("auth table must contain non-nil user and pass fields")

	// ErrInvalidRequest is returned when the request is not a table, internal.
	ErrInvalidRequest = errors.New("request must be a table")
)

// Client interface abstracts the http.Client
type Client interface {
	Do(req *http.Request) (*http.Response, error)
}

// Module implements HTTP client functionality for Lua runtime
type Module struct {
	log         *zap.Logger
	client      Client
	clientPool  *clientPool
	moduleTable *lua.LTable
	once        sync.Once
}

// NewHTTPClientModule creates a new HTTP module instance with the given client and logger
func NewHTTPClientModule(log *zap.Logger, client Client) *Module {
	return &Module{
		log:        log,
		client:     client,
		clientPool: newClientPool(client),
	}
}

// Name returns the module name
func (m *Module) Name() string {
	return "http_client"
}

// Loader implements the module loader
func (m *Module) Loader(l *lua.LState) int {
	// Create module table once and cache it
	m.once.Do(func() {
		// Pre-allocate table with exact capacity needed
		mod := l.CreateTable(0, 10) // 10 functions will be added

		// Directly register functions instead of using SetFuncs
		mod.RawSetString("get", l.NewFunction(m.makeMethod("GET")))
		mod.RawSetString("post", l.NewFunction(m.makeMethod("POST")))
		mod.RawSetString("put", l.NewFunction(m.makeMethod("PUT")))
		mod.RawSetString("delete", l.NewFunction(m.makeMethod("DELETE")))
		mod.RawSetString("head", l.NewFunction(m.makeMethod("HEAD")))
		mod.RawSetString("patch", l.NewFunction(m.makeMethod("PATCH")))
		mod.RawSetString("request", l.NewFunction(m.request))
		mod.RawSetString("request_batch", l.NewFunction(m.requestBatch))
		mod.RawSetString("encode_uri", l.NewFunction(encodeURI))
		mod.RawSetString("decode_uri", l.NewFunction(decodeURI))

		// Make the table immutable so it can be safely reused
		mod.Immutable = true
		m.moduleTable = mod
	})

	// Register response type using value helper
	value.RegisterMetamethods(l, "http.response", map[string]lua.LGFunction{
		"__index": httpResponseIndex,
	})
	stream.RegisterStream(l)

	l.Push(m.moduleTable)
	return 1
}

// makeMethod creates handler for specific HTTP method
func (m *Module) makeMethod(method string) lua.LGFunction {
	return func(l *lua.LState) int {
		url, err := getURLFromArgs(l, 1)
		if err != nil {
			l.ArgError(1, err.Error())
			return 0
		}

		opts, err := getOptionsFromArgs(l, 2)
		if err != nil {
			l.ArgError(2, err.Error())
			return 0
		}

		// Check Unix socket security permission
		if opts.unixSocket != "" {
			if !security.IsAllowed(l.Context(), "http_client.unix_socket", opts.unixSocket, nil) {
				l.RaiseError("not allowed to connect to Unix socket: %s", opts.unixSocket)
				return 0
			}
		}

		if !security.IsAllowed(l.Context(), "http_client.request", url, nil) {
			l.RaiseError("not allowed to make request to: %s", url)
			return 0
		}

		req, err := makeRequest(method, url, opts)
		if err != nil {
			// Consider using a more generic error message here
			l.ArgError(1, err.Error())
			return 0
		}

		if engine.IsCoroutineVM(l) {
			return m.executeRequestYield(l, req, opts)
		}
		return m.executeRequest(l, req, opts)
	}
}

// request handles generic HTTP requests
func (m *Module) request(l *lua.LState) int {
	method, err := getMethodFromArgs(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	url, err := getURLFromArgs(l, 2)
	if err != nil {
		l.ArgError(2, err.Error())
		return 0
	}

	opts, err := getOptionsFromArgs(l, 3)
	if err != nil {
		l.ArgError(3, err.Error())
		return 0
	}

	// Check Unix socket security permission
	if opts.unixSocket != "" {
		if !security.IsAllowed(l.Context(), "http_client.unix_socket", opts.unixSocket, nil) {
			l.RaiseError("not allowed to connect to Unix socket: %s", opts.unixSocket)
			return 0
		}
	}

	if !security.IsAllowed(l.Context(), "http_client.request", url, nil) {
		l.RaiseError("not allowed to make request to: %s", url)
		return 0
	}

	req, err := makeRequest(method, url, opts)
	if err != nil {
		// Consider using a more generic error message here
		l.ArgError(1, err.Error())
		return 0
	}

	if engine.IsCoroutineVM(l) {
		return m.executeRequestYield(l, req, opts)
	}

	return m.executeRequest(l, req, opts)
}

// getClientForTimeout returns appropriate client from the pool
func (m *Module) getClientForTimeout(timeout time.Duration, unixSocket string) Client {
	return m.clientPool.GetClient(timeout, unixSocket)
}

// executeRequest performs the actual HTTP request
func (m *Module) executeRequest(l *lua.LState, req *http.Request, opts *requestOptions) int {
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.RaiseError("unit of work not found")
		return 0
	}

	var closer context.CancelFunc
	ctx := uw.Context()
	if opts.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.timeout)
		closer = uw.AddCleanup(func() error { cancel(); return nil })
	}

	req = req.WithContext(ctx)

	// Get appropriate client from the pool
	client := m.getClientForTimeout(opts.timeout, opts.unixSocket)

	resp, err := client.Do(req)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	if opts.stream {
		return m.handleStreamResponse(ctx, l, resp, uw, closer)
	}
	defer closer()

	return m.handleRegularResponse(l, resp)
}

func (m *Module) handleStreamResponse(
	ctx context.Context,
	l *lua.LState,
	resp *http.Response,
	uw engine.UnitOfWork,
	closer context.CancelFunc,
) int {
	s, err := stream.NewStream(ctx, resp.Body)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Create the LuaStream which will be managed by the UoW
	luaStream := stream.NewLuaStream(uw, s, closer)
	ud := l.NewUserData()
	ud.Value = luaStream
	ud.Metatable = value.GetTypeMetatable(l, "Stream")

	l.Push(newResponseWithStream(resp, ud, l))
	return 1
}

func (m *Module) handleRegularResponse(l *lua.LState, resp *http.Response) int {
	body, err := io.ReadAll(resp.Body)

	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	if err := resp.Body.Close(); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(newResponse(resp, &body, len(body), l))
	return 1
}

// requestInfo holds request and its options for batch processing
type requestInfo struct {
	request *http.Request
	options *requestOptions
}

// requestBatch handles concurrent batch requests with proper closer cleanup
func (m *Module) requestBatch(l *lua.LState) int {
	requestsTable := l.CheckTable(1)
	count := requestsTable.Len()
	if count == 0 {
		l.ArgError(1, "requests table cannot be empty")
		return 0
	}

	type result struct {
		index    int
		response *lua.LUserData
		err      error
	}

	results := make(chan result, count)

	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.RaiseError("unit of work not found")
		return 0
	}

	// Validate, parse options, and build requests
	requestInfos := make([]requestInfo, 0, count)

	// Track cleanup functions to ensure they're properly evoked
	cancelers := make([]context.CancelFunc, count)

	requestsTable.ForEach(func(_ lua.LValue, value lua.LValue) {
		if value.Type() != lua.LTTable {
			l.ArgError(1, ErrInvalidRequest.Error())
			return
		}

		reqTable := value.(*lua.LTable)
		method := reqTable.RawGet(lua.LNumber(1))
		if method.Type() != lua.LTString {
			l.ArgError(1, "method must be a string")
			return
		}

		url := reqTable.RawGet(lua.LNumber(2))
		if url.Type() != lua.LTString {
			l.ArgError(1, "URL must be a string")
			return
		}

		optionsValue := reqTable.RawGet(lua.LNumber(3))
		opts, err := parseOptions(optionsValue)
		if err != nil {
			l.ArgError(1, err.Error())
			return
		}

		// Check Unix socket security permission
		if opts.unixSocket != "" {
			if !security.IsAllowed(l.Context(), "http_client.unix_socket", opts.unixSocket, nil) {
				l.ArgError(1, "not allowed to connect to Unix socket: "+opts.unixSocket)
				return
			}
		}

		if !security.IsAllowed(l.Context(), "http_client.request", url.String(), nil) {
			l.ArgError(1, "not allowed to make request to: "+url.String())
			return
		}

		// Don't allow streaming in batch requests
		if opts.stream {
			l.ArgError(1, "streaming is not supported in batch requests")
			return
		}

		req, err := makeRequest(method.String(), url.String(), opts)
		if err != nil {
			l.ArgError(1, err.Error())
			return
		}

		idx := len(requestInfos)
		// Set context with timeout from options
		reqCtx := uw.Context()
		if opts.timeout > 0 {
			var cancel context.CancelFunc
			reqCtx, cancel = context.WithTimeout(uw.Context(), opts.timeout)
			// Store the cancel function to be explicitly called later
			cancelers[idx] = cancel
		}

		requestInfos = append(requestInfos, requestInfo{
			request: req.WithContext(reqCtx),
			options: opts,
		})
	})

	// If any error occurred during validation, return immediately
	if l.GetTop() > 1 {
		// Clean up any created cancel functions
		for _, cancel := range cancelers {
			if cancel != nil {
				cancel()
			}
		}
		return 0
	}

	// Process requests concurrently
	for i, reqInfo := range requestInfos {
		go func(i int, reqInfo requestInfo) {
			defer func() {
				// Ensure the cancel function is called after request completes
				if cancelers[i] != nil {
					cancelers[i]()
				}
			}()

			// Get appropriate client from the pool
			client := m.getClientForTimeout(reqInfo.options.timeout, reqInfo.options.unixSocket)

			resp, err := client.Do(reqInfo.request)
			if err != nil {
				results <- result{i, nil, err}
				return
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				results <- result{i, nil, err}
				return
			}
			_ = resp.Body.Close()

			results <- result{i, newResponse(resp, &body, len(body), l), nil}
		}(i, reqInfo)
	}

	// Collect results
	responsesTable := l.CreateTable(count, 0) // Pre-allocate for exact array size
	errorsTable := l.CreateTable(count, 0)    // Pre-allocate for exact array size
	hasErrors := false

	for i := 0; i < count; i++ {
		res := <-results
		if res.err != nil {
			errorsTable.RawSetInt(res.index+1, lua.LString(res.err.Error()))
			responsesTable.RawSetInt(res.index+1, lua.LNil)
			hasErrors = true
		} else {
			errorsTable.RawSetInt(res.index+1, lua.LNil)
			responsesTable.RawSetInt(res.index+1, res.response)
		}
	}

	l.Push(responsesTable)
	if hasErrors {
		l.Push(errorsTable)
		return 2
	}
	return 1
}
