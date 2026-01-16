package queue

import (
	"github.com/yuin/gopher-lua/types"
)

// Message type
var messageType = &types.InterfaceType{
	Name: "queue.Message",
	Methods: map[string]*types.FunctionType{
		"id":      types.NewFunction([]types.Type{types.Self}, []types.Type{types.String, types.Optional(types.LuaError)}),
		"header":  types.NewFunction([]types.Type{types.Self, types.String}, []types.Type{types.Any, types.Optional(types.LuaError)}),
		"headers": types.NewFunction([]types.Type{types.Self}, []types.Type{types.NewMap(types.String, types.Any, false), types.Optional(types.LuaError)}),
	},
}

// ModuleTypes returns the type manifest for the queue module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("queue")

	m.DefineType("Message", messageType)

	moduleType := &types.InterfaceType{
		Name: "queue",
		Methods: map[string]*types.FunctionType{
			"publish": types.NewFunction([]types.Type{types.String, types.Any, types.Optional(types.Any)}, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
			"message": types.NewFunction(nil, []types.Type{messageType, types.Optional(types.LuaError)}),
		},
	}

	m.SetExport(moduleType)
	return m
}
