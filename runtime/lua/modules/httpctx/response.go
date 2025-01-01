package httpctx

import (
	"fmt"
	"github.com/ponyruntime/go-lua"
	"github.com/ponyruntime/pony/api/service/http"
	"github.com/ponyruntime/pony/runtime/lua/modules/json"
	basehttp "net/http"
)

// Response represents a Lua userdata object wrapping http.ResponseWriter
type Response struct {
	writer       basehttp.ResponseWriter
	rCtx         *http.RequestContext
	headersSent  bool
	transferMode string
}

// checkResponse gets and verifies Response userdata from Lua state
func checkResponse(l *lua.LState, n int) (*Response, error) {
	ud := l.CheckUserData(n)
	if ud == nil {
		return nil, fmt.Errorf("argument %d must be a Response", n)
	}

	if resp, ok := ud.Value.(*Response); ok {
		return resp, nil
	}
	return nil, fmt.Errorf("argument %d must be a Response, got %T", n, ud.Value)
}

// Declare response methods map
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

// responseSetStatus sets the HTTP status code
func responseSetStatus(l *lua.LState) int {
	resp, err := checkResponse(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	if resp.headersSent {
		l.Push(lua.LString("cannot set status after headers are sent"))
		return 1
	}

	code := l.CheckInt(2)
	if code < 100 || code > 599 {
		l.ArgError(2, "invalid status code (must be between 100 and 599)")
		return 0
	}

	resp.writer.WriteHeader(code)
	resp.headersSent = true
	resp.rCtx.MarkHandled()

	return 0
}

// responseSetHeader sets a response header
func responseSetHeader(l *lua.LState) int {
	resp, err := checkResponse(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	if resp.headersSent {
		l.Push(lua.LString("cannot set headers after they are sent"))
		return 1
	}

	key := l.CheckString(2)
	if key == "" {
		l.ArgError(2, "header key cannot be empty")
		return 0
	}

	value := l.CheckString(3)
	resp.writer.Header().Set(key, value)
	resp.rCtx.MarkHandled()

	return 0
}

// responseWrite writes raw data to the response
func responseWrite(l *lua.LState) int {
	resp, err := checkResponse(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	data := l.CheckString(2)
	_, writeErr := resp.writer.Write([]byte(data))
	if writeErr != nil {
		l.Push(lua.LString(fmt.Sprintf("write error: %v", writeErr)))
		return 1
	}

	resp.headersSent = true
	resp.rCtx.MarkHandled()

	l.Push(lua.LNil)
	return 1
}

// responseFlush flushes the response writer
func responseFlush(l *lua.LState) int {
	resp, err := checkResponse(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	resp.writer.(basehttp.Flusher).Flush()

	resp.headersSent = true
	resp.rCtx.MarkHandled()
	l.Push(lua.LNil)
	return 1
}

// responseWriteJSON writes JSON data to the response
func responseWriteJSON(l *lua.LState) int {
	resp, err := checkResponse(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	// Get the Lua value to encode
	luaValue := l.CheckAny(2)

	// Encode Lua value to JSON
	jsonData, err := json.Encode(luaValue)
	if err != nil {
		l.Push(lua.LString(fmt.Sprintf("JSON encode error: %v", err)))
		return 1
	}

	if !resp.headersSent {
		resp.writer.Header().Set("Content-Type", "application/json")
	}

	_, writeErr := resp.writer.Write(jsonData)
	if writeErr != nil {
		l.Push(lua.LString(fmt.Sprintf("JSON write error: %v", writeErr)))
		return 1
	}

	resp.headersSent = true
	resp.rCtx.MarkHandled()

	l.Push(lua.LNil)
	return 1
}

// responseSetContentType sets the Content-Type header
func responseSetContentType(l *lua.LState) int {
	resp, err := checkResponse(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	if resp.headersSent {
		l.Push(lua.LString("cannot set content type after headers are sent"))
		return 1
	}

	contentType := l.CheckString(2)
	if contentType == "" {
		l.ArgError(2, "content type cannot be empty")
		return 0
	}

	resp.writer.Header().Set("Content-Type", contentType)
	return 0
}

// responseSetTransfer implements transfer encoding settings
func responseSetTransfer(l *lua.LState) int {
	resp, err := checkResponse(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	if resp.headersSent {
		l.Push(lua.LString("cannot set transfer mode after headers are sent"))
		return 1
	}

	transferType := l.CheckString(2)

	switch transferType {
	case transferConstants["CHUNKED"]:
		resp.writer.Header().Set("Transfer-Encoding", "chunked")
		resp.transferMode = transferConstants["CHUNKED"]

	case transferConstants["SSE"]:
		resp.writer.Header().Set("Content-Type", "text/event-stream")
		resp.writer.Header().Set("Cache-Control", "no-cache")
		resp.writer.Header().Set("Connection", "keep-alive")
		resp.transferMode = transferConstants["SSE"]

	default:
		l.ArgError(2, "invalid transfer type")
		return 0
	}

	return 0
}

// responseWriteEvent writes a Server-Sent Event
func responseWriteEvent(l *lua.LState) int {
	resp, err := checkResponse(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	// Check if transfer mode is SSE
	if resp.transferMode != transferConstants["SSE"] {
		if resp.headersSent {
			l.Push(lua.LString("cannot switch to SSE mode after headers are sent"))
			return 1
		}
		// Auto-set SSE mode if not set
		resp.writer.Header().Set("Content-Type", "text/event-stream")
		resp.writer.Header().Set("Cache-Control", "no-cache")
		resp.writer.Header().Set("Connection", "keep-alive")
		resp.transferMode = transferConstants["SSE"]
	}

	eventTable := l.CheckTable(2)
	if eventTable == nil {
		l.ArgError(2, "expected table for event data")
		return 0
	}

	// Get event name and data
	name := lua.LVAsString(l.GetField(eventTable, "name"))
	if name == "" {
		l.ArgError(2, "missing event name")
		return 0
	}

	dataLV := l.GetField(eventTable, "data")
	if dataLV == lua.LNil {
		l.ArgError(2, "missing event data")
		return 0
	}

	data, err := json.Encode(dataLV)
	if err != nil {
		l.ArgError(2, fmt.Sprintf("failed to marshal event data: %v", err))
		return 0
	}

	// Write SSE format
	_, writeErr := fmt.Fprintf(resp.writer, "event: %s\ndata: %s\n\n", name, data)
	if writeErr != nil {
		l.Push(lua.LString(fmt.Sprintf("event write error: %v", writeErr)))
		return 1
	}

	resp.headersSent = true
	resp.rCtx.MarkHandled()

	// Flush if supported
	if f, ok := resp.writer.(basehttp.Flusher); ok {
		f.Flush()
	}

	l.Push(lua.LNil)
	return 1
}

// responseToString implements the __tostring metamethod for Response
func responseToString(l *lua.LState) int {
	l.Push(lua.LString("http.Response"))
	return 1
}

// newResponse creates a new Response from the context
func newResponse(l *lua.LState) int {
	// Get HTTP context from Lua state context
	ctx := l.Context()
	if ctx == nil {
		l.ArgError(1, "no context available")
		return 0
	}

	// Get HTTP request context
	val := ctx.Value(http.RequestCtx)
	if val == nil {
		l.ArgError(1, "no HTTP request context found")
		return 0
	}

	reqCtx, ok := val.(*http.RequestContext)
	if !ok {
		l.ArgError(1, "invalid HTTP request context type")
		return 0
	}

	// Create response userdata
	ud := l.NewUserData()
	ud.Value = &Response{
		writer:       reqCtx.ResponseWriter(),
		rCtx:         reqCtx,
		headersSent:  false,
		transferMode: "",
	}

	l.SetMetatable(ud, l.GetTypeMetatable("Response"))
	l.Push(ud)
	return 1
}

// registerResponse registers the Response type and its methods
func registerResponse(l *lua.LState, mod *lua.LTable) {
	mt := l.NewTypeMetatable("Response")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), responseMethods))
	l.SetField(mt, "__tostring", l.NewFunction(responseToString))

	// Register constructor
	l.SetField(mod, "response", l.NewFunction(newResponse))
}
