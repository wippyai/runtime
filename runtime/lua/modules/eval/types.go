package eval

import "github.com/yuin/gopher-lua/types"

// CompileOptions type
var compileOptionsType = &types.RecordType{
	Name: "eval.CompileOptions",
	Fields: []types.RecordField{
		{Name: "modules", Type: types.Optional(types.NewArray(types.String, false))},
	},
}

// RunConfig type
var runConfigType = &types.RecordType{
	Name: "eval.RunConfig",
	Fields: []types.RecordField{
		{Name: "source", Type: types.String},
		{Name: "method", Type: types.Optional(types.String)},
		{Name: "modules", Type: types.Optional(types.NewArray(types.String, false))},
		{Name: "args", Type: types.Optional(types.NewArray(types.Any, false))},
		{Name: "context", Type: types.Optional(types.Any)},
	},
}

// SandboxOptions type
var sandboxOptionsType = &types.RecordType{
	Name: "eval.SandboxOptions",
	Fields: []types.RecordField{
		{Name: "modules", Type: types.Optional(types.NewArray(types.String, false))},
	},
}

// Program type
var evalProgramType = &types.InterfaceType{
	Name: "eval.Program",
	Methods: map[string]*types.FunctionType{
		"run": {Params: []types.Type{types.Optional(types.Any)}, Variadic: types.Any, Returns: []types.Type{types.Any, types.Optional(types.LuaError)}},
	},
}

// Sandbox type
var evalSandboxType = &types.InterfaceType{
	Name: "eval.Sandbox",
	Methods: map[string]*types.FunctionType{
		"run":  {Params: []types.Type{types.Optional(types.String)}, Variadic: types.Any, Returns: []types.Type{types.Any, types.Optional(types.LuaError)}},
		"call": {Params: []types.Type{types.String}, Variadic: types.Any, Returns: []types.Type{types.Any, types.Optional(types.LuaError)}},
	},
}

// ModuleTypes returns the type manifest for the eval module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("eval")

	m.DefineType("Program", evalProgramType)
	m.DefineType("Sandbox", evalSandboxType)
	m.DefineType("CompileOptions", compileOptionsType)
	m.DefineType("RunConfig", runConfigType)
	m.DefineType("SandboxOptions", sandboxOptionsType)

	moduleType := &types.InterfaceType{
		Name: "eval",
		Methods: map[string]*types.FunctionType{
			"compile": types.NewFunction([]types.Type{types.String, types.Optional(types.String), types.Optional(compileOptionsType)}, []types.Type{evalProgramType, types.Optional(types.LuaError)}),
			"run":     types.NewFunction([]types.Type{runConfigType}, []types.Type{types.Any, types.Optional(types.LuaError)}),
			"sandbox": types.NewFunction([]types.Type{types.String, types.Optional(sandboxOptionsType)}, []types.Type{evalSandboxType}),
		},
	}

	m.SetExport(moduleType)
	return m
}
