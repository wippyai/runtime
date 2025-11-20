package http

import (
	"sync"

	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/modules/stream"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Module represents the http Lua module
type Module struct {
	log         *zap.Logger
	moduleTable *lua.LTable
	once        sync.Once
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
	m.once.Do(func() {
		m.initModuleTable(l)
	})

	l.Push(m.moduleTable)
	return 1
}

// initModuleTable creates and initializes the module table once
func (m *Module) initModuleTable(l *lua.LState) {
	// Register stream helper (only once)
	stream.RegisterStream(l)

	// Register all HTTP types with their methods (only once)
	m.registerMultipartFileType(l)
	m.registerRequestType(l)
	m.registerResponseType(l)

	// Create module table
	mod := l.NewTable()

	// Register constants with immutable tables
	m.registerConstants(l, mod)

	// Register constructors
	mod.RawSetString("request", l.NewFunction(newRequest))
	mod.RawSetString("response", l.NewFunction(newResponse))

	// Make the module table immutable so it can be safely reused
	mod.Immutable = true

	m.moduleTable = mod
}

// registerMultipartFileType registers the MultipartFile type and its methods
func (m *Module) registerMultipartFileType(l *lua.LState) {
	value.RegisterTypeMethods(l, "MultipartFile",
		map[string]lua.LGFunction{
			"__tostring": multipartFileToString,
		},
		map[string]lua.LGFunction{
			"stream": multipartFileStream,
			"size":   multipartFileSize,
			"name":   multipartFileName,
		},
	)
}

// registerRequestType registers the Request type and its methods
func (m *Module) registerRequestType(l *lua.LState) {
	value.RegisterTypeMethods(l, "Request",
		map[string]lua.LGFunction{
			"__tostring": requestToString,
		},
		map[string]lua.LGFunction{
			"method":          requestMethod,
			"path":            requestPath,
			"query":           requestQuery,
			"query_params":    requestQueryAll,
			"header":          requestHeader,
			"content_type":    requestContentType,
			"content_length":  requestContentLength,
			"host":            requestHost,
			"remote_addr":     requestRemoteAddr,
			"body":            requestBody,
			"body_json":       requestBodyJSON,
			"has_body":        requestHasBody,
			"accepts":         requestAccepts,
			"is_content_type": requestIsContentType,
			"stream":          requestStream,
			"parse_multipart": requestParseMultipart,
			"param":           requestParam,
			"params":          requestParams,
		},
	)
}

// registerResponseType registers the Response type and its methods
func (m *Module) registerResponseType(l *lua.LState) {
	value.RegisterTypeMethods(l, "Response",
		map[string]lua.LGFunction{
			"__tostring": responseToString,
		},
		map[string]lua.LGFunction{
			"set_status":       responseSetStatus,
			"set_header":       responseSetHeader,
			"write":            responseWrite,
			"flush":            responseFlush,
			"write_json":       responseWriteJSON,
			"set_content_type": responseSetContentType,
			"write_event":      responseWriteEvent,
			"set_transfer":     responseSetTransfer,
		},
	)
}

// registerConstants registers all constant tables with explanatory comments
func (m *Module) registerConstants(l *lua.LState, mod *lua.LTable) {
	// METHOD table - HTTP methods
	methods := map[string]string{
		"GET":     "GET",
		"POST":    "POST",
		"PUT":     "PUT",
		"DELETE":  "DELETE",
		"PATCH":   "PATCH",
		"HEAD":    "HEAD",
		"OPTIONS": "OPTIONS",
	}
	methodTbl := l.CreateTable(0, len(methods))
	for name, val := range methods {
		methodTbl.RawSetString(name, lua.LString(val))
	}
	methodTbl.Immutable = true
	mod.RawSetString("METHOD", methodTbl)

	// STATUS table - HTTP status codes
	statuses := map[string]int{
		// Success codes (2xx)
		"OK":              200, // Success
		"CREATED":         201, // Resource created
		"ACCEPTED":        202, // Request accepted for processing
		"NO_CONTENT":      204, // Success with no body
		"PARTIAL_CONTENT": 206, // Partial content (range requests)

		// Redirection codes (3xx)
		"MOVED_PERMANENTLY":  301, // Resource moved permanently
		"FOUND":              302, // Temporary redirect
		"SEE_OTHER":          303, // See other resource
		"NOT_MODIFIED":       304, // Resource not modified since last request
		"TEMPORARY_REDIRECT": 307, // Temporary redirect, preserve method
		"PERMANENT_REDIRECT": 308, // Permanent redirect, preserve method

		// Client error codes (4xx)
		"BAD_REQUEST":        400, // Client error
		"UNAUTHORIZED":       401, // Authentication required
		"PAYMENT_REQUIRED":   402, // Payment required
		"FORBIDDEN":          403, // Access denied
		"NOT_FOUND":          404, // Resource not found
		"METHOD_NOT_ALLOWED": 405, // HTTP method not allowed
		"NOT_ACCEPTABLE":     406, // Not acceptable based on client-provided criteria
		"CONFLICT":           409, // Conflict with resource state
		"GONE":               410, // Resource no longer available
		"UNPROCESSABLE":      422, // Unprocessable entity
		"TOO_MANY_REQUESTS":  429, // Rate limit exceeded

		// Server error codes (5xx)
		"INTERNAL_ERROR":        500, // Server error
		"NOT_IMPLEMENTED":       501, // Functionality not implemented
		"BAD_GATEWAY":           502, // Bad gateway
		"SERVICE_UNAVAILABLE":   503, // Service temporarily unavailable
		"GATEWAY_TIMEOUT":       504, // Gateway timeout
		"VERSION_NOT_SUPPORTED": 505, // HTTP version not supported
	}
	statusTbl := l.CreateTable(0, len(statuses))
	for name, val := range statuses {
		statusTbl.RawSetString(name, lua.LNumber(val))
	}
	statusTbl.Immutable = true
	mod.RawSetString("STATUS", statusTbl)

	// CONTENT table - Content types
	contentTypes := map[string]string{
		"JSON":      "application/json",
		"FORM":      "application/x-www-form-urlencoded",
		"MULTIPART": "multipart/form-data",
		"TEXT":      "text/plain",
		"STREAM":    "application/octet-stream",
	}
	contentTbl := l.CreateTable(0, len(contentTypes))
	for name, val := range contentTypes {
		contentTbl.RawSetString(name, lua.LString(val))
	}
	contentTbl.Immutable = true
	mod.RawSetString("CONTENT", contentTbl)

	// TRANSFER table - Transfer encoding types
	transferConstants := getTransferConstants()
	transferTbl := l.CreateTable(0, len(transferConstants))
	for name, val := range transferConstants {
		transferTbl.RawSetString(name, lua.LString(val))
	}
	transferTbl.Immutable = true
	mod.RawSetString("TRANSFER", transferTbl)

	// ERROR table - Error types
	errorTypes := map[string]string{
		"PARSE_FAILED":  "PARSE_FAILED",  // Body parsing failed
		"INVALID_STATE": "INVALID_STATE", // Operation not valid in current state
		"WRITE_FAILED":  "WRITE_FAILED",  // Response write failed
		"STREAM_ERROR":  "STREAM_ERROR",  // Streaming operation failed
	}
	errorTbl := l.CreateTable(0, len(errorTypes))
	for name, errVal := range errorTypes {
		errorTbl.RawSetString(name, lua.LString(errVal))
	}
	errorTbl.Immutable = true
	mod.RawSetString("ERROR", errorTbl)
}
