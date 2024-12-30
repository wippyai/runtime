package httpctx

import (
	"github.com/ponyruntime/go-lua"
	"go.uber.org/zap"
)

// Module represents the httpctx Lua module
type Module struct {
	log *zap.Logger
}

// Method constants
var methodConstants = map[string]string{
	"GET":     "GET",
	"POST":    "POST",
	"PUT":     "PUT",
	"DELETE":  "DELETE",
	"PATCH":   "PATCH",
	"HEAD":    "HEAD",
	"OPTIONS": "OPTIONS",
}

// Status code constants
var statusConstants = map[string]int{
	"OK":             200,
	"CREATED":        201,
	"NO_CONTENT":     204,
	"BAD_REQUEST":    400,
	"UNAUTHORIZED":   401,
	"NOT_FOUND":      404,
	"INTERNAL_ERROR": 500,
}

// Content type constants
var contentConstants = map[string]string{
	"JSON":      "application/json",
	"FORM":      "application/x-www-form-urlencoded",
	"MULTIPART": "multipart/form-data",
	"TEXT":      "text/plain",
	"STREAM":    "application/octet-stream",
}

// Transfer type constants
var transferConstants = map[string]string{
	"CHUNKED": "chunked",
	"SSE":     "sse",
}

// Error type constants
var errorConstants = map[string]string{
	"PARSE_FAILED":  "PARSE_FAILED",
	"INVALID_STATE": "INVALID_STATE",
	"WRITE_FAILED":  "WRITE_FAILED",
	"STREAM_ERROR":  "STREAM_ERROR",
}

func NewHttpContext(log *zap.Logger) *Module {
	return &Module{log: log}
}

func (m *Module) Name() string {
	return "httpctx"
}

// Loader registers the module functions and constants
func (m *Module) Loader(l *lua.LState) int {
	// Create module table
	mod := l.NewTable()

	// Register constants
	m.registerConstants(l, mod)

	// Register Request type and methods
	registerRequest(l, mod)

	// Register Response type and methods
	registerResponse(l, mod)

	// Set the module
	l.Push(mod)
	return 1
}

// registerConstants registers all constant tables
func (m *Module) registerConstants(l *lua.LState, mod *lua.LTable) {
	// METHOD table
	methodTbl := l.NewTable()
	for name, value := range methodConstants {
		l.SetField(methodTbl, name, lua.LString(value))
	}
	l.SetField(mod, "METHOD", methodTbl)

	// STATUS table
	statusTbl := l.NewTable()
	for name, value := range statusConstants {
		l.SetField(statusTbl, name, lua.LNumber(value))
	}
	l.SetField(mod, "STATUS", statusTbl)

	// CONTENT table
	contentTbl := l.NewTable()
	for name, value := range contentConstants {
		l.SetField(contentTbl, name, lua.LString(value))
	}
	l.SetField(mod, "CONTENT", contentTbl)

	// TRANSFER table
	transferTbl := l.NewTable()
	for name, value := range transferConstants {
		l.SetField(transferTbl, name, lua.LString(value))
	}
	l.SetField(mod, "TRANSFER", transferTbl)

	// ERROR table
	errorTbl := l.NewTable()
	for name, value := range errorConstants {
		l.SetField(errorTbl, name, lua.LString(value))
	}
	l.SetField(mod, "ERROR", errorTbl)
}
