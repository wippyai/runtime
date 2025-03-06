package http

import (
	"github.com/ponyruntime/pony/runtime/lua/modules/stream"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Module represents the http Lua module
type Module struct {
	log *zap.Logger
}

func getTransferConstants() map[string]string {
	return map[string]string{
		"CHUNKED": "chunked", // Chunked transfer encoding
		"SSE":     "sse",     // Server-sent events
	}
}

// NewHTTPAPIModule creates a new HTTP context module
func NewHTTPAPIModule(log *zap.Logger) *Module {
	return &Module{log: log}
}

// Name returns the module name
func (m *Module) Name() string {
	return "http"
}

// Loader registers the module functions and constants
func (m *Module) Loader(l *lua.LState) int {
	// Spawn module table
	mod := l.NewTable()

	// Register constants
	m.registerConstants(l, mod)

	// a helper class is always needed
	stream.RegisterStream(l)

	// Register Request type and methods
	registerRequest(l, mod)

	// Register Response type and methods
	registerResponse(l, mod)

	// Set the module
	l.Push(mod)
	return 1
}

// registerConstants registers all constant tables with explanatory comments
func (m *Module) registerConstants(l *lua.LState, mod *lua.LTable) {
	// METHOD table - HTTP methods
	methodTbl := l.NewTable()
	methods := map[string]string{
		"GET":     "GET",
		"POST":    "POST",
		"PUT":     "PUT",
		"DELETE":  "DELETE",
		"PATCH":   "PATCH",
		"HEAD":    "HEAD",
		"OPTIONS": "OPTIONS",
	}
	for name, val := range methods {
		methodTbl.RawSetString(name, lua.LString(val))
	}
	mod.RawSetString("METHOD", methodTbl)

	// STATUS table - HTTP status codes
	statusTbl := l.NewTable()
	statuses := map[string]int{
		"OK":             200, // Success
		"CREATED":        201, // Resource created
		"NO_CONTENT":     204, // Success with no body
		"BAD_REQUEST":    400, // Client error
		"UNAUTHORIZED":   401, // Authentication required
		"NOT_FOUND":      404, // Resource not found
		"INTERNAL_ERROR": 500, // Server error
	}
	for name, value := range statuses {
		statusTbl.RawSetString(name, lua.LNumber(value))
	}
	mod.RawSetString("STATUS", statusTbl)

	// CONTENT table - Content types
	contentTbl := l.NewTable()
	contentTypes := map[string]string{
		"JSON":      "application/json",
		"FORM":      "application/x-www-form-urlencoded",
		"MULTIPART": "multipart/form-data",
		"TEXT":      "text/plain",
		"STREAM":    "application/octet-stream",
	}
	for name, val := range contentTypes {
		contentTbl.RawSetString(name, lua.LString(val))
	}
	mod.RawSetString("CONTENT", contentTbl)

	// TRANSFER table - Transfer encoding types
	transferTbl := l.NewTable()
	for name, val := range getTransferConstants() {
		transferTbl.RawSetString(name, lua.LString(val))
	}
	mod.RawSetString("TRANSFER", transferTbl)

	// ERROR table - Error types
	errorTbl := l.NewTable()
	errorTypes := map[string]string{
		"PARSE_FAILED":  "PARSE_FAILED",  // Body parsing failed
		"INVALID_STATE": "INVALID_STATE", // Operation not valid in current state
		"WRITE_FAILED":  "WRITE_FAILED",  // Response write failed
		"STREAM_ERROR":  "STREAM_ERROR",  // Streaming operation failed
	}
	for name, val := range errorTypes {
		errorTbl.RawSetString(name, lua.LString(val))
	}
	mod.RawSetString("ERROR", errorTbl)
}
