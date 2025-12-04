package http

import (
	"fmt"
	basehttp "net/http"

	httpservice "github.com/wippyai/runtime/api/service/http"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	jsonmod "github.com/wippyai/runtime/runtime/lua/modules/json"
	lua "github.com/yuin/gopher-lua"
)

type Response struct {
	writer       basehttp.ResponseWriter
	rCtx         *httpservice.RequestContext
	headersSent  bool
	transferMode string
}

var responseMethods = map[string]lua.LGFunction{
	"set_status":       responseSetStatus,
	"set_header":       responseSetHeader,
	"write":            responseWrite,
	"flush":            responseFlush,
	"write_json":       responseWriteJSON,
	"set_content_type": responseSetContentType,
	"write_event":      responseWriteEvent,
	"set_transfer":     responseSetTransfer,
}

func pushResponse(l *lua.LState, res *Response) {
	value.PushTypedUserData(l, res, responseTypeName)
}

func newResponse(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		err := lua.NewLuaError(l, "no context available").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	reqCtx, ok := httpservice.GetRequestContext(ctx)
	if !ok {
		err := lua.NewLuaError(l, "no HTTP request context found").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	pushResponse(l, &Response{writer: reqCtx.ResponseWriter(), rCtx: reqCtx})
	l.Push(lua.LNil)
	return 2
}

func checkResponse(l *lua.LState, idx int) *Response { //nolint:unparam
	ud := l.CheckUserData(idx)
	if res, ok := ud.Value.(*Response); ok {
		return res
	}
	l.ArgError(idx, "http.Response expected")
	return nil
}

func responseSetStatus(l *lua.LState) int {
	res := checkResponse(l, 1)
	if res == nil {
		return 0
	}
	if res.headersSent {
		err := lua.NewLuaError(l, "cannot set status after headers are sent").
			WithKind(lua.KindInvalid).
			WithRetryable(false)
		l.Push(err)
		return 1
	}
	code := l.CheckInt(2)
	res.writer.WriteHeader(code)
	res.headersSent = true
	res.rCtx.MarkHandled()
	l.Push(lua.LNil)
	return 1
}

func responseSetHeader(l *lua.LState) int {
	res := checkResponse(l, 1)
	if res == nil {
		return 0
	}
	if res.headersSent {
		err := lua.NewLuaError(l, "cannot set headers after they are sent").
			WithKind(lua.KindInvalid).
			WithRetryable(false)
		l.Push(err)
		return 1
	}
	name := l.CheckString(2)
	val := l.CheckString(3)
	res.writer.Header().Set(name, val)
	res.rCtx.MarkHandled()
	l.Push(lua.LNil)
	return 1
}

func responseWrite(l *lua.LState) int {
	res := checkResponse(l, 1)
	if res == nil {
		return 0
	}
	data := l.CheckString(2)
	_, err := res.writer.Write([]byte(data))
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "write failed").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		l.Push(luaErr)
		return 1
	}
	res.headersSent = true
	res.rCtx.MarkHandled()
	l.Push(lua.LNil)
	return 1
}

func responseFlush(l *lua.LState) int {
	res := checkResponse(l, 1)
	if res == nil {
		return 0
	}
	if flusher, ok := res.writer.(interface{ Flush() }); ok {
		flusher.Flush()
	}
	res.headersSent = true
	res.rCtx.MarkHandled()
	l.Push(lua.LNil)
	return 1
}

func responseWriteJSON(l *lua.LState) int {
	res := checkResponse(l, 1)
	if res == nil {
		return 0
	}
	val := l.Get(2)
	data, err := jsonmod.Encode(val)
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "failed to encode JSON").
			WithKind(lua.KindInvalid).
			WithRetryable(false)
		l.Push(luaErr)
		return 1
	}
	if !res.headersSent {
		res.writer.Header().Set("Content-Type", "application/json")
	}
	_, err = res.writer.Write(data)
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "write failed").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		l.Push(luaErr)
		return 1
	}
	res.headersSent = true
	res.rCtx.MarkHandled()
	l.Push(lua.LNil)
	return 1
}

func responseSetContentType(l *lua.LState) int {
	res := checkResponse(l, 1)
	if res == nil {
		return 0
	}
	if res.headersSent {
		err := lua.NewLuaError(l, "cannot set content type after headers are sent").
			WithKind(lua.KindInvalid).
			WithRetryable(false)
		l.Push(err)
		return 1
	}
	ct := l.CheckString(2)
	res.writer.Header().Set("Content-Type", ct)
	l.Push(lua.LNil)
	return 1
}

func responseWriteEvent(l *lua.LState) int {
	res := checkResponse(l, 1)
	if res == nil {
		return 0
	}

	// Auto-set SSE mode if not already set
	if res.transferMode != "sse" {
		if res.headersSent {
			err := lua.NewLuaError(l, "cannot switch to SSE mode after headers are sent").
				WithKind(lua.KindInvalid).
				WithRetryable(false)
			l.Push(err)
			return 1
		}
		res.writer.Header().Set("Content-Type", "text/event-stream")
		res.writer.Header().Set("Cache-Control", "no-cache")
		res.writer.Header().Set("Connection", "keep-alive")
		res.transferMode = "sse"
	}

	eventTable := l.CheckTable(2)
	if eventTable == nil {
		l.ArgError(2, "expected table for event data")
		return 0
	}

	name := lua.LVAsString(eventTable.RawGetString("name"))
	if name == "" {
		l.ArgError(2, "missing event name")
		return 0
	}

	dataLV := eventTable.RawGetString("data")
	if dataLV == lua.LNil {
		l.ArgError(2, "missing event data")
		return 0
	}

	data, err := jsonmod.Encode(dataLV)
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "failed to marshal event data").
			WithKind(lua.KindInvalid).
			WithRetryable(false)
		l.Push(luaErr)
		return 1
	}

	_, writeErr := fmt.Fprintf(res.writer, "event: %s\ndata: %s\n\n", name, data)
	if writeErr != nil {
		luaErr := lua.WrapErrorWithLua(l, writeErr, "write event failed").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		l.Push(luaErr)
		return 1
	}

	if flusher, ok := res.writer.(interface{ Flush() }); ok {
		flusher.Flush()
	}

	res.headersSent = true
	res.rCtx.MarkHandled()
	l.Push(lua.LNil)
	return 1
}

func responseSetTransfer(l *lua.LState) int {
	res := checkResponse(l, 1)
	if res == nil {
		return 0
	}
	if res.headersSent {
		err := lua.NewLuaError(l, "cannot set transfer mode after headers are sent").
			WithKind(lua.KindInvalid).
			WithRetryable(false)
		l.Push(err)
		return 1
	}
	mode := l.CheckString(2)
	switch mode {
	case "chunked":
		res.writer.Header().Set("Transfer-Encoding", "chunked")
		res.writer.Header().Set("Cache-Control", "no-cache")
		res.transferMode = "chunked"
	case "sse":
		res.writer.Header().Set("Content-Type", "text/event-stream")
		res.writer.Header().Set("Cache-Control", "no-cache")
		res.writer.Header().Set("Connection", "keep-alive")
		res.transferMode = "sse"
	default:
		l.ArgError(2, "invalid transfer type")
		return 0
	}
	l.Push(lua.LNil)
	return 1
}

func responseToString(l *lua.LState) int {
	l.Push(lua.LString("http.Response{}"))
	return 1
}
