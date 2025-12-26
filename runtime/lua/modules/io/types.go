package io

import (
	"github.com/yuin/gopher-lua/types"
)

// ModuleTypes returns the type manifest for the io module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("io")

	moduleType := &types.InterfaceType{
		Name: "io",
		Methods: map[string]*types.FunctionType{
			"write":    {Params: nil, Variadic: types.Any, Returns: []types.Type{types.Boolean, types.Optional(types.LuaError)}},
			"print":    {Params: nil, Variadic: types.Any, Returns: []types.Type{types.Boolean, types.Optional(types.LuaError)}},
			"eprint":   {Params: nil, Variadic: types.Any, Returns: []types.Type{types.Boolean, types.Optional(types.LuaError)}},
			"read":     types.NewFunction([]types.Type{types.Optional(types.Number)}, []types.Type{types.String, types.Optional(types.LuaError)}),
			"readline": types.NewFunction(nil, []types.Type{types.String, types.Optional(types.LuaError)}),
			"flush":    types.NewFunction(nil, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
			"args":     types.NewFunction(nil, []types.Type{types.NewArray(types.String, false)}),
		},
	}

	m.SetExport(moduleType)
	return m
}
