package funcs

import (
	"github.com/yuin/gopher-lua/types"
)

// Future type
var futureType = &types.InterfaceType{
	Name: "funcs.Future",
	Methods: map[string]*types.FunctionType{
		"response":    types.NewFunction([]types.Type{types.Self}, []types.Type{types.Any}),
		"channel":     types.NewFunction([]types.Type{types.Self}, []types.Type{types.Any}),
		"is_complete": types.NewFunction([]types.Type{types.Self}, []types.Type{types.Boolean}),
		"is_canceled": types.NewFunction([]types.Type{types.Self}, []types.Type{types.Boolean}),
		"result":      types.NewFunction([]types.Type{types.Self}, []types.Type{types.Any, types.Optional(types.LuaError)}),
		"error":       types.NewFunction([]types.Type{types.Self}, []types.Type{types.Optional(types.LuaError), types.Boolean}),
		"cancel":      types.NewFunction([]types.Type{types.Self}, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
		"await":       types.NewFunction([]types.Type{types.Self}, []types.Type{types.Any, types.Optional(types.LuaError)}),
	},
}

// Forward declaration for self-referential Executor type
var executorType *types.InterfaceType

func init() {
	executorType = &types.InterfaceType{
		Name:    "funcs.Executor",
		Methods: map[string]*types.FunctionType{},
	}
	executorType.Methods["with_context"] = types.NewFunction([]types.Type{types.Self, types.Any}, []types.Type{executorType})
	executorType.Methods["with_actor"] = types.NewFunction([]types.Type{types.Self, types.Any}, []types.Type{executorType})
	executorType.Methods["with_scope"] = types.NewFunction([]types.Type{types.Self, types.Any}, []types.Type{executorType})
	executorType.Methods["with_options"] = types.NewFunction([]types.Type{types.Self, types.Any}, []types.Type{executorType})
	executorType.Methods["call"] = &types.FunctionType{Params: []types.Type{types.Self, types.String}, Variadic: types.Any, Returns: []types.Type{types.Any, types.Optional(types.LuaError)}}
	executorType.Methods["async"] = &types.FunctionType{Params: []types.Type{types.Self, types.String}, Variadic: types.Any, Returns: []types.Type{futureType, types.Optional(types.LuaError)}}
}

// ModuleTypes returns the type manifest for the funcs module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("funcs")

	m.DefineType("Executor", executorType)
	m.DefineType("Future", futureType)

	moduleType := &types.InterfaceType{
		Name: "funcs",
		Methods: map[string]*types.FunctionType{
			"new":   types.NewFunction(nil, []types.Type{executorType}),
			"call":  {Params: []types.Type{types.String}, Variadic: types.Any, Returns: []types.Type{types.Any, types.Optional(types.LuaError)}},
			"async": {Params: []types.Type{types.String}, Variadic: types.Any, Returns: []types.Type{futureType, types.Optional(types.LuaError)}},
		},
	}

	m.SetExport(moduleType)
	return m
}
