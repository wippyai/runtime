package json

import "github.com/yuin/gopher-lua/types"

// ModuleTypes returns the type manifest for the json module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("json")

	moduleType := &types.InterfaceType{
		Name: "json",
		Methods: map[string]*types.FunctionType{
			// json.encode(value: any): string, Error?
			"encode": types.NewFunction([]types.Type{types.Any}, []types.Type{types.String, types.Optional(types.LuaError)}),

			// json.decode(str: string): any, Error?
			"decode": types.NewFunction(
				[]types.Type{types.String},
				[]types.Type{types.Any, types.Optional(types.LuaError)},
			),

			// json.validate(schema: table|string, data: any): boolean, Error?
			"validate": types.NewFunction(
				[]types.Type{types.NewUnion(types.NewMap(types.String, types.Any, false), types.String), types.Any},
				[]types.Type{types.Boolean, types.Optional(types.LuaError)},
			),

			// json.validate_string(schema: table|string, str: string): boolean, Error?
			"validate_string": types.NewFunction(
				[]types.Type{types.NewUnion(types.NewMap(types.String, types.Any, false), types.String), types.String},
				[]types.Type{types.Boolean, types.Optional(types.LuaError)},
			),
		},
	}

	m.SetExport(moduleType)
	return m
}
