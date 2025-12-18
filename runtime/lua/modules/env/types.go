package env

import "github.com/yuin/gopher-lua/types"

// ModuleTypes returns the type manifest for the env module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("env")

	moduleType := &types.InterfaceType{
		Name: "env",
		Methods: map[string]*types.FunctionType{
			// env.get(key: string): string, Error?
			"get": types.NewFunction(
				[]types.Type{types.String},
				[]types.Type{types.String, types.Optional(types.LuaError)},
			),
			// env.set(key: string, value: string): boolean, Error?
			"set": types.NewFunction(
				[]types.Type{types.String, types.String},
				[]types.Type{types.Boolean, types.Optional(types.LuaError)},
			),
			// env.get_all(): {string: string}, Error?
			"get_all": types.NewFunction(
				nil,
				[]types.Type{types.NewMap(types.String, types.String, true), types.Optional(types.LuaError)},
			),
		},
	}

	m.SetExport(moduleType)
	return m
}
