package expr

import (
	"github.com/yuin/gopher-lua/types"
)

// Program type
var programType = &types.InterfaceType{
	Name: "expr.Program",
	Methods: map[string]*types.FunctionType{
		"run": types.NewFunction([]types.Type{types.Optional(types.Any)}, []types.Type{types.Any, types.Optional(types.LuaError)}),
	},
}

// ModuleTypes returns the type manifest for the expr module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("expr")

	m.DefineType("Program", programType)

	moduleType := &types.InterfaceType{
		Name: "expr",
		Methods: map[string]*types.FunctionType{
			"compile": types.NewFunction([]types.Type{types.String, types.Optional(types.Any)}, []types.Type{programType, types.Optional(types.LuaError)}),
			"eval":    types.NewFunction([]types.Type{types.String, types.Optional(types.Any)}, []types.Type{types.Any, types.Optional(types.LuaError)}),
		},
	}

	m.SetExport(moduleType)
	return m
}
