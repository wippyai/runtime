package httpclient

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// StreamReader type for streaming response body
var streamReaderType = typ.NewInterface("http_client.StreamReader", []typ.Method{
	{Name: "read", Type: typ.Func().Param("self", typ.Self).Param("size", typ.Number).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "close", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
})

// Response type for HTTP responses
var responseType = typ.NewRecord().
	Field("status_code", typ.Number).
	Field("headers", typ.NewMap(typ.String, typ.String)).
	Field("cookies", typ.NewMap(typ.String, typ.String)).
	OptField("body", typ.String).
	Field("body_size", typ.Number).
	Field("url", typ.String).
	OptField("stream", streamReaderType).
	Build()

// RequestOptions type for request configuration
var requestOptionsType = typ.NewRecord().
	OptField("headers", typ.NewMap(typ.String, typ.String)).
	OptField("body", typ.String).
	OptField("timeout", typ.NewUnion(typ.Number, typ.String)).
	OptField("unix_socket", typ.String).
	OptField("query", typ.NewMap(typ.String, typ.String)).
	OptField("cookies", typ.NewMap(typ.String, typ.String)).
	OptField("form", typ.NewMap(typ.String, typ.String)).
	OptField("files", typ.NewArray(typ.Any)).
	OptField("auth", typ.Any).
	OptField("stream", typ.Boolean).
	OptField("max_response_body", typ.Number).
	Build()

// ModuleTypes returns the type manifest for the http_client module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("http_client")

	// Register exported types
	m.DefineType("Response", responseType)
	m.DefineType("RequestOptions", requestOptionsType)
	m.DefineType("StreamReader", streamReaderType)

	// Function type for HTTP methods (get, post, put, etc.)
	httpMethodFn := typ.Func().
		Param("url", typ.String).
		OptParam("opts", requestOptionsType).
		Returns(responseType, typ.NewOptional(typ.LuaError)).
		Build()

	moduleType := typ.NewInterface("http_client", []typ.Method{
		{Name: "get", Type: httpMethodFn},
		{Name: "post", Type: httpMethodFn},
		{Name: "put", Type: httpMethodFn},
		{Name: "delete", Type: httpMethodFn},
		{Name: "head", Type: httpMethodFn},
		{Name: "patch", Type: httpMethodFn},
		// request(method, url, opts?): Response, Error?
		{Name: "request", Type: typ.Func().
			Param("method", typ.String).
			Param("url", typ.String).
			OptParam("opts", requestOptionsType).
			Returns(responseType, typ.NewOptional(typ.LuaError)).
			Build()},
		// request_batch(requests): {Response}, Error?
		{Name: "request_batch", Type: typ.Func().
			Param("requests", typ.NewArray(typ.Any)).
			Returns(typ.NewArray(responseType), typ.NewOptional(typ.LuaError)).
			Build()},
		// encode_uri(s): string
		{Name: "encode_uri", Type: typ.Func().
			Param("s", typ.String).
			Returns(typ.String).
			Build()},
		// decode_uri(s): string, Error?
		{Name: "decode_uri", Type: typ.Func().
			Param("s", typ.String).
			Returns(typ.String, typ.NewOptional(typ.LuaError)).
			Build()},
	})

	m.SetExport(moduleType)
	return m
}
