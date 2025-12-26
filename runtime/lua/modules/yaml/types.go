package yaml

import (
	"github.com/yuin/gopher-lua/types"
)

// EncodeOptions type
var encodeOptionsType = &types.RecordType{
	Name: "yaml.EncodeOptions",
	Fields: []types.RecordField{
		{Name: "field_order", Type: types.NewArray(types.String, false)},
		{Name: "sort_unordered", Type: types.Boolean},
	},
}

// ModuleTypes returns the type manifest for the yaml module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("yaml")

	m.DefineType("EncodeOptions", encodeOptionsType)

	moduleType := &types.InterfaceType{
		Name: "yaml",
		Methods: map[string]*types.FunctionType{
			// yaml.encode(value: table, options?: EncodeOptions): string, Error?
			"encode": types.NewFunction(
				[]types.Type{types.NewMap(types.String, types.Any, false), types.Optional(encodeOptionsType)},
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
