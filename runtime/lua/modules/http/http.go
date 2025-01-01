package http

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/ponyruntime/go-lua"
	"github.com/ponyruntime/pony/runtime/lua/modules/stream"
	"go.uber.org/zap"
)

const (
	DefaultTimeout = 30 * time.Second
)

var (
	ErrInvalidAuth    = errors.New("auth table must contain non-nil user and pass fields")
	ErrInvalidRequest = errors.New("request must be a table")
)

// Client interface abstracts the http.Client
type Client interface {
	Do(req *http.Request) (*http.Response, error)
}

type Module struct {
	log    *zap.Logger
	client Client
}

func NewHTTPModule(client Client, log *zap.Logger) *Module {
	return &Module{log: log, client: client}
}

func (m *Module) Name() string {
	return "http"
}

// Loader implements the module loader
func (m *Module) Loader(l *lua.LState) int {
	mod := l.NewTable()
	l.SetFuncs(mod, map[string]lua.LGFunction{
		"get":           m.makeMethod("GET"),
		"post":          m.makeMethod("POST"),
		"put":           m.makeMethod("PUT"),
		"delete":        m.makeMethod("DELETE"),
		"head":          m.makeMethod("HEAD"),
		"patch":         m.makeMethod("PATCH"),
		"request":       m.request,
		"request_batch": m.requestBatch,
		"encode_uri":    encodeURI,
		"decode_uri":    decodeURI,
	})

	// Register response type
	registerHTTPResponseType(mod, l)
	stream.RegisterStream(l)

	l.Push(mod)
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

		req, err := makeRequest(method, url, opts)
		if err != nil {
			// Consider using a more generic error message here
			l.ArgError(1, err.Error())
			return 0
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

	req, err := makeRequest(method, url, opts)
	if err != nil {
		// Consider using a more generic error message here
		l.ArgError(1, err.Error())
		return 0
	}

	return m.executeRequest(l, req, opts)
}

// executeRequest performs the actual HTTP request
func (m *Module) executeRequest(l *lua.LState, req *http.Request, opts *requestOptions) int {
	ctx := req.Context()
	if l.Context() != nil {
		ctx = l.Context()
	}

	// Set context with timeout from options
	var cancel context.CancelFunc
	if opts.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, opts.timeout)
		// we do not trigger cancel on streaming responses, either timeout or user will trigger it
	}

	req = req.WithContext(ctx)

	// Execute request
	m.log.Debug("executing request",
		zap.String("method", req.Method),
		zap.String("url", req.URL.String()),
	)

	resp, err := m.client.Do(req)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Handle streaming if stream option is set
	if opts.stream != nil {
		s, err := stream.NewStream(ctx, resp.Body, opts.stream)
		if err != nil {
			resp.Body.Close()
			cancel()
			l.Push(lua.LNil)
			l.Push(lua.LString(err.Error()))
			return 2
		}

		// Create a LuaStream and associate it with the response
		luaStream := &stream.LuaStream{Stream: s}
		ud := l.NewUserData()
		ud.Value = luaStream

		l.SetMetatable(ud, l.GetTypeMetatable("Stream"))

		// Create a new HTTP response that includes the stream
		l.Push(newResponseWithStream(resp, ud, l))

		return 1
	}

	// --- Non-streaming handling ---
	defer resp.Body.Close()
	if cancel != nil {
		defer cancel()
	}

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(newResponse(resp, &body, len(body), l))
	return 1
}

// requestBatch handles concurrent batch requests
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
	ctx := l.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	// Validate, parse options, and build requests
	requests := make([]*http.Request, 0, count) //Preallocate with capacity
	requestsTable.ForEach(func(idx lua.LValue, value lua.LValue) {
		if value.Type() != lua.LTTable {
			l.ArgError(1, ErrInvalidRequest.Error())
			return // Stop processing further if an invalid request is found
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
			return // Stop processing further if option parsing fails
		}

		req, err := makeRequest(method.String(), url.String(), opts)
		if err != nil {
			l.ArgError(1, err.Error())
			return // Stop processing further if request creation fails
		}

		// Set context with timeout from options
		reqCtx := ctx
		if opts.timeout > 0 {
			reqCtx, _ = context.WithTimeout(ctx, opts.timeout)
		}

		requests = append(requests, req.WithContext(reqCtx))
	})

	// If any error occurred during validation, parsing, or request creation, return immediately
	if l.GetTop() > 1 {
		return 0
	}

	// Process requests concurrently
	for i, req := range requests {
		go func(i int, req *http.Request) {
			resp, err := m.client.Do(req)
			if err != nil {
				results <- result{i, nil, err}
				return
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				results <- result{i, nil, err}
				return
			}

			results <- result{i, newResponse(resp, &body, len(body), l), nil}
		}(i, req)
	}

	// Collect results
	responsesTable := l.NewTable()
	errorsTable := l.NewTable()
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
