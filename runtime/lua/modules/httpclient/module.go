package httpclient

import (
	"net/url"
	"strings"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	httpapi "github.com/wippyai/runtime/api/dispatcher/http"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/security"
	lua "github.com/yuin/gopher-lua"
)

var responseMetatable *lua.LTable

// Module is the http_client module definition.
var Module = &luaapi.ModuleDef{
	Name:        "http_client",
	Description: "HTTP client requests (get, post, etc.)",
	Class:       []string{luaapi.ClassNetwork, luaapi.ClassIO, luaapi.ClassNondeterministic},
	Build:       buildModule,
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	responseMetatable = lua.CreateTable(0, 2)
	responseMetatable.RawSetString("__index", lua.LGoFunc(responseIndex))
	responseMetatable.RawSetString("__tostring", lua.LGoFunc(responseToString))
	responseMetatable.Immutable = true

	mod := lua.CreateTable(0, 10)
	mod.RawSetString("get", lua.LGoFunc(makeMethod("GET")))
	mod.RawSetString("post", lua.LGoFunc(makeMethod("POST")))
	mod.RawSetString("put", lua.LGoFunc(makeMethod("PUT")))
	mod.RawSetString("delete", lua.LGoFunc(makeMethod("DELETE")))
	mod.RawSetString("head", lua.LGoFunc(makeMethod("HEAD")))
	mod.RawSetString("patch", lua.LGoFunc(makeMethod("PATCH")))
	mod.RawSetString("request", lua.LGoFunc(request))
	mod.RawSetString("request_batch", lua.LGoFunc(requestBatch))
	mod.RawSetString("encode_uri", lua.LGoFunc(encodeURI))
	mod.RawSetString("decode_uri", lua.LGoFunc(decodeURI))
	mod.Immutable = true

	yields := []luaapi.YieldType{
		{Sample: &RequestYield{}, CmdID: dispatcher.CommandID(httpapi.CmdRequest)},
		{Sample: &RequestBatchYield{}, CmdID: dispatcher.CommandID(httpapi.CmdRequestBatch)},
	}

	return mod, yields
}

func makeMethod(method string) lua.LGFunction {
	return func(l *lua.LState) int {
		urlStr := l.CheckString(1)
		if urlStr == "" {
			l.ArgError(1, "URL required")
			return 0
		}

		opts := parseOptions(l, 2)

		ctx := l.Context()
		if ctx == nil {
			l.Push(lua.LNil)
			l.Push(lua.LString("no context"))
			return 2
		}

		if opts.unixSocket != "" {
			if !security.IsAllowed(ctx, "http.unix_socket", opts.unixSocket, nil) {
				l.Push(lua.LNil)
				l.Push(lua.LString("not allowed: unix socket " + opts.unixSocket))
				return 2
			}
		}

		if !security.IsAllowed(ctx, "http.request", urlStr, nil) {
			l.Push(lua.LNil)
			l.Push(lua.LString("not allowed: " + urlStr))
			return 2
		}

		yield := AcquireRequestYield()
		populateYield(yield, method, urlStr, opts)

		l.Push(yield)
		return -1
	}
}

func request(l *lua.LState) int {
	method := strings.ToUpper(l.CheckString(1))
	if method == "" {
		l.ArgError(1, "method required")
		return 0
	}

	urlStr := l.CheckString(2)
	if urlStr == "" {
		l.ArgError(2, "URL required")
		return 0
	}

	opts := parseOptions(l, 3)

	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context"))
		return 2
	}

	if opts.unixSocket != "" {
		if !security.IsAllowed(ctx, "http.unix_socket", opts.unixSocket, nil) {
			l.Push(lua.LNil)
			l.Push(lua.LString("not allowed: unix socket " + opts.unixSocket))
			return 2
		}
	}

	if !security.IsAllowed(ctx, "http.request", urlStr, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("not allowed: " + urlStr))
		return 2
	}

	yield := AcquireRequestYield()
	populateYield(yield, method, urlStr, opts)

	l.Push(yield)
	return -1
}

func populateYield(yield *RequestYield, method, url string, opts *requestOptions) {
	yield.Method = method
	yield.URL = url
	yield.Headers = opts.headers
	yield.Body = opts.body
	yield.Timeout = opts.timeout
	yield.UnixSocket = opts.unixSocket
	yield.Query = opts.query
	yield.Cookies = opts.cookies
	yield.Form = opts.form
	yield.BasicAuthUser = opts.basicAuthUser
	yield.BasicAuthPass = opts.basicAuthPass
	yield.Stream = opts.stream

	// Convert files
	if len(opts.files) > 0 {
		yield.Files = make([]httpapi.FileUpload, len(opts.files))
		for i, f := range opts.files {
			yield.Files[i] = httpapi.FileUpload{
				FieldName: f.fieldName,
				FileName:  f.fileName,
				Data:      f.data,
			}
		}
	}
}

type requestOptions struct {
	headers       map[string]string
	body          []byte
	timeout       time.Duration
	unixSocket    string
	query         map[string]string
	cookies       map[string]string
	form          map[string]string
	files         []fileUpload
	basicAuthUser string
	basicAuthPass string
	stream        bool
}

type fileUpload struct {
	fieldName string
	fileName  string
	data      []byte
}

func parseOptions(l *lua.LState, idx int) *requestOptions {
	opts := &requestOptions{}

	if l.GetTop() < idx {
		return opts
	}

	tbl := l.OptTable(idx, nil)
	if tbl == nil {
		return opts
	}

	if headers := tbl.RawGetString("headers"); headers.Type() == lua.LTTable {
		opts.headers = make(map[string]string)
		headers.(*lua.LTable).ForEach(func(k, v lua.LValue) {
			if k.Type() == lua.LTString && v.Type() == lua.LTString {
				opts.headers[k.String()] = v.String()
			}
		})
	}

	if body := tbl.RawGetString("body"); body.Type() == lua.LTString {
		opts.body = []byte(body.String())
	}

	if timeout := tbl.RawGetString("timeout"); timeout.Type() == lua.LTNumber || timeout.Type() == lua.LTInteger {
		opts.timeout = time.Duration(lua.LVAsNumber(timeout) * lua.LNumber(time.Second))
	}

	if socket := tbl.RawGetString("unix_socket"); socket.Type() == lua.LTString {
		opts.unixSocket = socket.String()
	}

	// Query params
	if query := tbl.RawGetString("query"); query.Type() == lua.LTTable {
		opts.query = make(map[string]string)
		query.(*lua.LTable).ForEach(func(k, v lua.LValue) {
			if k.Type() == lua.LTString && v.Type() == lua.LTString {
				opts.query[k.String()] = v.String()
			}
		})
	}

	// Cookies
	if cookies := tbl.RawGetString("cookies"); cookies.Type() == lua.LTTable {
		opts.cookies = make(map[string]string)
		cookies.(*lua.LTable).ForEach(func(k, v lua.LValue) {
			if k.Type() == lua.LTString && v.Type() == lua.LTString {
				opts.cookies[k.String()] = v.String()
			}
		})
	}

	// Form data
	if form := tbl.RawGetString("form"); form.Type() == lua.LTTable {
		opts.form = make(map[string]string)
		form.(*lua.LTable).ForEach(func(k, v lua.LValue) {
			if k.Type() == lua.LTString && v.Type() == lua.LTString {
				opts.form[k.String()] = v.String()
			}
		})
	}

	// Files (array of {name, filename, content})
	if files := tbl.RawGetString("files"); files.Type() == lua.LTTable {
		files.(*lua.LTable).ForEach(func(_, v lua.LValue) {
			if ft, ok := v.(*lua.LTable); ok {
				f := fileUpload{}
				if name := ft.RawGetString("name"); name.Type() == lua.LTString {
					f.fieldName = name.String()
				}
				if filename := ft.RawGetString("filename"); filename.Type() == lua.LTString {
					f.fileName = filename.String()
				}
				if content := ft.RawGetString("content"); content.Type() == lua.LTString {
					f.data = []byte(content.String())
				}
				if f.fieldName != "" && len(f.data) > 0 {
					opts.files = append(opts.files, f)
				}
			}
		})
	}

	// Basic auth
	if auth := tbl.RawGetString("auth"); auth.Type() == lua.LTTable {
		authTbl := auth.(*lua.LTable)
		if user := authTbl.RawGetString("user"); user.Type() == lua.LTString {
			opts.basicAuthUser = user.String()
		}
		if pass := authTbl.RawGetString("pass"); pass.Type() == lua.LTString {
			opts.basicAuthPass = pass.String()
		}
	}

	// Stream response
	if stream := tbl.RawGetString("stream"); stream.Type() == lua.LTBool {
		opts.stream = bool(stream.(lua.LBool))
	}

	return opts
}

func encodeURI(l *lua.LState) int {
	s := l.CheckString(1)
	l.Push(lua.LString(url.QueryEscape(s)))
	return 1
}

func decodeURI(l *lua.LState) int {
	s := l.CheckString(1)
	decoded, err := url.QueryUnescape(s)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(lua.LString(decoded))
	return 1
}

// requestBatch handles concurrent batch HTTP requests.
// Each entry in the table is: {method, url, options?}
func requestBatch(l *lua.LState) int {
	requestsTable := l.CheckTable(1)
	if requestsTable.Len() == 0 {
		l.ArgError(1, "requests table cannot be empty")
		return 0
	}

	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context"))
		return 2
	}

	yield := AcquireRequestBatchYield()
	yield.Requests = make([]*httpapi.RequestCmd, 0, requestsTable.Len())

	var parseErr string
	requestsTable.ForEach(func(_ lua.LValue, value lua.LValue) {
		if parseErr != "" {
			return
		}
		if value.Type() != lua.LTTable {
			parseErr = "each request must be a table"
			return
		}

		reqTable := value.(*lua.LTable)
		methodVal := reqTable.RawGetInt(1)
		if methodVal.Type() != lua.LTString {
			parseErr = "method must be a string"
			return
		}
		method := strings.ToUpper(methodVal.String())

		urlVal := reqTable.RawGetInt(2)
		if urlVal.Type() != lua.LTString {
			parseErr = "URL must be a string"
			return
		}
		urlStr := urlVal.String()

		// Check security
		if !security.IsAllowed(ctx, "http.request", urlStr, nil) {
			parseErr = "not allowed: " + urlStr
			return
		}

		var opts *requestOptions
		optsVal := reqTable.RawGetInt(3)
		if optsVal.Type() == lua.LTTable {
			opts = parseOptionsFromTable(optsVal.(*lua.LTable))
			// Check unix socket security
			if opts.unixSocket != "" {
				if !security.IsAllowed(ctx, "http.unix_socket", opts.unixSocket, nil) {
					parseErr = "not allowed: unix socket " + opts.unixSocket
					return
				}
			}
			// Streaming not supported in batch
			if opts.stream {
				parseErr = "streaming not supported in batch requests"
				return
			}
		} else {
			opts = &requestOptions{}
		}

		req := httpapi.AcquireRequestCmd()
		req.Method = method
		req.URL = urlStr
		req.Headers = opts.headers
		req.Body = opts.body
		req.Timeout = opts.timeout
		req.UnixSocket = opts.unixSocket
		req.Query = opts.query
		req.Cookies = opts.cookies
		req.Form = opts.form
		req.BasicAuthUser = opts.basicAuthUser
		req.BasicAuthPass = opts.basicAuthPass

		if len(opts.files) > 0 {
			req.Files = make([]httpapi.FileUpload, len(opts.files))
			for i, f := range opts.files {
				req.Files[i] = httpapi.FileUpload{
					FieldName: f.fieldName,
					FileName:  f.fileName,
					Data:      f.data,
				}
			}
		}

		yield.Requests = append(yield.Requests, req)
	})

	if parseErr != "" {
		ReleaseRequestBatchYield(yield)
		l.Push(lua.LNil)
		l.Push(lua.LString(parseErr))
		return 2
	}

	l.Push(yield)
	return -1
}

// parseOptionsFromTable extracts request options from a Lua table.
func parseOptionsFromTable(tbl *lua.LTable) *requestOptions {
	opts := &requestOptions{}

	if headers := tbl.RawGetString("headers"); headers.Type() == lua.LTTable {
		opts.headers = make(map[string]string)
		headers.(*lua.LTable).ForEach(func(k, v lua.LValue) {
			if k.Type() == lua.LTString && v.Type() == lua.LTString {
				opts.headers[k.String()] = v.String()
			}
		})
	}

	if body := tbl.RawGetString("body"); body.Type() == lua.LTString {
		opts.body = []byte(body.String())
	}

	if timeout := tbl.RawGetString("timeout"); timeout.Type() == lua.LTNumber || timeout.Type() == lua.LTInteger {
		opts.timeout = time.Duration(lua.LVAsNumber(timeout) * lua.LNumber(time.Second))
	}

	if socket := tbl.RawGetString("unix_socket"); socket.Type() == lua.LTString {
		opts.unixSocket = socket.String()
	}

	if query := tbl.RawGetString("query"); query.Type() == lua.LTTable {
		opts.query = make(map[string]string)
		query.(*lua.LTable).ForEach(func(k, v lua.LValue) {
			if k.Type() == lua.LTString && v.Type() == lua.LTString {
				opts.query[k.String()] = v.String()
			}
		})
	}

	if cookies := tbl.RawGetString("cookies"); cookies.Type() == lua.LTTable {
		opts.cookies = make(map[string]string)
		cookies.(*lua.LTable).ForEach(func(k, v lua.LValue) {
			if k.Type() == lua.LTString && v.Type() == lua.LTString {
				opts.cookies[k.String()] = v.String()
			}
		})
	}

	if form := tbl.RawGetString("form"); form.Type() == lua.LTTable {
		opts.form = make(map[string]string)
		form.(*lua.LTable).ForEach(func(k, v lua.LValue) {
			if k.Type() == lua.LTString && v.Type() == lua.LTString {
				opts.form[k.String()] = v.String()
			}
		})
	}

	if files := tbl.RawGetString("files"); files.Type() == lua.LTTable {
		files.(*lua.LTable).ForEach(func(_, v lua.LValue) {
			if ft, ok := v.(*lua.LTable); ok {
				f := fileUpload{}
				if name := ft.RawGetString("name"); name.Type() == lua.LTString {
					f.fieldName = name.String()
				}
				if filename := ft.RawGetString("filename"); filename.Type() == lua.LTString {
					f.fileName = filename.String()
				}
				if content := ft.RawGetString("content"); content.Type() == lua.LTString {
					f.data = []byte(content.String())
				}
				if f.fieldName != "" && len(f.data) > 0 {
					opts.files = append(opts.files, f)
				}
			}
		})
	}

	if auth := tbl.RawGetString("auth"); auth.Type() == lua.LTTable {
		authTbl := auth.(*lua.LTable)
		if user := authTbl.RawGetString("user"); user.Type() == lua.LTString {
			opts.basicAuthUser = user.String()
		}
		if pass := authTbl.RawGetString("pass"); pass.Type() == lua.LTString {
			opts.basicAuthPass = pass.String()
		}
	}

	if stream := tbl.RawGetString("stream"); stream.Type() == lua.LTBool {
		opts.stream = bool(stream.(lua.LBool))
	}

	return opts
}
