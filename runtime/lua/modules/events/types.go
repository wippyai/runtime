package events

import (
	"github.com/yuin/gopher-lua/types"
)

// ModuleTypes returns the type manifest for the events module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("events")

	moduleType := &types.InterfaceType{
		Name: "events",
		Methods: map[string]*types.FunctionType{
			"subscribe": types.NewFunction([]types.Type{types.String, types.Optional(types.String)}, []types.Type{types.Any, types.Optional(types.LuaError)}),
			"send":      types.NewFunction([]types.Type{types.String, types.String, types.String, types.Optional(types.Any)}, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
		},
	}

	m.SetExport(moduleType)
	return m
}
