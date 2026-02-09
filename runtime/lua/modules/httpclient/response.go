package httpclient

import (
	lua "github.com/wippyai/go-lua"
)

type httpResponse struct {
	headers    map[string][]string
	cookies    map[string]string
	url        string
	body       []byte
	statusCode int
}

func NewResponse(l *lua.LState, statusCode int, headers map[string][]string, cookies map[string]string, body []byte, url string) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = &httpResponse{
		statusCode: statusCode,
		headers:    headers,
		cookies:    cookies,
		body:       body,
		url:        url,
	}
	ud.Metatable = responseMetatable
	return ud
}

func responseIndex(l *lua.LState) int {
	ud := l.CheckUserData(1)
	res, ok := ud.Value.(*httpResponse)
	if !ok {
		l.Push(lua.LNil)
		return 1
	}

	key := l.CheckString(2)
	switch key {
	case "status_code":
		l.Push(lua.LNumber(res.statusCode))
	case "headers":
		tbl := lua.CreateTable(0, len(res.headers))
		for k, vs := range res.headers {
			if len(vs) > 0 {
				tbl.RawSetString(k, lua.LString(vs[0]))
			}
		}
		l.Push(tbl)
	case "cookies":
		tbl := lua.CreateTable(0, len(res.cookies))
		for k, v := range res.cookies {
			tbl.RawSetString(k, lua.LString(v))
		}
		l.Push(tbl)
	case "body":
		l.Push(lua.LString(res.body))
	case "body_size":
		l.Push(lua.LNumber(len(res.body)))
	case "url":
		l.Push(lua.LString(res.url))
	default:
		l.Push(lua.LNil)
	}
	return 1
}

func responseToString(l *lua.LState) int {
	ud := l.CheckUserData(1)
	res, ok := ud.Value.(*httpResponse)
	if !ok {
		l.Push(lua.LString("httpclient.Response{invalid}"))
		return 1
	}
	l.Push(lua.LString("httpclient.Response{status=" + lua.LNumber(res.statusCode).String() + "}"))
	return 1
}
