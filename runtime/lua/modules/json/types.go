package json

import (
	"github.com/yuin/gopher-lua/types"
)

// ModuleTypes returns the type manifest for the json module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("json")

	// Schema accepts any table structure or string reference
	schemaParam := types.NewUnion(types.Any, types.String)

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

			// json.validate(schema: Schema|string, data: any): boolean, Error?
			"validate": types.NewFunction(
				[]types.Type{schemaParam, types.Any},
				[]types.Type{types.Boolean, types.Optional(types.LuaError)},
			),

			// json.validate_string(schema: Schema|string, str: string): boolean, Error?
			"validate_string": types.NewFunction(
				[]types.Type{schemaParam, types.String},
				[]types.Type{types.Boolean, types.Optional(types.LuaError)},
			),
		},
	}

	m.SetExport(moduleType)
	return m
}
