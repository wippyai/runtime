package httpclient

import (
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	"net/http"

	lua "github.com/yuin/gopher-lua"
)

// luaHTTPResponseTypeName is the type name for HTTP response userdata in Lua
const luaHTTPResponseTypeName = "http.response"

// luaHTTPResponse represents an HTTP response in Lua
type luaHTTPResponse struct {
	res      *http.Response
	body     lua.LString
	bodySize int
	stream   *lua.LUserData
}

// registerHTTPResponseType registers the HTTP response type and its metatable
// in the Lua state.
func registerHTTPResponseType(module *lua.LTable, l *lua.LState) {
	mt := l.NewTypeMetatable(luaHTTPResponseTypeName)
	l.SetField(mt, "__index", l.NewFunction(httpResponseIndex))
	l.SetField(module, "http.Response", mt)
}

// newResponse creates a new HTTP response userdata with the given response,
// body, and size.
func newResponse(res *http.Response, body *[]byte, bodySize int, l *lua.LState) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = &luaHTTPResponse{
		res:      res,
		body:     lua.LString(*body),
		bodySize: bodySize,
	}
	ud.Metatable = value.GetTypeMetatable(l, luaHTTPResponseTypeName)
	return ud
}

// newResponseWithStream creates a new HTTP response with a stream
func newResponseWithStream(res *http.Response, stream *lua.LUserData, l *lua.LState) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = &luaHTTPResponse{
		res:    res,
		stream: stream,
	}
	ud.Metatable = value.GetTypeMetatable(l, luaHTTPResponseTypeName)
	return ud
}

// checkHTTPResponse checks if the value at index is an HTTP response and returns it.
// Returns nil and raises an error if the value is not an HTTP response.
func checkHTTPResponse(l *lua.LState) *luaHTTPResponse {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*luaHTTPResponse); ok {
		return v
	}
	l.ArgError(1, "http.Response expected")
	return nil
}

// httpResponseIndex implements the index metamethod for HTTP response objects.
// This allows accessing response properties like response.headers, response.body, etc.
func httpResponseIndex(l *lua.LState) int {
	res := checkHTTPResponse(l)
	if res == nil {
		return 0
	}

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
	case "stream":
		return httpResponseStream(res, l)
	default:
		l.Push(lua.LNil)
		return 1
	}
}

// httpResponseHeaders returns a table of response headers.
// Returns nil and an error message if header access fails.
func httpResponseHeaders(res *luaHTTPResponse, l *lua.LState) int {
	if res.res == nil || res.res.Header == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("invalid response headers"))
		return 2
	}

	headers := l.CreateTable(len(res.res.Header), 0)
	for key := range res.res.Header {
		val := res.res.Header.Get(key)
		if val != "" {
			headers.RawSetString(http.CanonicalHeaderKey(key), lua.LString(val))
		}
	}

	l.Push(headers)
	return 1
}

// httpResponseCookies returns a table of response cookies.
// Returns nil and an error message if cookie access fails.
func httpResponseCookies(res *luaHTTPResponse, l *lua.LState) int {
	if res.res == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("invalid response"))
		return 2
	}

	cookies := l.CreateTable(len(res.res.Cookies()), 0)
	for _, cookie := range res.res.Cookies() {
		if cookie != nil && cookie.Name != "" {
			cookies.RawSetString(cookie.Name, lua.LString(cookie.Value))
		}
	}

	l.Push(cookies)
	return 1
}

// httpResponseStatusCode returns the response status code.
func httpResponseStatusCode(res *luaHTTPResponse, l *lua.LState) int {
	l.Push(lua.LNumber(res.res.StatusCode))
	return 1
}

// httpResponseURL returns the response URL.
func httpResponseURL(res *luaHTTPResponse, l *lua.LState) int {
	l.Push(lua.LString(res.res.Request.URL.String()))
	return 1
}

// httpResponseBody returns the response body as a string.
// Returns nil if streaming is used.
func httpResponseBody(res *luaHTTPResponse, l *lua.LState) int {
	if res.stream != nil {
		l.Push(lua.LNil)
	} else {
		l.Push(res.body)
	}
	return 1
}

// httpResponseBodySize returns the size of the response body in bytes.
// Returns -1 if streaming is used.
func httpResponseBodySize(res *luaHTTPResponse, l *lua.LState) int {
	if res.stream != nil {
		l.Push(lua.LNumber(-1))
	} else {
		l.Push(lua.LNumber(res.bodySize))
	}
	return 1
}

// httpResponseStream returns the stream userdata associated with the response.
func httpResponseStream(res *luaHTTPResponse, l *lua.LState) int {
	if res.stream != nil {
		l.Push(res.stream)
	} else {
		l.Push(lua.LNil)
	}
	return 1
}
