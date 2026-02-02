package http

import (
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/wippyai/go-lua"
)

const (
	requestTypeName       = "http.Request"
	responseTypeName      = "http.Response"
	multipartFileTypeName = "http.MultipartFile"
)

func init() {
	value.RegisterTypeMethods(nil, requestTypeName,
		map[string]lua.LGoFunc{"__tostring": requestToString},
		requestMethods)

	value.RegisterTypeMethods(nil, responseTypeName,
		map[string]lua.LGoFunc{"__tostring": responseToString},
		responseMethods)

	value.RegisterTypeMethods(nil, multipartFileTypeName,
		map[string]lua.LGoFunc{"__tostring": multipartFileToString},
		multipartFileMethods)
}

// Module is the http module definition.
var Module = &luaapi.ModuleDef{
	Name:        "http",
	Description: "HTTP request and response types",
	Class:       []string{luaapi.ClassNetwork, luaapi.ClassIO, luaapi.ClassNondeterministic},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		mod := lua.CreateTable(0, 16)
		mod.RawSetString("request", lua.LGoFunc(newRequest))
		mod.RawSetString("response", lua.LGoFunc(newResponse))
		registerConstants(mod)
		mod.Immutable = true
		return mod, nil
	},
	Types: ModuleTypes,
}

func registerConstants(mod *lua.LTable) {
	methods := map[string]string{
		"GET": "GET", "POST": "POST", "PUT": "PUT",
		"DELETE": "DELETE", "PATCH": "PATCH", "HEAD": "HEAD", "OPTIONS": "OPTIONS",
	}
	methodTbl := &lua.LTable{}
	for name, val := range methods {
		methodTbl.RawSetString(name, lua.LString(val))
	}
	methodTbl.Immutable = true
	mod.RawSetString("METHOD", methodTbl)

	statuses := map[string]int{
		// Success codes (2xx)
		"OK": 200, "CREATED": 201, "ACCEPTED": 202, "NO_CONTENT": 204,
		"PARTIAL_CONTENT": 206,
		// Redirection codes (3xx)
		"MOVED_PERMANENTLY": 301, "FOUND": 302, "SEE_OTHER": 303, "NOT_MODIFIED": 304,
		"TEMPORARY_REDIRECT": 307, "PERMANENT_REDIRECT": 308,
		// Client error codes (4xx)
		"BAD_REQUEST": 400, "UNAUTHORIZED": 401, "PAYMENT_REQUIRED": 402,
		"FORBIDDEN": 403, "NOT_FOUND": 404, "METHOD_NOT_ALLOWED": 405,
		"NOT_ACCEPTABLE": 406, "CONFLICT": 409, "GONE": 410,
		"UNPROCESSABLE": 422, "TOO_MANY_REQUESTS": 429,
		// Server error codes (5xx)
		"INTERNAL_ERROR": 500, "INTERNAL_SERVER_ERROR": 500, "NOT_IMPLEMENTED": 501, "BAD_GATEWAY": 502,
		"SERVICE_UNAVAILABLE": 503, "GATEWAY_TIMEOUT": 504, "VERSION_NOT_SUPPORTED": 505,
	}
	statusTbl := &lua.LTable{}
	for name, val := range statuses {
		statusTbl.RawSetString(name, lua.LNumber(val))
	}
	statusTbl.Immutable = true
	mod.RawSetString("STATUS", statusTbl)

	contentTypes := map[string]string{
		"JSON": "application/json", "FORM": "application/x-www-form-urlencoded",
		"MULTIPART": "multipart/form-data", "TEXT": "text/plain",
		"STREAM": "application/octet-stream",
	}
	contentTbl := &lua.LTable{}
	for name, val := range contentTypes {
		contentTbl.RawSetString(name, lua.LString(val))
	}
	contentTbl.Immutable = true
	mod.RawSetString("CONTENT", contentTbl)

	transferConstants := map[string]string{"CHUNKED": "chunked", "SSE": "sse"}
	transferTbl := &lua.LTable{}
	for name, val := range transferConstants {
		transferTbl.RawSetString(name, lua.LString(val))
	}
	transferTbl.Immutable = true
	mod.RawSetString("TRANSFER", transferTbl)

	// ERROR table - Error types
	errorTypes := map[string]string{
		"PARSE_FAILED":  "PARSE_FAILED",
		"INVALID_STATE": "INVALID_STATE",
		"WRITE_FAILED":  "WRITE_FAILED",
		"STREAM_ERROR":  "STREAM_ERROR",
	}
	errorTbl := &lua.LTable{}
	for name, val := range errorTypes {
		errorTbl.RawSetString(name, lua.LString(val))
	}
	errorTbl.Immutable = true
	mod.RawSetString("ERROR", errorTbl)
}
