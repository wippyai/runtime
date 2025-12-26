package stream

import (
	"github.com/yuin/gopher-lua/types"
)

// Stream type
var streamType = &types.InterfaceType{
	Name: "stream.Stream",
	Methods: map[string]*types.FunctionType{
		"read":     types.NewFunction([]types.Type{types.Optional(types.Number)}, []types.Type{types.String, types.Optional(types.LuaError)}),
		"read_all": types.NewFunction(nil, []types.Type{types.String, types.Optional(types.LuaError)}),
		"write":    types.NewFunction([]types.Type{types.String}, []types.Type{types.Number, types.Optional(types.LuaError)}),
		"seek":     types.NewFunction([]types.Type{types.String, types.Number}, []types.Type{types.Number, types.Optional(types.LuaError)}),
		"flush":    types.NewFunction(nil, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
		"stat":     types.NewFunction(nil, []types.Type{types.Any, types.Optional(types.LuaError)}),
		"close":    types.NewFunction(nil, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
		"scanner":  types.NewFunction([]types.Type{types.Optional(types.String)}, []types.Type{scannerType, types.Optional(types.LuaError)}),
	},
}

// Scanner type
var scannerType = &types.InterfaceType{
	Name: "stream.Scanner",
	Methods: map[string]*types.FunctionType{
		"scan": types.NewFunction(nil, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
		"text": types.NewFunction(nil, []types.Type{types.String}),
		"err":  types.NewFunction(nil, []types.Type{types.Optional(types.LuaError)}),
	},
}

// ModuleTypes returns the type manifest for the stream module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("stream")

	m.DefineType("Stream", streamType)
	m.DefineType("Scanner", scannerType)

	moduleType := &types.InterfaceType{
		Name: "stream",
	}

	m.SetExport(moduleType)
	return m
}
