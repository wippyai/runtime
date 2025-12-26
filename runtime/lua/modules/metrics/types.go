package metrics

import (
	"github.com/yuin/gopher-lua/types"
)

// ModuleTypes returns the type manifest for the metrics module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("metrics")

	// Labels type accepts any values - runtime ignores non-string values
	labelsType := types.Optional(types.NewMap(types.String, types.Any, false))

	moduleType := &types.InterfaceType{
		Name: "metrics",
		Methods: map[string]*types.FunctionType{
			"counter_inc": types.NewFunction([]types.Type{types.String, labelsType}, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
			"counter_add": types.NewFunction([]types.Type{types.String, types.Number, labelsType}, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
			"gauge_set":   types.NewFunction([]types.Type{types.String, types.Number, labelsType}, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
			"gauge_inc":   types.NewFunction([]types.Type{types.String, labelsType}, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
			"gauge_dec":   types.NewFunction([]types.Type{types.String, labelsType}, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
			"histogram":   types.NewFunction([]types.Type{types.String, types.Number, labelsType}, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
		},
	}

	m.SetExport(moduleType)
	return m
}
