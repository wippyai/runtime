package websocket

import (
	"github.com/yuin/gopher-lua/types"
)

// Client type
var clientType = &types.InterfaceType{
	Name: "websocket.Client",
	Methods: map[string]*types.FunctionType{
		"send":    types.NewFunction([]types.Type{types.String, types.Optional(types.Number)}, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
		"receive": types.NewFunction(nil, []types.Type{types.Any, types.Optional(types.LuaError)}),
		"channel": types.NewFunction(nil, []types.Type{types.Any, types.Optional(types.LuaError)}),
		"close":   types.NewFunction([]types.Type{types.Optional(types.Number), types.Optional(types.String)}, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
		"ping":    types.NewFunction(nil, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
	},
}

// COMPRESSION constants
var compressionConstType = &types.InterfaceType{
	Name: "websocket.COMPRESSION",
	Fields: map[string]types.Type{
		"DISABLED":         types.Number,
		"CONTEXT_TAKEOVER": types.Number,
		"NO_CONTEXT":       types.Number,
	},
}

// CLOSE_CODES constants
var closeCodesConstType = &types.InterfaceType{
	Name: "websocket.CLOSE_CODES",
	Fields: map[string]types.Type{
		"NORMAL":              types.Number,
		"GOING_AWAY":          types.Number,
		"PROTOCOL_ERROR":      types.Number,
		"UNSUPPORTED_DATA":    types.Number,
		"RESERVED":            types.Number,
		"NO_STATUS":           types.Number,
		"ABNORMAL_CLOSURE":    types.Number,
		"INVALID_PAYLOAD":     types.Number,
		"POLICY_VIOLATION":    types.Number,
		"MESSAGE_TOO_BIG":     types.Number,
		"MANDATORY_EXTENSION": types.Number,
		"INTERNAL_ERROR":      types.Number,
		"SERVICE_RESTART":     types.Number,
		"TRY_AGAIN_LATER":     types.Number,
		"BAD_GATEWAY":         types.Number,
		"TLS_HANDSHAKE":       types.Number,
	},
}

// ModuleTypes returns the type manifest for the websocket module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("websocket")

	m.DefineType("Client", clientType)

	moduleType := &types.InterfaceType{
		Name: "websocket",
		Fields: map[string]types.Type{
			"TYPE_TEXT":   types.String,
			"TYPE_BINARY": types.String,
			"TYPE_PING":   types.String,
			"TYPE_PONG":   types.String,
			"TYPE_CLOSE":  types.String,
			"TEXT":        types.Number,
			"BINARY":      types.Number,
			"COMPRESSION": compressionConstType,
			"CLOSE_CODES": closeCodesConstType,
		},
		Methods: map[string]*types.FunctionType{
			"connect": types.NewFunction([]types.Type{types.String, types.Optional(types.Any)}, []types.Type{clientType, types.Optional(types.LuaError)}),
		},
	}

	m.SetExport(moduleType)
	return m
}
