package http

import (
	"errors"
	"github.com/ponyruntime/pony/runtime/lua/modules/stream"
	"github.com/yuin/gopher-lua"
	"io"
	"net/http"
	"strings"
	"time"
)

// requestOptions holds parsed request options
type requestOptions struct {
	headers map[string]string
	cookies map[string]string
	body    string
	query   string
	timeout time.Duration
	auth    *struct{ user, pass string }
	stream  *stream.Options // Add stream configuration
}

// parseOptions parses Lua value into requestOptions
func parseOptions(value lua.LValue) (*requestOptions, error) {
	opts := &requestOptions{
		headers: make(map[string]string),
		cookies: make(map[string]string),
		timeout: DefaultTimeout, // Set default timeout
	}

	if value == nil || value.Type() != lua.LTTable {
		return opts, nil
	}

	table := value.(*lua.LTable)

	if headers := table.RawGetString("headers"); headers != lua.LNil {
		if t, ok := headers.(*lua.LTable); ok {
			t.ForEach(func(k, v lua.LValue) {
				opts.headers[k.String()] = v.String()
			})
		}
	}

	if cookies := table.RawGetString("cookies"); cookies != lua.LNil {
		if t, ok := cookies.(*lua.LTable); ok {
			t.ForEach(func(k, v lua.LValue) {
				opts.cookies[k.String()] = v.String()
			})
		}
	}

	if body := table.RawGetString("body"); body != lua.LNil {
		opts.body = body.String()
	} else if form := table.RawGetString("form"); form != lua.LNil {
		opts.body = form.String()
		opts.headers["Content-type"] = "application/x-www-form-urlencoded"
	}

	if query := table.RawGetString("query"); query != lua.LNil {
		opts.query = query.String()
	}

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

	if streamOpts := table.RawGetString("stream"); streamOpts != lua.LNil {
		if streamTable, ok := streamOpts.(*lua.LTable); ok {
			bufferSize := int64(0)
			if bs := streamTable.RawGetString("buffer_size"); bs.Type() == lua.LTNumber {
				bufferSize = int64(bs.(lua.LNumber))
			}

			opts.stream = stream.NewStreamConfig(bufferSize)
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
