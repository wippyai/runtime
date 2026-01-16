package payload

import (
	"github.com/yuin/gopher-lua/types"
)

// Payload type - defined in init to avoid initialization cycle
var payloadType *types.InterfaceType

func init() {
	payloadType = &types.InterfaceType{
		Name: "payload.Payload",
		Methods: map[string]*types.FunctionType{
			"get_format": types.NewFunction([]types.Type{types.Self}, []types.Type{types.String}),
			"data":       types.NewFunction([]types.Type{types.Self}, []types.Type{types.Any, types.Optional(types.LuaError)}),
			"unmarshal":  types.NewFunction([]types.Type{types.Self}, []types.Type{types.Any, types.Optional(types.LuaError)}),
		},
	}
	// Add transcode after init to avoid cycle
	payloadType.Methods["transcode"] = types.NewFunction([]types.Type{types.Self, types.String}, []types.Type{payloadType, types.Optional(types.LuaError)})
}

// format constants type
var formatType = &types.InterfaceType{
	Name: "payload.format",
	Fields: map[string]types.Type{
		"JSON":    types.String,
		"YAML":    types.String,
		"STRING":  types.String,
		"GOLANG":  types.String,
		"LUA":     types.String,
		"BYTES":   types.String,
		"MSGPACK": types.String,
		"ERROR":   types.String,
	},
}

// ModuleTypes returns the type manifest for the payload module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("payload")

	m.DefineType("Payload", payloadType)

	moduleType := &types.InterfaceType{
		Name: "payload",
		Fields: map[string]types.Type{
			"format": formatType,
		},
		Methods: map[string]*types.FunctionType{
			"new": types.NewFunction([]types.Type{types.Any}, []types.Type{payloadType}),
		},
	}

	m.SetExport(moduleType)
	return m
}
