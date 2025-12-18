package excel

import "github.com/yuin/gopher-lua/types"

// Workbook type
var workbookType = &types.InterfaceType{
	Name: "excel.Workbook",
	Methods: map[string]*types.FunctionType{
		"new_sheet":      types.NewFunction([]types.Type{types.String}, []types.Type{types.Number, types.Optional(types.LuaError)}),
		"get_sheet_list": types.NewFunction(nil, []types.Type{types.NewArray(types.String, false), types.Optional(types.LuaError)}),
		"get_rows":       types.NewFunction([]types.Type{types.String}, []types.Type{types.NewArray(types.NewArray(types.String, false), false), types.Optional(types.LuaError)}),
		"set_cell_value": types.NewFunction([]types.Type{types.String, types.String, types.Any}, []types.Type{types.Optional(types.LuaError)}),
		"write_to":       types.NewFunction([]types.Type{types.Any}, []types.Type{types.Optional(types.LuaError)}),
		"close":          types.NewFunction(nil, []types.Type{types.Optional(types.LuaError)}),
	},
}

// ModuleTypes returns the type manifest for the excel module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("excel")

	m.DefineType("Workbook", workbookType)

	moduleType := &types.InterfaceType{
		Name: "excel",
		Methods: map[string]*types.FunctionType{
			"new":  types.NewFunction(nil, []types.Type{workbookType, types.Optional(types.LuaError)}),
			"open": types.NewFunction([]types.Type{types.Any}, []types.Type{workbookType, types.Optional(types.LuaError)}),
		},
	}

	m.SetExport(moduleType)
	return m
}
