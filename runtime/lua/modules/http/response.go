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
	writer basehttp.ResponseWriter
	rCtx   *httpservice.RequestContext
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

func newResponse(l *lua.LState) int {
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

	value.PushUserData(l, &Response{writer: reqCtx.ResponseWriter(), rCtx: reqCtx}, responseMetatable)
	l.Push(lua.LNil)
	return 2
}

func checkResponse(l *lua.LState, idx int) *Response {
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
	code := l.CheckInt(2)
	res.writer.WriteHeader(code)
	res.rCtx.MarkHandled()
	return 0
}

func responseSetHeader(l *lua.LState) int {
	res := checkResponse(l, 1)
	if res == nil {
		return 0
	}
	name := l.CheckString(2)
	val := l.CheckString(3)
	res.writer.Header().Set(name, val)
	return 0
}

func responseWrite(l *lua.LState) int {
	res := checkResponse(l, 1)
	if res == nil {
		return 0
	}
	data := l.CheckString(2)
	_, err := res.writer.Write([]byte(data))
	if err != nil {
		l.Push(lua.LString(err.Error()))
		return 1
	}
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
		l.Push(lua.LString(fmt.Sprintf("failed to encode JSON: %v", err)))
		return 1
	}
	res.writer.Header().Set("Content-Type", "application/json")
	_, err = res.writer.Write(data)
	if err != nil {
		l.Push(lua.LString(err.Error()))
		return 1
	}
	res.rCtx.MarkHandled()
	l.Push(lua.LNil)
	return 1
}

func responseSetContentType(l *lua.LState) int {
	res := checkResponse(l, 1)
	if res == nil {
		return 0
	}
	ct := l.CheckString(2)
	res.writer.Header().Set("Content-Type", ct)
	return 0
}

func responseWriteEvent(l *lua.LState) int {
	res := checkResponse(l, 1)
	if res == nil {
		return 0
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
		l.ArgError(2, fmt.Sprintf("failed to marshal event data: %v", err))
		return 0
	}

	_, writeErr := fmt.Fprintf(res.writer, "event: %s\ndata: %s\n\n", name, data)
	if writeErr != nil {
		l.Push(lua.LString(writeErr.Error()))
		return 1
	}

	if flusher, ok := res.writer.(interface{ Flush() }); ok {
		flusher.Flush()
	}

	res.rCtx.MarkHandled()
	l.Push(lua.LNil)
	return 1
}

func responseSetTransfer(l *lua.LState) int {
	res := checkResponse(l, 1)
	if res == nil {
		return 0
	}
	mode := l.CheckString(2)
	switch mode {
	case "chunked":
		res.writer.Header().Set("Transfer-Encoding", "chunked")
		res.writer.Header().Set("Cache-Control", "no-cache")
	case "sse":
		res.writer.Header().Set("Content-Type", "text/event-stream")
		res.writer.Header().Set("Cache-Control", "no-cache")
		res.writer.Header().Set("Connection", "keep-alive")
	default:
		l.ArgError(2, "invalid transfer type")
		return 0
	}
	return 0
}

func responseToString(l *lua.LState) int {
	l.Push(lua.LString("http.Response{}"))
	return 1
}
