// SPDX-License-Identifier: MPL-2.0

package httpclient

import (
	"context"
	"net"
	"net/url"
	"strings"
	"time"

	lua "github.com/wippyai/go-lua"
	netapi "github.com/wippyai/runtime/api/net"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	httpapi "github.com/wippyai/runtime/api/service/http"
	"github.com/wippyai/runtime/runtime/security"
)

func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

// hasOverlay reports whether the request will be routed through an overlay
// network — either because the user set overlay_network explicitly, or
// because a frame-level / app-wide default is present on the context. When
// true, the private-IP check must skip local DNS resolution to avoid
// leaking the target hostname to the system resolver.
func hasOverlay(ctx context.Context, opts *requestOptions) bool {
	return opts.overlayNetwork != "" || netapi.GetDefaultNetwork(ctx) != ""
}

func checkPrivateIP(ctx context.Context, urlStr string, hasOverlay bool) string {
	u, err := url.Parse(urlStr)
	if err != nil {
		return ""
	}

	host := u.Hostname()
	if host == "" {
		return ""
	}

	if ip := net.ParseIP(host); ip != nil {
		if isPrivateIP(ip) {
			if !security.IsAllowed(ctx, "http_client.private_ip", host, nil) {
				return "not allowed: private IP " + host
			}
		}
		return ""
	}

	// When an overlay network is active (Tor, I2P, etc.), skip local DNS
	// resolution. The overlay resolves DNS at the remote end; performing
	// a local lookup would leak the target hostname to the system resolver,
	// defeating the privacy guarantees of the overlay.
	if hasOverlay {
		return ""
	}

	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return ""
	}

	for _, ip := range ips {
		if isPrivateIP(ip) {
			if !security.IsAllowed(ctx, "http_client.private_ip", ip.String(), nil) {
				return "not allowed: private IP " + ip.String()
			}
		}
	}

	return ""
}

// parseDuration parses a Lua value into time.Duration.
// Supports numbers (as seconds) and strings (Go duration format like "5m", "30s", "1h").
func parseDuration(lv lua.LValue) (time.Duration, bool) {
	switch v := lv.(type) {
	case lua.LString:
		d, err := time.ParseDuration(string(v))
		if err != nil {
			return 0, false
		}
		return d, true
	case lua.LNumber:
		return time.Duration(v) * time.Second, true
	case lua.LInteger:
		return time.Duration(v) * time.Second, true
	default:
		return 0, false
	}
}

var responseMetatable *lua.LTable

// Module is the http_client module definition.
var Module = &luaapi.ModuleDef{
	Name:        "http_client",
	Description: "HTTP client requests (get, post, etc.)",
	Class:       []string{luaapi.ClassNetwork, luaapi.ClassIO, luaapi.ClassNondeterministic},
	Build:       buildModule,
	Types:       ModuleTypes,
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	responseMetatable = lua.CreateTable(0, 2)
	responseMetatable.RawSetString("__index", lua.LGoFunc(responseIndex))
	responseMetatable.RawSetString("__tostring", lua.LGoFunc(responseToString))
	responseMetatable.Immutable = true

	mod := lua.CreateTable(0, 10)
	mod.RawSetString("get", makeMethod("GET"))
	mod.RawSetString("post", makeMethod("POST"))
	mod.RawSetString("put", makeMethod("PUT"))
	mod.RawSetString("delete", makeMethod("DELETE"))
	mod.RawSetString("head", makeMethod("HEAD"))
	mod.RawSetString("patch", makeMethod("PATCH"))
	mod.RawSetString("request", lua.LGoFunc(request))
	mod.RawSetString("request_batch", lua.LGoFunc(requestBatch))
	mod.RawSetString("encode_uri", lua.LGoFunc(encodeURI))
	mod.RawSetString("decode_uri", lua.LGoFunc(decodeURI))
	mod.Immutable = true

	yields := []luaapi.YieldType{
		{Sample: &RequestYield{}, CmdID: httpapi.Request},
		{Sample: &RequestBatchYield{}, CmdID: httpapi.RequestBatch},
	}

	return mod, yields
}

func makeMethod(method string) lua.LGoFunc {
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
			l.Push(lua.NewLuaError(l, "no context").WithKind(lua.Internal).WithRetryable(false))
			return 2
		}

		if opts.unixSocket != "" {
			if !security.IsAllowed(ctx, "http_client.unix_socket", opts.unixSocket, nil) {
				l.Push(lua.LNil)
				l.Push(lua.NewLuaError(l, "not allowed: unix socket "+opts.unixSocket).WithKind(lua.PermissionDenied).WithRetryable(false))
				return 2
			}
		}

		if opts.tls != nil && opts.tls.InsecureSkipVerify {
			if !security.IsAllowed(ctx, "http_client.insecure_tls", urlStr, nil) {
				l.Push(lua.LNil)
				l.Push(lua.NewLuaError(l, "not allowed: insecure TLS for "+urlStr).WithKind(lua.PermissionDenied).WithRetryable(false))
				return 2
			}
		}

		if opts.overlayNetwork != "" {
			if !security.IsAllowed(ctx, "network.select", opts.overlayNetwork, nil) {
				l.Push(lua.LNil)
				l.Push(lua.NewLuaError(l, "not allowed: network "+opts.overlayNetwork).WithKind(lua.PermissionDenied).WithRetryable(false))
				return 2
			}
		}

		if !security.IsAllowed(ctx, "http_client.request", urlStr, nil) {
			l.Push(lua.LNil)
			l.Push(lua.NewLuaError(l, "not allowed: "+urlStr).WithKind(lua.PermissionDenied).WithRetryable(false))
			return 2
		}

		// Skip private IP check for unix sockets — hostname is irrelevant for local IPC
		if opts.unixSocket == "" {
			if errMsg := checkPrivateIP(ctx, urlStr, hasOverlay(ctx, opts)); errMsg != "" {
				l.Push(lua.LNil)
				l.Push(lua.NewLuaError(l, errMsg).WithKind(lua.PermissionDenied).WithRetryable(false))
				return 2
			}
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
		l.Push(lua.NewLuaError(l, "no context").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	if opts.unixSocket != "" {
		if !security.IsAllowed(ctx, "http_client.unix_socket", opts.unixSocket, nil) {
			l.Push(lua.LNil)
			l.Push(lua.NewLuaError(l, "not allowed: unix socket "+opts.unixSocket).WithKind(lua.PermissionDenied).WithRetryable(false))
			return 2
		}
	}

	if opts.tls != nil && opts.tls.InsecureSkipVerify {
		if !security.IsAllowed(ctx, "http_client.insecure_tls", urlStr, nil) {
			l.Push(lua.LNil)
			l.Push(lua.NewLuaError(l, "not allowed: insecure TLS for "+urlStr).WithKind(lua.PermissionDenied).WithRetryable(false))
			return 2
		}
	}

	if opts.overlayNetwork != "" {
		if !security.IsAllowed(ctx, "network.select", opts.overlayNetwork, nil) {
			l.Push(lua.LNil)
			l.Push(lua.NewLuaError(l, "not allowed: network "+opts.overlayNetwork).WithKind(lua.PermissionDenied).WithRetryable(false))
			return 2
		}
	}

	if !security.IsAllowed(ctx, "http_client.request", urlStr, nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "not allowed: "+urlStr).WithKind(lua.PermissionDenied).WithRetryable(false))
		return 2
	}

	// Skip private IP check for unix sockets — hostname is irrelevant for local IPC
	if opts.unixSocket == "" {
		if errMsg := checkPrivateIP(ctx, urlStr, hasOverlay(ctx, opts)); errMsg != "" {
			l.Push(lua.LNil)
			l.Push(lua.NewLuaError(l, errMsg).WithKind(lua.PermissionDenied).WithRetryable(false))
			return 2
		}
	}

	yield := AcquireRequestYield()
	populateYield(yield, method, urlStr, opts)

	l.Push(yield)
	return -1
}

func populateYield(yield *RequestYield, method, url string, opts *requestOptions) {
	yield.Method = method
	yield.URL = url
	if len(opts.headers) > 0 {
		yield.Headers = make(map[string][]string, len(opts.headers))
		for k, v := range opts.headers {
			yield.Headers[k] = []string{v}
		}
	}
	yield.Body = opts.body
	yield.Timeout = opts.timeout
	yield.UnixSocket = opts.unixSocket
	yield.Query = opts.query
	yield.Cookies = opts.cookies
	yield.Form = opts.form
	yield.BasicAuthUser = opts.basicAuthUser
	yield.BasicAuthPass = opts.basicAuthPass
	yield.Stream = opts.stream
	yield.MaxResponseBody = opts.maxResponseBody
	yield.TLS = opts.tls
	yield.OverlayNetwork = opts.overlayNetwork

	// Convert files
	if len(opts.files) > 0 {
		yield.Files = make([]httpapi.FileUpload, len(opts.files))
		for i, f := range opts.files {
			data := f.data

			// If reader is provided, read data from it
			if f.reader != nil {
				if r, ok := f.reader.(interface{ Read([]byte) (int, error) }); ok {
					buf := make([]byte, 0, 4096)
					tmp := make([]byte, 4096)
					for {
						n, err := r.Read(tmp)
						if n > 0 {
							buf = append(buf, tmp[:n]...)
						}
						if err != nil {
							break
						}
					}
					data = buf
				}
			}

			yield.Files[i] = httpapi.FileUpload{
				FieldName: f.fieldName,
				FileName:  f.fileName,
				Data:      data,
			}
		}
	}
}

type requestOptions struct {
	tls             *httpapi.TLSConfig
	query           map[string]string
	cookies         map[string]string
	form            map[string]string
	headers         map[string]string
	unixSocket      string
	basicAuthUser   string
	basicAuthPass   string
	overlayNetwork  string
	body            []byte
	files           []fileUpload
	timeout         time.Duration
	maxResponseBody int64
	stream          bool
}

type fileUpload struct {
	reader      any
	fieldName   string
	fileName    string
	contentType string
	data        []byte
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

	if timeout := tbl.RawGetString("timeout"); timeout != lua.LNil {
		if d, ok := parseDuration(timeout); ok {
			opts.timeout = d
		}
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

	// Files (array of {name, filename, content_type?, content?, reader?})
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
				if contentType := ft.RawGetString("content_type"); contentType.Type() == lua.LTString {
					f.contentType = contentType.String()
				} else {
					f.contentType = "application/octet-stream"
				}

				// Support both content (string) and reader (io.Reader)
				if content := ft.RawGetString("content"); content.Type() == lua.LTString {
					f.data = []byte(content.String())
				} else if reader := ft.RawGetString("reader"); reader.Type() == lua.LTUserData {
					f.reader = reader.(*lua.LUserData).Value
				}

				if f.fieldName != "" && (len(f.data) > 0 || f.reader != nil) {
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

	// Max response body size (0 = use default)
	if maxBody := tbl.RawGetString("max_response_body"); maxBody.Type() == lua.LTNumber || maxBody.Type() == lua.LTInteger {
		opts.maxResponseBody = int64(lua.LVAsNumber(maxBody))
	}

	// TLS configuration
	if tlsVal := tbl.RawGetString("tls"); tlsVal.Type() == lua.LTTable {
		opts.tls = parseTLSConfig(tlsVal.(*lua.LTable))
	}

	// Overlay network
	if overlay := tbl.RawGetString("overlay_network"); overlay.Type() == lua.LTString {
		opts.overlayNetwork = overlay.String()
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
		l.Push(lua.NewLuaError(l, err.Error()).WithKind(lua.Invalid).WithRetryable(false))
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
		l.Push(lua.NewLuaError(l, "no context").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	yield := AcquireRequestBatchYield()
	yield.Requests = make([]*httpapi.RequestCmd, 0, requestsTable.Len())

	var parseErr string
	parseErrKind := lua.Invalid
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
		if !security.IsAllowed(ctx, "http_client.request", urlStr, nil) {
			parseErr = "not allowed: " + urlStr
			parseErrKind = lua.PermissionDenied
			return
		}

		var opts *requestOptions
		optsVal := reqTable.RawGetInt(3)
		if optsVal.Type() == lua.LTTable {
			opts = parseOptionsFromTable(optsVal.(*lua.LTable))
			// Check unix socket security
			if opts.unixSocket != "" {
				if !security.IsAllowed(ctx, "http_client.unix_socket", opts.unixSocket, nil) {
					parseErr = "not allowed: unix socket " + opts.unixSocket
					parseErrKind = lua.PermissionDenied
					return
				}
			}
			if opts.tls != nil && opts.tls.InsecureSkipVerify {
				if !security.IsAllowed(ctx, "http_client.insecure_tls", urlStr, nil) {
					parseErr = "not allowed: insecure TLS for " + urlStr
					parseErrKind = lua.PermissionDenied
					return
				}
			}
			if opts.overlayNetwork != "" {
				if !security.IsAllowed(ctx, "network.select", opts.overlayNetwork, nil) {
					parseErr = "not allowed: network " + opts.overlayNetwork
					parseErrKind = lua.PermissionDenied
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

		// Skip private IP check for unix sockets — hostname is irrelevant for local IPC
		if opts.unixSocket == "" {
			if errMsg := checkPrivateIP(ctx, urlStr, hasOverlay(ctx, opts)); errMsg != "" {
				parseErr = errMsg
				parseErrKind = lua.PermissionDenied
				return
			}
		}

		req := httpapi.AcquireRequestCmd()
		req.Method = method
		req.URL = urlStr
		if len(opts.headers) > 0 {
			req.Headers = make(map[string][]string, len(opts.headers))
			for k, v := range opts.headers {
				req.Headers[k] = []string{v}
			}
		}
		req.Body = opts.body
		req.Timeout = opts.timeout
		req.UnixSocket = opts.unixSocket
		req.Query = opts.query
		req.Cookies = opts.cookies
		req.Form = opts.form
		req.BasicAuthUser = opts.basicAuthUser
		req.BasicAuthPass = opts.basicAuthPass
		req.MaxResponseBody = opts.maxResponseBody
		req.TLS = opts.tls
		req.OverlayNetwork = opts.overlayNetwork

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
		l.Push(lua.NewLuaError(l, parseErr).WithKind(parseErrKind).WithRetryable(false))
		return 2
	}

	l.Push(yield)
	return -1
}

// parseTLSConfig extracts TLS configuration from a Lua table.
// Returns nil if the table contains no recognized TLS fields.
func parseTLSConfig(tbl *lua.LTable) *httpapi.TLSConfig {
	cfg := &httpapi.TLSConfig{}
	hasFields := false

	if cert := tbl.RawGetString("cert"); cert.Type() == lua.LTString {
		cfg.CertPEM = []byte(cert.String())
		hasFields = true
	}
	if key := tbl.RawGetString("key"); key.Type() == lua.LTString {
		cfg.KeyPEM = []byte(key.String())
		hasFields = true
	}
	if ca := tbl.RawGetString("ca"); ca.Type() == lua.LTString {
		cfg.CAPEM = []byte(ca.String())
		hasFields = true
	}
	if serverName := tbl.RawGetString("server_name"); serverName.Type() == lua.LTString {
		cfg.ServerName = serverName.String()
		hasFields = true
	}
	if insecure := tbl.RawGetString("insecure_skip_verify"); insecure.Type() == lua.LTBool {
		cfg.InsecureSkipVerify = bool(insecure.(lua.LBool))
		hasFields = true
	}

	if !hasFields {
		return nil
	}
	return cfg
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

	if timeout := tbl.RawGetString("timeout"); timeout != lua.LNil {
		if d, ok := parseDuration(timeout); ok {
			opts.timeout = d
		}
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
				if contentType := ft.RawGetString("content_type"); contentType.Type() == lua.LTString {
					f.contentType = contentType.String()
				} else {
					f.contentType = "application/octet-stream"
				}

				// Support both content (string) and reader (io.Reader)
				if content := ft.RawGetString("content"); content.Type() == lua.LTString {
					f.data = []byte(content.String())
				} else if reader := ft.RawGetString("reader"); reader.Type() == lua.LTUserData {
					f.reader = reader.(*lua.LUserData).Value
				}

				if f.fieldName != "" && (len(f.data) > 0 || f.reader != nil) {
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

	if maxBody := tbl.RawGetString("max_response_body"); maxBody.Type() == lua.LTNumber || maxBody.Type() == lua.LTInteger {
		opts.maxResponseBody = int64(lua.LVAsNumber(maxBody))
	}

	if tlsVal := tbl.RawGetString("tls"); tlsVal.Type() == lua.LTTable {
		opts.tls = parseTLSConfig(tlsVal.(*lua.LTable))
	}

	// Overlay network
	if overlay := tbl.RawGetString("overlay_network"); overlay.Type() == lua.LTString {
		opts.overlayNetwork = overlay.String()
	}

	return opts
}
