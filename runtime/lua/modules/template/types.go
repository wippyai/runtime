package template

import (
	"github.com/yuin/gopher-lua/types"
)

// Set type
var setType = &types.InterfaceType{
	Name: "template.Set",
	Methods: map[string]*types.FunctionType{
		"render":  types.NewFunction([]types.Type{types.Self, types.String, types.Optional(types.Any)}, []types.Type{types.String, types.Optional(types.LuaError)}),
		"release": types.NewFunction([]types.Type{types.Self}, []types.Type{types.Boolean}),
	},
}

// ModuleTypes returns the type manifest for the template module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("templates")

	m.DefineType("Set", setType)

	moduleType := &types.InterfaceType{
		Name: "templates",
		Methods: map[string]*types.FunctionType{
			"get": types.NewFunction([]types.Type{types.String}, []types.Type{setType, types.Optional(types.LuaError)}),
		},
	}

	m.SetExport(moduleType)
	return m
}
