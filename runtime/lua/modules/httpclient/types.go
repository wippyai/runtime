package httpclient

import (
	"github.com/yuin/gopher-lua/types"
)

// StreamReader type for streaming response body
var streamReaderType = &types.InterfaceType{
	Name: "http_client.StreamReader",
	Methods: map[string]*types.FunctionType{
		"read":  types.NewFunction([]types.Type{types.Number}, []types.Type{types.String, types.Optional(types.LuaError)}),
		"close": types.NewFunction(nil, nil),
	},
}

// Response type for HTTP responses
var responseType = &types.InterfaceType{
	Name: "http_client.Response",
	Fields: map[string]types.Type{
		"status_code": types.Number,
		"headers":     types.NewMap(types.String, types.String, true),
		"cookies":     types.NewMap(types.String, types.String, true),
		"body":        types.String,
		"body_size":   types.Number,
		"url":         types.String,
		"stream":      types.Optional(streamReaderType),
	},
}

// RequestOptions type for request configuration
var requestOptionsType = &types.RecordType{
	Name: "http_client.RequestOptions",
	Fields: []types.RecordField{
		{Name: "headers", Type: types.NewMap(types.String, types.String, false), Optional: true},
		{Name: "body", Type: types.String, Optional: true},
		{Name: "timeout", Type: types.NewUnion(types.Number, types.String), Optional: true},
		{Name: "unix_socket", Type: types.String, Optional: true},
		{Name: "query", Type: types.NewMap(types.String, types.String, false), Optional: true},
		{Name: "cookies", Type: types.NewMap(types.String, types.String, false), Optional: true},
		{Name: "form", Type: types.NewMap(types.String, types.String, false), Optional: true},
		{Name: "files", Type: types.NewArray(types.Any, false), Optional: true},
		{Name: "auth", Type: types.Any, Optional: true},
		{Name: "stream", Type: types.Boolean, Optional: true},
		{Name: "max_response_body", Type: types.Number, Optional: true},
	},
}

// ModuleTypes returns the type manifest for the http_client module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("http_client")

	// Register exported types
	m.DefineType("Response", responseType)
	m.DefineType("RequestOptions", requestOptionsType)
	m.DefineType("StreamReader", streamReaderType)

	// Function type for HTTP methods (get, post, put, etc.)
	httpMethodFn := types.NewFunction(
		[]types.Type{types.String, types.Optional(requestOptionsType)},
		[]types.Type{responseType, types.Optional(types.LuaError)},
	)

	moduleType := &types.InterfaceType{
		Name: "http_client",
		Methods: map[string]*types.FunctionType{
			"get":    httpMethodFn,
			"post":   httpMethodFn,
			"put":    httpMethodFn,
			"delete": httpMethodFn,
			"head":   httpMethodFn,
			"patch":  httpMethodFn,

			// request(method, url, opts?): Response, Error?
			"request": types.NewFunction(
				[]types.Type{types.String, types.String, types.Optional(requestOptionsType)},
				[]types.Type{responseType, types.Optional(types.LuaError)},
			),

			// request_batch(requests): {Response}, Error?
			"request_batch": types.NewFunction(
				[]types.Type{types.NewArray(types.Any, false)},
				[]types.Type{types.NewArray(responseType, false), types.Optional(types.LuaError)},
			),

			// encode_uri(s): string
			"encode_uri": types.NewFunction(
				[]types.Type{types.String},
				[]types.Type{types.String},
			),

			// decode_uri(s): string, Error?
			"decode_uri": types.NewFunction(
				[]types.Type{types.String},
				[]types.Type{types.String, types.Optional(types.LuaError)},
			),
		},
	}

	m.SetExport(moduleType)
	return m
}
