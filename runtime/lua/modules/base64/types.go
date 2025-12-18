package base64

import "github.com/yuin/gopher-lua/types"

// ModuleTypes returns the type manifest for the base64 module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("base64")

	moduleType := &types.InterfaceType{
		Name: "base64",
		Methods: map[string]*types.FunctionType{
			// base64.encode(data: string): string
			"encode": types.NewFunction(
				[]types.Type{types.String},
				[]types.Type{types.String},
			),
			// base64.decode(data: string): string, Error?
			"decode": types.NewFunction(
				[]types.Type{types.String},
				[]types.Type{types.String, types.Optional(types.LuaError)},
			),
		},
	}

	m.SetExport(moduleType)
	return m
}
