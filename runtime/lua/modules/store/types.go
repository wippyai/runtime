package store

import (
	"github.com/yuin/gopher-lua/types"
)

// Store type
var storeType = &types.InterfaceType{
	Name: "store.Store",
	Methods: map[string]*types.FunctionType{
		"get":     types.NewFunction([]types.Type{types.Self, types.String}, []types.Type{types.Any, types.Optional(types.LuaError)}),
		"set":     types.NewFunction([]types.Type{types.Self, types.String, types.Any, types.Optional(types.Number)}, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
		"delete":  types.NewFunction([]types.Type{types.Self, types.String}, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
		"has":     types.NewFunction([]types.Type{types.Self, types.String}, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
		"release": types.NewFunction([]types.Type{types.Self}, []types.Type{types.Boolean}),
	},
}

// ModuleTypes returns the type manifest for the store module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("store")

	m.DefineType("Store", storeType)

	moduleType := &types.InterfaceType{
		Name: "store",
		Methods: map[string]*types.FunctionType{
			"get": types.NewFunction([]types.Type{types.String}, []types.Type{types.Optional(storeType), types.Optional(types.LuaError)}),
		},
	}

	m.SetExport(moduleType)
	return m
}
