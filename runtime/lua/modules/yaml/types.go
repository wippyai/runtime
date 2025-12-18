package yaml

import "github.com/yuin/gopher-lua/types"

// ModuleTypes returns the type manifest for the yaml module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("yaml")

	moduleType := &types.InterfaceType{
		Name: "yaml",
		Methods: map[string]*types.FunctionType{
			// yaml.encode(value: table): string, Error?
			"encode": types.NewFunction(
				[]types.Type{types.Any},
				[]types.Type{types.String, types.Optional(types.LuaError)},
			),
			// yaml.decode(str: string): any, Error?
			"decode": types.NewFunction(
				[]types.Type{types.String},
				[]types.Type{types.Any, types.Optional(types.LuaError)},
			),
		},
	}

	m.SetExport(moduleType)
	return m
}
