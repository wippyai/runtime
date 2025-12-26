package exec

import (
	"github.com/yuin/gopher-lua/types"
)

// Executor type
var executorType = &types.InterfaceType{
	Name: "exec.Executor",
	Methods: map[string]*types.FunctionType{
		"exec":    types.NewFunction([]types.Type{types.String, types.Optional(types.Any)}, []types.Type{processType, types.Optional(types.LuaError)}),
		"release": types.NewFunction(nil, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
	},
}

// Process type
var processType = &types.InterfaceType{
	Name: "exec.Process",
	Methods: map[string]*types.FunctionType{
		"start":         types.NewFunction(nil, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
		"wait":          types.NewFunction(nil, []types.Type{types.Any, types.Optional(types.LuaError)}),
		"signal":        types.NewFunction([]types.Type{types.Number}, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
		"write_stdin":   types.NewFunction([]types.Type{types.String}, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
		"stdout_stream": types.NewFunction(nil, []types.Type{types.Any, types.Optional(types.LuaError)}),
		"stderr_stream": types.NewFunction(nil, []types.Type{types.Any, types.Optional(types.LuaError)}),
		"close":         types.NewFunction([]types.Type{types.Optional(types.Boolean)}, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
	},
}

// ModuleTypes returns the type manifest for the exec module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("exec")

	m.DefineType("Executor", executorType)
	m.DefineType("Process", processType)

	moduleType := &types.InterfaceType{
		Name: "exec",
		Methods: map[string]*types.FunctionType{
			"get": types.NewFunction([]types.Type{types.String}, []types.Type{executorType, types.Optional(types.LuaError)}),
		},
	}

	m.SetExport(moduleType)
	return m
}
