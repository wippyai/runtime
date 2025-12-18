package ctx

import "github.com/yuin/gopher-lua/types"

// ModuleTypes returns the type manifest for the ctx module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("ctx")

	moduleType := &types.InterfaceType{
		Name: "ctx",
		Methods: map[string]*types.FunctionType{
			// ctx.get(key: string): any, Error?
			"get": types.NewFunction(
				[]types.Type{types.String},
				[]types.Type{types.Any, types.Optional(types.LuaError)},
			),
			// ctx.all(): table, Error?
			"all": types.NewFunction(
				nil,
				[]types.Type{types.Any, types.Optional(types.LuaError)},
			),
		},
	}

	m.SetExport(moduleType)
	return m
}
