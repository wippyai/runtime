package httpctx

import (
	"fmt"
	"github.com/ponyruntime/go-lua"
	"github.com/ponyruntime/pony/api/service/http"
	basehttp "net/http"
)

// Response represents a Lua userdata object wrapping http.ResponseWriter
type Response struct {
	writer basehttp.ResponseWriter
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
	"write_json":       responseWriteJSON,
	"set_content_type": responseSetContentType,
	"write_event":      responseWriteEvent,
}

// responseSetStatus sets the HTTP status code
func responseSetStatus(l *lua.LState) int {
	resp, err := checkResponse(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	code := l.CheckInt(2)
	if code < 100 || code > 599 {
		l.ArgError(2, "invalid status code (must be between 100 and 599)")
		return 0
	}

	resp.writer.WriteHeader(code)
	return 0
}

// responseSetHeader sets a response header
func responseSetHeader(l *lua.LState) int {
	resp, err := checkResponse(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	key := l.CheckString(2)
	if key == "" {
		l.ArgError(2, "header key cannot be empty")
		return 0
	}

	value := l.CheckString(3)
	resp.writer.Header().Set(key, value)
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

	// Ensure proper content type
	resp.writer.Header().Set("Content-Type", "application/json")

	data := l.CheckString(2)
	_, writeErr := resp.writer.Write([]byte(data))
	if writeErr != nil {
		l.Push(lua.LString(fmt.Sprintf("JSON write error: %v", writeErr)))
		return 1
	}

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

	contentType := l.CheckString(2)
	if contentType == "" {
		l.ArgError(2, "content type cannot be empty")
		return 0
	}

	resp.writer.Header().Set("Content-Type", contentType)
	return 0
}

// responseWriteEvent writes a Server-Sent Event
func responseWriteEvent(l *lua.LState) int {
	resp, err := checkResponse(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	event := l.CheckString(2)
	data := l.CheckString(3)

	// Set SSE headers if not already set // todo: double headers??
	resp.writer.Header().Set("Content-Type", "text/event-stream")
	resp.writer.Header().Set("Cache-Control", "no-cache")
	resp.writer.Header().Set("Connection", "keep-alive")

	// Write SSE format
	_, writeErr := fmt.Fprintf(resp.writer, "event: %s\ndata: %s\n\n", event, data)
	if writeErr != nil {
		l.Push(lua.LString(fmt.Sprintf("event write error: %v", writeErr)))
		return 1
	}

	// Flush if the response writer supports it
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
		writer: reqCtx.ResponseWriter(),
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
