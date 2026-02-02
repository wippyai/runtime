package http

import (
	"github.com/wippyai/runtime/runtime/lua/modules/stream"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

var (
	streamType        typ.Type
	requestType       *typ.Interface
	multipartFileType *typ.Interface
	multipartFormType typ.Type
)

func init() {
	// Get stream type from manifest
	if manifest := stream.ModuleTypes(); manifest != nil {
		if t, ok := manifest.LookupType("Stream"); ok {
			streamType = t
		}
	}
	if streamType == nil {
		streamType = typ.Any
	}

	// Build multipart types first (requestType depends on multipartFormType)
	multipartFileType = typ.NewInterface("http.MultipartFile", []typ.Method{
		{Name: "stream", Type: typ.Func().Param("self", typ.Self).Returns(streamType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "size", Type: typ.Func().Param("self", typ.Self).Returns(typ.Number, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "name", Type: typ.Func().Param("self", typ.Self).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "header", Type: typ.Func().Param("self", typ.Self).Param("name", typ.String).Returns(typ.NewOptional(typ.String)).Build()},
	})

	multipartFormType = typ.NewRecord().
		OptField("values", typ.NewMap(typ.String, typ.NewArray(typ.String))).
		OptField("files", typ.NewMap(typ.String, typ.NewArray(multipartFileType))).
		Build()

	// Build request type
	requestType = typ.NewInterface("http.Request", []typ.Method{
		{Name: "method", Type: typ.Func().Param("self", typ.Self).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "path", Type: typ.Func().Param("self", typ.Self).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "query", Type: typ.Func().Param("self", typ.Self).Param("key", typ.String).Returns(typ.NewOptional(typ.String), typ.NewOptional(typ.LuaError)).Build()},
		{Name: "query_params", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewMap(typ.String, typ.String), typ.NewOptional(typ.LuaError)).Build()},
		{Name: "header", Type: typ.Func().Param("self", typ.Self).Param("name", typ.String).Returns(typ.NewOptional(typ.String), typ.NewOptional(typ.LuaError)).Build()},
		{Name: "content_type", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewOptional(typ.String), typ.NewOptional(typ.LuaError)).Build()},
		{Name: "content_length", Type: typ.Func().Param("self", typ.Self).Returns(typ.Number, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "host", Type: typ.Func().Param("self", typ.Self).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "remote_addr", Type: typ.Func().Param("self", typ.Self).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "body", Type: typ.Func().Param("self", typ.Self).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "body_json", Type: typ.Func().Param("self", typ.Self).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "has_body", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "accepts", Type: typ.Func().Param("self", typ.Self).Param("contentType", typ.String).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "is_content_type", Type: typ.Func().Param("self", typ.Self).Param("contentType", typ.String).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "param", Type: typ.Func().Param("self", typ.Self).Param("key", typ.String).Returns(typ.NewOptional(typ.String), typ.NewOptional(typ.LuaError)).Build()},
		{Name: "params", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewMap(typ.String, typ.String), typ.NewOptional(typ.LuaError)).Build()},
		{Name: "stream", Type: typ.Func().Param("self", typ.Self).Returns(streamType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "parse_multipart", Type: typ.Func().Param("self", typ.Self).OptParam("maxMemory", typ.Number).Returns(multipartFormType, typ.NewOptional(typ.LuaError)).Build()},
	})
}

// Response type
var responseType = typ.NewInterface("http.Response", []typ.Method{
	{Name: "set_status", Type: typ.Func().Param("self", typ.Self).Param("status", typ.Number).Returns(typ.NewOptional(typ.LuaError)).Build()},
	{Name: "set_header", Type: typ.Func().Param("self", typ.Self).Param("name", typ.String).Param("value", typ.String).Returns(typ.NewOptional(typ.LuaError)).Build()},
	{Name: "write", Type: typ.Func().Param("self", typ.Self).Param("data", typ.String).Returns(typ.NewOptional(typ.LuaError)).Build()},
	{Name: "flush", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewOptional(typ.LuaError)).Build()},
	{Name: "write_json", Type: typ.Func().Param("self", typ.Self).Param("data", typ.Any).Returns(typ.NewOptional(typ.LuaError)).Build()},
	{Name: "set_content_type", Type: typ.Func().Param("self", typ.Self).Param("contentType", typ.String).Returns(typ.NewOptional(typ.LuaError)).Build()},
	{Name: "write_event", Type: typ.Func().Param("self", typ.Self).Param("data", typ.Any).Returns(typ.NewOptional(typ.LuaError)).Build()},
	{Name: "set_transfer", Type: typ.Func().Param("self", typ.Self).Param("encoding", typ.String).Returns(typ.NewOptional(typ.LuaError)).Build()},
})

// METHOD constants
var methodConstType = typ.NewRecord().
	Field("GET", typ.String).
	Field("POST", typ.String).
	Field("PUT", typ.String).
	Field("DELETE", typ.String).
	Field("PATCH", typ.String).
	Field("HEAD", typ.String).
	Field("OPTIONS", typ.String).
	Build()

// STATUS constants
var statusConstType = typ.NewRecord().
	Field("OK", typ.Number).
	Field("CREATED", typ.Number).
	Field("ACCEPTED", typ.Number).
	Field("NO_CONTENT", typ.Number).
	Field("PARTIAL_CONTENT", typ.Number).
	Field("MOVED_PERMANENTLY", typ.Number).
	Field("FOUND", typ.Number).
	Field("SEE_OTHER", typ.Number).
	Field("NOT_MODIFIED", typ.Number).
	Field("TEMPORARY_REDIRECT", typ.Number).
	Field("PERMANENT_REDIRECT", typ.Number).
	Field("BAD_REQUEST", typ.Number).
	Field("UNAUTHORIZED", typ.Number).
	Field("PAYMENT_REQUIRED", typ.Number).
	Field("FORBIDDEN", typ.Number).
	Field("NOT_FOUND", typ.Number).
	Field("METHOD_NOT_ALLOWED", typ.Number).
	Field("NOT_ACCEPTABLE", typ.Number).
	Field("CONFLICT", typ.Number).
	Field("GONE", typ.Number).
	Field("UNPROCESSABLE", typ.Number).
	Field("TOO_MANY_REQUESTS", typ.Number).
	Field("INTERNAL_ERROR", typ.Number).
	Field("INTERNAL_SERVER_ERROR", typ.Number).
	Field("NOT_IMPLEMENTED", typ.Number).
	Field("BAD_GATEWAY", typ.Number).
	Field("SERVICE_UNAVAILABLE", typ.Number).
	Field("GATEWAY_TIMEOUT", typ.Number).
	Field("VERSION_NOT_SUPPORTED", typ.Number).
	Build()

// CONTENT constants
var contentConstType = typ.NewRecord().
	Field("JSON", typ.String).
	Field("FORM", typ.String).
	Field("MULTIPART", typ.String).
	Field("TEXT", typ.String).
	Field("STREAM", typ.String).
	Build()

// TRANSFER constants
var transferConstType = typ.NewRecord().
	Field("CHUNKED", typ.String).
	Field("SSE", typ.String).
	Build()

// ModuleTypes returns the type manifest for the http module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("http")

	m.DefineType("Request", requestType)
	m.DefineType("Response", responseType)
	m.DefineType("MultipartFile", multipartFileType)
	m.DefineType("MultipartForm", multipartFormType)
	m.DefineType("Stream", streamType)

	moduleMethodsType := typ.NewInterface("http", []typ.Method{
		{Name: "request", Type: typ.Func().OptParam("config", typ.Any).Returns(requestType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "response", Type: typ.Func().Returns(responseType, typ.NewOptional(typ.LuaError)).Build()},
	})

	moduleFieldsType := typ.NewRecord().
		Field("METHOD", methodConstType).
		Field("STATUS", statusConstType).
		Field("CONTENT", contentConstType).
		Field("TRANSFER", transferConstType).
		Build()

	m.SetExport(typ.NewIntersection(moduleMethodsType, moduleFieldsType))
	return m
}
