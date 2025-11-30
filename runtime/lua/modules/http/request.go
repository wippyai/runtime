package http

import (
	"fmt"
	"io"
	basehttp "net/http"
	"strings"

	httpservice "github.com/wippyai/runtime/api/service/http"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	jsonmod "github.com/wippyai/runtime/runtime/lua/modules/json"
	lua "github.com/yuin/gopher-lua"
)

type Request struct {
	request *basehttp.Request
}

var requestMethods = map[string]lua.LGFunction{
	"method":          requestMethod,
	"path":            requestPath,
	"query":           requestQuery,
	"query_params":    requestQueryParams,
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
	"param":           requestParam,
	"params":          requestParams,
}

func newRequest(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context available"))
		return 2
	}

	reqCtx, ok := httpservice.GetRequestContext(ctx)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("no HTTP request context found"))
		return 2
	}

	value.NewUserData(l, &Request{request: reqCtx.Request()}, requestMetatable)
	l.Push(lua.LNil)
	return 2
}

func checkRequest(l *lua.LState, idx int) *Request {
	ud := l.CheckUserData(idx)
	if req, ok := ud.Value.(*Request); ok {
		return req
	}
	l.ArgError(idx, "http.Request expected")
	return nil
}

func requestMethod(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	l.Push(lua.LString(req.request.Method))
	l.Push(lua.LNil)
	return 2
}

func requestPath(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	l.Push(lua.LString(req.request.URL.Path))
	l.Push(lua.LNil)
	return 2
}

func requestQuery(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	key := l.CheckString(2)
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

func requestQueryParams(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	tbl := l.CreateTable(0, len(req.request.URL.Query()))
	for k, vals := range req.request.URL.Query() {
		if len(vals) > 0 {
			tbl.RawSetString(k, lua.LString(strings.Join(vals, ",")))
		}
	}
	l.Push(tbl)
	l.Push(lua.LNil)
	return 2
}

func requestHeader(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	key := l.CheckString(2)
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

func requestContentType(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	ct := req.request.Header.Get("Content-Type")
	if ct == "" {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		return 2
	}
	l.Push(lua.LString(ct))
	l.Push(lua.LNil)
	return 2
}

func requestContentLength(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	l.Push(lua.LNumber(req.request.ContentLength))
	l.Push(lua.LNil)
	return 2
}

func requestHost(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	l.Push(lua.LString(req.request.Host))
	l.Push(lua.LNil)
	return 2
}

func requestRemoteAddr(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	l.Push(lua.LString(req.request.RemoteAddr))
	l.Push(lua.LNil)
	return 2
}

func requestBody(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	if req.request.Body == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no body"))
		return 2
	}
	body, err := io.ReadAll(req.request.Body)
	defer func() { _ = req.request.Body.Close() }()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to read body: %v", err)))
		return 2
	}
	l.Push(lua.LString(body))
	l.Push(lua.LNil)
	return 2
}

func requestBodyJSON(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	if req.request.Body == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no body"))
		return 2
	}
	body, err := io.ReadAll(req.request.Body)
	defer func() { _ = req.request.Body.Close() }()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to read body: %v", err)))
		return 2
	}
	val, err := jsonmod.Decode(body)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("invalid JSON: %v", err)))
		return 2
	}
	l.Push(val)
	l.Push(lua.LNil)
	return 2
}

func requestHasBody(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	has := req.request.Body != nil && req.request.ContentLength > 0
	l.Push(lua.LBool(has))
	l.Push(lua.LNil)
	return 2
}

func requestAccepts(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	ct := l.CheckString(2)
	accept := req.request.Header.Get("Accept")
	if accept == "" {
		l.Push(lua.LFalse)
		l.Push(lua.LNil)
		return 2
	}
	for _, a := range strings.Split(accept, ",") {
		a = strings.TrimSpace(a)
		if a == ct || a == "*/*" {
			l.Push(lua.LTrue)
			l.Push(lua.LNil)
			return 2
		}
	}
	l.Push(lua.LFalse)
	l.Push(lua.LNil)
	return 2
}

func requestIsContentType(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	expected := l.CheckString(2)
	actual := req.request.Header.Get("Content-Type")
	l.Push(lua.LBool(strings.HasPrefix(actual, expected)))
	l.Push(lua.LNil)
	return 2
}

func requestParam(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	name := l.CheckString(2)
	routeInfo, ok := httpservice.GetRouteInfo(req.request.Context())
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("no route parameters found"))
		return 2
	}
	val, exists := routeInfo.Params[name]
	if !exists {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		return 2
	}
	l.Push(lua.LString(val))
	l.Push(lua.LNil)
	return 2
}

func requestParams(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	routeInfo, ok := httpservice.GetRouteInfo(req.request.Context())
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("no route parameters found"))
		return 2
	}
	params := l.CreateTable(0, len(routeInfo.Params))
	for k, v := range routeInfo.Params {
		params.RawSetString(k, lua.LString(v))
	}
	l.Push(params)
	l.Push(lua.LNil)
	return 2
}

func requestToString(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	l.Push(lua.LString(fmt.Sprintf("http.Request{method=%s, path=%s}",
		req.request.Method, req.request.URL.Path)))
	return 1
}
