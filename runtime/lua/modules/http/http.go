package http

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ponyruntime/go-lua"
	"go.uber.org/zap"
)

const (
	DefaultTimeout = 30 * time.Second
)

var (
	ErrInvalidAuth    = errors.New("auth table must contain non-nil user and pass fields")
	ErrInvalidRequest = errors.New("request must be a table")
)

// HTTPClient interface abstracts the http.Client
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type Module struct {
	log    *zap.Logger
	client HTTPClient
}

func NewHTTPModule(client HTTPClient, log *zap.Logger) *Module {
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
		"encode_uri":    m.encodeURI,
		"decode_uri":    m.decodeURI,
	})

	// Register response type
	registerHTTPResponseType(mod, l)
	l.Push(mod)
	return 1
}

// requestOptions holds parsed request options
type requestOptions struct {
	headers map[string]string
	cookies map[string]string
	body    string
	query   string
	timeout time.Duration
	auth    *struct{ user, pass string }
}

// parseOptions parses Lua value into requestOptions
func parseOptions(l *lua.LState, value lua.LValue) (*requestOptions, error) {
	opts := &requestOptions{
		headers: make(map[string]string),
		cookies: make(map[string]string),
		timeout: DefaultTimeout, // Set default timeout
	}

	// Handle nil or non-table value
	if value == nil || value.Type() != lua.LTTable {
		return opts, nil
	}

	table := value.(*lua.LTable)

	// Parse headers
	if headers := table.RawGetString("headers"); headers != lua.LNil {
		if t, ok := headers.(*lua.LTable); ok {
			t.ForEach(func(k, v lua.LValue) {
				opts.headers[k.String()] = v.String()
			})
		}
	}

	// Parse cookies
	if cookies := table.RawGetString("cookies"); cookies != lua.LNil {
		if t, ok := cookies.(*lua.LTable); ok {
			t.ForEach(func(k, v lua.LValue) {
				opts.cookies[k.String()] = v.String()
			})
		}
	}

	// Parse body
	if body := table.RawGetString("body"); body != lua.LNil {
		opts.body = body.String()
	} else if form := table.RawGetString("form"); form != lua.LNil {
		opts.body = form.String()
		opts.headers["Content-Type"] = "application/x-www-form-urlencoded"
	}

	// Parse query
	if query := table.RawGetString("query"); query != lua.LNil {
		opts.query = query.String()
	}

	// Parse timeout
	if timeout := table.RawGetString("timeout"); timeout != lua.LNil {
		switch t := timeout.(type) {
		case lua.LNumber:
			opts.timeout = time.Duration(t) * time.Second
		case lua.LString:
			if d, err := time.ParseDuration(string(t)); err == nil {
				opts.timeout = d
			}
		}
	}

	// Parse auth
	if auth := table.RawGetString("auth"); auth != lua.LNil {
		if t, ok := auth.(*lua.LTable); ok {
			user := t.RawGetString("user")
			pass := t.RawGetString("pass")
			if user.Type() != lua.LTString || pass.Type() != lua.LTString {
				return nil, ErrInvalidAuth
			}
			opts.auth = &struct{ user, pass string }{
				user: user.String(),
				pass: pass.String(),
			}
		}
	}

	return opts, nil
}

// makeRequest creates an HTTP request with the given method, URL, and options
func makeRequest(
	method, url string,
	opts *requestOptions,
) (*http.Request, error) {
	if method == "" {
		return nil, errors.New("method cannot be empty")
	}

	if url == "" {
		return nil, errors.New("URL cannot be empty")
	}

	req, err := http.NewRequest(strings.ToUpper(method), url, nil)
	if err != nil {
		return nil, err
	}

	// Apply options
	if opts.query != "" {
		req.URL.RawQuery = opts.query
	}

	for k, v := range opts.headers {
		req.Header.Set(k, v)
	}

	for k, v := range opts.cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}

	if opts.body != "" {
		req.Body = io.NopCloser(strings.NewReader(opts.body))
		req.ContentLength = int64(len(opts.body))
	}

	if opts.auth != nil {
		req.SetBasicAuth(opts.auth.user, opts.auth.pass)
	}

	return req, nil
}

// getMethodFromArgs fetches and validates the HTTP method from Lua arguments.
func getMethodFromArgs(l *lua.LState, argIndex int) (string, error) {
	method := l.CheckString(argIndex)
	if method == "" {
		return "", errors.New("method cannot be empty")
	}

	// validate method
	switch strings.ToUpper(method) {
	case "GET", "POST", "PUT", "DELETE", "HEAD", "PATCH":
		return strings.ToUpper(method), nil
	default:
		return "", errors.New("invalid method")
	}
}

// getURLFromArgs fetches and validates the URL from Lua arguments.
func getURLFromArgs(l *lua.LState, argIndex int) (string, error) {
	if l.Get(argIndex).Type() != lua.LTString {
		return "", errors.New("URL must be a string")
	}

	url := l.CheckString(argIndex)
	if url == "" {
		return "", errors.New("URL cannot be empty")
	}

	return url, nil
}

// getOptionsFromArgs fetches and validates the options from Lua arguments.
func getOptionsFromArgs(l *lua.LState, argIndex int) (*requestOptions, error) {
	optionsValue := l.OptTable(argIndex, l.NewTable())
	opts, err := parseOptions(l, optionsValue)
	if err != nil {
		return nil, err
	}

	return opts, nil
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
	if opts.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.timeout)
		defer cancel()
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
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(newHTTPResponse(resp, &body, len(body), l))
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

		opts, err := parseOptions(l, optionsValue)
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

			results <- result{i, newHTTPResponse(resp, &body, len(body), l), nil}
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

func (m *Module) encodeURI(l *lua.LState) int {
	// check num args
	if l.GetTop() != 1 {
		l.ArgError(1, "encode_uri requires exactly 1 string argument")
		return 0
	}

	// check type
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}

	str := l.CheckString(1) // Correctly checks if the first argument is a string.

	l.Push(lua.LString(url.QueryEscape(str)))
	return 1
}

func (m *Module) decodeURI(l *lua.LState) int {
	// check num args
	if l.GetTop() != 1 {
		l.ArgError(1, "decode_uri requires exactly 1 string argument")
		return 0
	}

	// check type
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}

	str := l.CheckString(1) // Correctly checks if the first argument is a string.
	decoded, err := url.QueryUnescape(str)
	if err != nil {
		// This is a processing error, not an argument error
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(lua.LString(decoded))
	return 1
}

// toTable safely converts a LValue to LTable
func toTable(v lua.LValue) *lua.LTable {
	if t, ok := v.(*lua.LTable); ok {
		return t
	}
	return nil
}
