package httpctx

import (
	"github.com/ponyruntime/go-lua"
	"github.com/ponyruntime/pony/runtime/lua/modules/stream"
	"go.uber.org/zap"
)

// Module represents the httpctx Lua module
type Module struct {
	log *zap.Logger
}

// Method constants as defined in spec
var methodConstants = map[string]string{
	"GET":     "GET",
	"POST":    "POST",
	"PUT":     "PUT",
	"DELETE":  "DELETE",
	"PATCH":   "PATCH",
	"HEAD":    "HEAD",
	"OPTIONS": "OPTIONS",
}

// Status code constants as defined in spec
var statusConstants = map[string]int{
	"OK":             200, // Success
	"CREATED":        201, // Resource created
	"NO_CONTENT":     204, // Success with no body
	"BAD_REQUEST":    400, // Client error
	"UNAUTHORIZED":   401, // Authentication required
	"NOT_FOUND":      404, // Resource not found
	"INTERNAL_ERROR": 500, // Server error
}

// Content type constants as defined in spec
var contentConstants = map[string]string{
	"JSON":      "application/json",
	"FORM":      "application/x-www-form-urlencoded",
	"MULTIPART": "multipart/form-data",
	"TEXT":      "text/plain",
	"STREAM":    "application/octet-stream",
}

// Transfer type constants as defined in spec
var transferConstants = map[string]string{
	"CHUNKED": "chunked", // Chunked transfer encoding
	"SSE":     "sse",     // Server-sent events
}

// Error type constants as defined in spec
var errorConstants = map[string]string{
	"PARSE_FAILED":  "PARSE_FAILED",  // Body parsing failed
	"INVALID_STATE": "INVALID_STATE", // Operation not valid in current state
	"WRITE_FAILED":  "WRITE_FAILED",  // Response write failed
	"STREAM_ERROR":  "STREAM_ERROR",  // Streaming operation failed
}

// NewHttpContext creates a new HTTP context module
func NewHttpContext(log *zap.Logger) *Module {
	return &Module{log: log}
}

// Name returns the module name
func (m *Module) Name() string {
	return "httpctx"
}

// Loader registers the module functions and constants
func (m *Module) Loader(l *lua.LState) int {
	// Create module table
	mod := l.NewTable()

	// Register constants
	m.registerConstants(l, mod)

	// helper class is always needed
	stream.RegisterStream(l, mod)

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
	for name, value := range methodConstants {
		l.SetField(methodTbl, name, lua.LString(value))
	}
	l.SetField(mod, "METHOD", methodTbl)

	// STATUS table - HTTP status codes
	statusTbl := l.NewTable()
	for name, value := range statusConstants {
		l.SetField(statusTbl, name, lua.LNumber(value))
	}
	l.SetField(mod, "STATUS", statusTbl)

	// CONTENT table - Content types
	contentTbl := l.NewTable()
	for name, value := range contentConstants {
		l.SetField(contentTbl, name, lua.LString(value))
	}
	l.SetField(mod, "CONTENT", contentTbl)

	// TRANSFER table - Transfer encoding types
	transferTbl := l.NewTable()
	for name, value := range transferConstants {
		l.SetField(transferTbl, name, lua.LString(value))
	}
	l.SetField(mod, "TRANSFER", transferTbl)

	// ERROR table - Error types
	errorTbl := l.NewTable()
	for name, value := range errorConstants {
		l.SetField(errorTbl, name, lua.LString(value))
	}
	l.SetField(mod, "ERROR", errorTbl)
}
