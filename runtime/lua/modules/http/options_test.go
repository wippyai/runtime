package http

import (
	"github.com/stretchr/testify/assert"
	"github.com/yuin/gopher-lua"
	"net/http"
	"testing"
	"time"
)

func TestParseOptions(t *testing.T) {
	tests := []struct {
		name        string
		setupTable  func(*lua.LState) *lua.LTable
		validate    func(*testing.T, *requestOptions)
		expectError bool
	}{
		{
			name: "empty table",
			setupTable: func(l *lua.LState) *lua.LTable {
				return l.NewTable()
			},
			validate: func(t *testing.T, opts *requestOptions) {
				assert.Empty(t, opts.headers)
				assert.Empty(t, opts.cookies)
				assert.Empty(t, opts.body)
				assert.Empty(t, opts.query)
				assert.Equal(t, DefaultTimeout, opts.timeout)
				assert.Nil(t, opts.auth)
			},
		},
		{
			name: "with headers",
			setupTable: func(l *lua.LState) *lua.LTable {
				tbl := l.NewTable()
				headers := l.NewTable()
				headers.RawSetString("Content-opType", lua.LString("application/json"))
				headers.RawSetString("Authorization", lua.LString("Bearer token"))
				tbl.RawSetString("headers", headers)
				return tbl
			},
			validate: func(t *testing.T, opts *requestOptions) {
				assert.Equal(t, "application/json", opts.headers["Content-opType"])
				assert.Equal(t, "Bearer token", opts.headers["Authorization"])
			},
		},
		{
			name: "with cookies",
			setupTable: func(l *lua.LState) *lua.LTable {
				tbl := l.NewTable()
				cookies := l.NewTable()
				cookies.RawSetString("sessionId", lua.LString("abc123"))
				tbl.RawSetString("cookies", cookies)
				return tbl
			},
			validate: func(t *testing.T, opts *requestOptions) {
				assert.Equal(t, "abc123", opts.cookies["sessionId"])
			},
		},
		{
			name: "with body",
			setupTable: func(l *lua.LState) *lua.LTable {
				tbl := l.NewTable()
				tbl.RawSetString("body", lua.LString(`{"key":"value"}`))
				return tbl
			},
			validate: func(t *testing.T, opts *requestOptions) {
				assert.Equal(t, `{"key":"value"}`, opts.body)
			},
		},
		{
			name: "with form",
			setupTable: func(l *lua.LState) *lua.LTable {
				tbl := l.NewTable()
				tbl.RawSetString("form", lua.LString("key=value"))
				return tbl
			},
			validate: func(t *testing.T, opts *requestOptions) {
				assert.Equal(t, "key=value", opts.body)
				assert.Equal(t, "application/x-www-form-urlencoded", opts.headers["Content-opType"])
			},
		},
		{
			name: "with timeout number",
			setupTable: func(l *lua.LState) *lua.LTable {
				tbl := l.NewTable()
				tbl.RawSetString("timeout", lua.LNumber(10))
				return tbl
			},
			validate: func(t *testing.T, opts *requestOptions) {
				assert.Equal(t, 10*time.Second, opts.timeout)
			},
		},
		{
			name: "with timeout string",
			setupTable: func(l *lua.LState) *lua.LTable {
				tbl := l.NewTable()
				tbl.RawSetString("timeout", lua.LString("5s"))
				return tbl
			},
			validate: func(t *testing.T, opts *requestOptions) {
				assert.Equal(t, 5*time.Second, opts.timeout)
			},
		},
		{
			name: "with valid auth",
			setupTable: func(l *lua.LState) *lua.LTable {
				tbl := l.NewTable()
				auth := l.NewTable()
				auth.RawSetString("user", lua.LString("username"))
				auth.RawSetString("pass", lua.LString("password"))
				tbl.RawSetString("auth", auth)
				return tbl
			},
			validate: func(t *testing.T, opts *requestOptions) {
				assert.NotNil(t, opts.auth)
				assert.Equal(t, "username", opts.auth.user)
				assert.Equal(t, "password", opts.auth.pass)
			},
		},
		{
			name: "with invalid auth",
			setupTable: func(l *lua.LState) *lua.LTable {
				tbl := l.NewTable()
				auth := l.NewTable()
				auth.RawSetString("user", lua.LNumber(123)) // Invalid type
				auth.RawSetString("pass", lua.LString("password"))
				tbl.RawSetString("auth", auth)
				return tbl
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			tbl := tt.setupTable(l)
			opts, err := parseOptions(tbl)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				tt.validate(t, opts)
			}
		})
	}
}

func TestMakeRequest(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		url         string
		opts        *requestOptions
		validate    func(*testing.T, *http.Request)
		expectError bool
	}{
		{
			name:   "basic GET request",
			method: "GET",
			url:    "http://example.com",
			opts: &requestOptions{
				headers: make(map[string]string),
				cookies: make(map[string]string),
			},
			validate: func(t *testing.T, req *http.Request) {
				assert.Equal(t, "GET", req.Method)
				assert.Equal(t, "http://example.com", req.URL.String())
			},
		},
		{
			name:   "request with query",
			method: "GET",
			url:    "http://example.com",
			opts: &requestOptions{
				headers: make(map[string]string),
				cookies: make(map[string]string),
				query:   "key=value",
			},
			validate: func(t *testing.T, req *http.Request) {
				assert.Equal(t, "key=value", req.URL.RawQuery)
			},
		},
		{
			name:   "request with headers",
			method: "POST",
			url:    "http://example.com",
			opts: &requestOptions{
				headers: map[string]string{"Content-opType": "application/json"},
				cookies: make(map[string]string),
			},
			validate: func(t *testing.T, req *http.Request) {
				assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
			},
		},
		{
			name:   "request with body",
			method: "POST",
			url:    "http://example.com",
			opts: &requestOptions{
				headers: make(map[string]string),
				cookies: make(map[string]string),
				body:    `{"key":"value"}`,
			},
			validate: func(t *testing.T, req *http.Request) {
				assert.Equal(t, int64(len(`{"key":"value"}`)), req.ContentLength)
			},
		},
		{
			name:   "request with auth",
			method: "GET",
			url:    "http://example.com",
			opts: &requestOptions{
				headers: make(map[string]string),
				cookies: make(map[string]string),
				auth:    &struct{ user, pass string }{user: "username", pass: "password"},
			},
			validate: func(t *testing.T, req *http.Request) {
				user, pass, ok := req.BasicAuth()
				assert.True(t, ok)
				assert.Equal(t, "username", user)
				assert.Equal(t, "password", pass)
			},
		},
		{
			name:        "empty method",
			method:      "",
			url:         "http://example.com",
			opts:        &requestOptions{},
			expectError: true,
		},
		{
			name:        "empty URL",
			method:      "GET",
			url:         "",
			opts:        &requestOptions{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := makeRequest(tt.method, tt.url, tt.opts)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				tt.validate(t, req)
			}
		})
	}
}
