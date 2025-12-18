package http

import "github.com/yuin/gopher-lua/types"

// Request type
var requestType = &types.InterfaceType{
	Name: "http.Request",
	Methods: map[string]*types.FunctionType{
		"method":          types.NewFunction(nil, []types.Type{types.String, types.Optional(types.LuaError)}),
		"path":            types.NewFunction(nil, []types.Type{types.String, types.Optional(types.LuaError)}),
		"query":           types.NewFunction([]types.Type{types.String}, []types.Type{types.Optional(types.String), types.Optional(types.LuaError)}),
		"query_params":    types.NewFunction(nil, []types.Type{types.NewMap(types.String, types.String, false), types.Optional(types.LuaError)}),
		"header":          types.NewFunction([]types.Type{types.String}, []types.Type{types.Optional(types.String), types.Optional(types.LuaError)}),
		"content_type":    types.NewFunction(nil, []types.Type{types.Optional(types.String), types.Optional(types.LuaError)}),
		"content_length":  types.NewFunction(nil, []types.Type{types.Number, types.Optional(types.LuaError)}),
		"host":            types.NewFunction(nil, []types.Type{types.String, types.Optional(types.LuaError)}),
		"remote_addr":     types.NewFunction(nil, []types.Type{types.String, types.Optional(types.LuaError)}),
		"body":            types.NewFunction(nil, []types.Type{types.String, types.Optional(types.LuaError)}),
		"body_json":       types.NewFunction(nil, []types.Type{types.Any, types.Optional(types.LuaError)}),
		"has_body":        types.NewFunction(nil, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
		"accepts":         types.NewFunction([]types.Type{types.String}, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
		"is_content_type": types.NewFunction([]types.Type{types.String}, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
		"param":           types.NewFunction([]types.Type{types.String}, []types.Type{types.Optional(types.String), types.Optional(types.LuaError)}),
		"params":          types.NewFunction(nil, []types.Type{types.NewMap(types.String, types.String, false), types.Optional(types.LuaError)}),
		"stream":          types.NewFunction(nil, []types.Type{types.Any, types.Optional(types.LuaError)}),
		"parse_multipart": types.NewFunction([]types.Type{types.Optional(types.Number)}, []types.Type{types.Any, types.Optional(types.LuaError)}),
	},
}

// Response type
var responseType = &types.InterfaceType{
	Name: "http.Response",
	Methods: map[string]*types.FunctionType{
		"set_status":       types.NewFunction([]types.Type{types.Number}, []types.Type{types.Optional(types.LuaError)}),
		"set_header":       types.NewFunction([]types.Type{types.String, types.String}, []types.Type{types.Optional(types.LuaError)}),
		"write":            types.NewFunction([]types.Type{types.String}, []types.Type{types.Optional(types.LuaError)}),
		"flush":            types.NewFunction(nil, []types.Type{types.Optional(types.LuaError)}),
		"write_json":       types.NewFunction([]types.Type{types.Any}, []types.Type{types.Optional(types.LuaError)}),
		"set_content_type": types.NewFunction([]types.Type{types.String}, []types.Type{types.Optional(types.LuaError)}),
		"write_event":      types.NewFunction([]types.Type{types.Any}, []types.Type{types.Optional(types.LuaError)}),
		"set_transfer":     types.NewFunction([]types.Type{types.String}, []types.Type{types.Optional(types.LuaError)}),
	},
}

// MultipartFile type
var multipartFileType = &types.InterfaceType{
	Name: "http.MultipartFile",
	Methods: map[string]*types.FunctionType{
		"stream": types.NewFunction(nil, []types.Type{types.Any, types.Optional(types.LuaError)}),
		"size":   types.NewFunction(nil, []types.Type{types.Number, types.Optional(types.LuaError)}),
		"name":   types.NewFunction(nil, []types.Type{types.String, types.Optional(types.LuaError)}),
		"header": types.NewFunction([]types.Type{types.String}, []types.Type{types.Optional(types.String)}),
	},
}

// METHOD constants
var methodConstType = &types.InterfaceType{
	Name: "http.METHOD",
	Fields: map[string]types.Type{
		"GET":     types.String,
		"POST":    types.String,
		"PUT":     types.String,
		"DELETE":  types.String,
		"PATCH":   types.String,
		"HEAD":    types.String,
		"OPTIONS": types.String,
	},
}

// STATUS constants
var statusConstType = &types.InterfaceType{
	Name: "http.STATUS",
	Fields: map[string]types.Type{
		"OK":                    types.Number,
		"CREATED":               types.Number,
		"ACCEPTED":              types.Number,
		"NO_CONTENT":            types.Number,
		"PARTIAL_CONTENT":       types.Number,
		"MOVED_PERMANENTLY":     types.Number,
		"FOUND":                 types.Number,
		"SEE_OTHER":             types.Number,
		"NOT_MODIFIED":          types.Number,
		"TEMPORARY_REDIRECT":    types.Number,
		"PERMANENT_REDIRECT":    types.Number,
		"BAD_REQUEST":           types.Number,
		"UNAUTHORIZED":          types.Number,
		"PAYMENT_REQUIRED":      types.Number,
		"FORBIDDEN":             types.Number,
		"NOT_FOUND":             types.Number,
		"METHOD_NOT_ALLOWED":    types.Number,
		"NOT_ACCEPTABLE":        types.Number,
		"CONFLICT":              types.Number,
		"GONE":                  types.Number,
		"UNPROCESSABLE":         types.Number,
		"TOO_MANY_REQUESTS":     types.Number,
		"INTERNAL_ERROR":        types.Number,
		"INTERNAL_SERVER_ERROR": types.Number,
		"NOT_IMPLEMENTED":       types.Number,
		"BAD_GATEWAY":           types.Number,
		"SERVICE_UNAVAILABLE":   types.Number,
		"GATEWAY_TIMEOUT":       types.Number,
		"VERSION_NOT_SUPPORTED": types.Number,
	},
}

// CONTENT constants
var contentConstType = &types.InterfaceType{
	Name: "http.CONTENT",
	Fields: map[string]types.Type{
		"JSON":      types.String,
		"FORM":      types.String,
		"MULTIPART": types.String,
		"TEXT":      types.String,
		"STREAM":    types.String,
	},
}

// TRANSFER constants
var transferConstType = &types.InterfaceType{
	Name: "http.TRANSFER",
	Fields: map[string]types.Type{
		"CHUNKED": types.String,
		"SSE":     types.String,
	},
}

// ModuleTypes returns the type manifest for the http module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("http")

	m.DefineType("Request", requestType)
	m.DefineType("Response", responseType)
	m.DefineType("MultipartFile", multipartFileType)

	moduleType := &types.InterfaceType{
		Name: "http",
		Fields: map[string]types.Type{
			"METHOD":   methodConstType,
			"STATUS":   statusConstType,
			"CONTENT":  contentConstType,
			"TRANSFER": transferConstType,
		},
		Methods: map[string]*types.FunctionType{
			"request":  types.NewFunction([]types.Type{types.Optional(types.Any)}, []types.Type{requestType, types.Optional(types.LuaError)}),
			"response": types.NewFunction(nil, []types.Type{responseType, types.Optional(types.LuaError)}),
		},
	}

	m.SetExport(moduleType)
	return m
}
