package http

import (
	"net/http"

	"github.com/ponyruntime/go-lua"
)

const luaHTTPResponseTypeName = "http.response"

type luaHTTPResponse struct {
	res      *http.Response
	body     lua.LString
	bodySize int
}

func registerHTTPResponseType(module *lua.LTable, l *lua.LState) {
	mt := l.NewTypeMetatable(luaHTTPResponseTypeName)
	l.SetField(mt, "__index", l.NewFunction(httpResponseIndex))

	l.SetField(module, "response", mt)
}

func newHTTPResponse(res *http.Response, body *[]byte, bodySize int, l *lua.LState) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = &luaHTTPResponse{
		res:      res,
		body:     lua.LString(*body),
		bodySize: bodySize,
	}
	l.SetMetatable(ud, l.GetTypeMetatable(luaHTTPResponseTypeName))
	return ud
}

func checkHTTPResponse(l *lua.LState) *luaHTTPResponse {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*luaHTTPResponse); ok {
		return v
	}
	l.ArgError(1, "http.response expected")
	return nil
}

func httpResponseIndex(l *lua.LState) int {
	res := checkHTTPResponse(l)

	switch l.CheckString(2) {
	case "headers":
		return httpResponseHeaders(res, l)
	case "cookies":
		return httpResponseCookies(res, l)
	case "status_code":
		return httpResponseStatusCode(res, l)
	case "url":
		return httpResponseURL(res, l)
	case "body":
		return httpResponseBody(res, l)
	case "body_size":
		return httpResponseBodySize(res, l)
	}

	return 0
}

func httpResponseHeaders(res *luaHTTPResponse, l *lua.LState) int {
	headers := l.NewTable()

	for key := range res.res.Header {
		headers.RawSetString(key, lua.LString(res.res.Header.Get(key)))
	}

	l.Push(headers)
	return 1
}

func httpResponseCookies(res *luaHTTPResponse, l *lua.LState) int {
	cookies := l.NewTable()
	for _, cookie := range res.res.Cookies() {
		cookies.RawSetString(cookie.Name, lua.LString(cookie.Value))
	}
	l.Push(cookies)
	return 1
}

func httpResponseStatusCode(res *luaHTTPResponse, l *lua.LState) int {
	l.Push(lua.LNumber(res.res.StatusCode))
	return 1
}

func httpResponseURL(res *luaHTTPResponse, l *lua.LState) int {
	l.Push(lua.LString(res.res.Request.URL.String()))
	return 1
}

func httpResponseBody(res *luaHTTPResponse, l *lua.LState) int {
	l.Push(res.body)
	return 1
}

func httpResponseBodySize(res *luaHTTPResponse, l *lua.LState) int {
	l.Push(lua.LNumber(res.bodySize))
	return 1
}
