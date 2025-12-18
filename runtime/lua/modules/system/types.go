package system

import "github.com/yuin/gopher-lua/types"

// MemStats type
var memStatsType = &types.RecordType{
	Name: "system.MemStats",
	Fields: []types.RecordField{
		{Name: "alloc", Type: types.Number},
		{Name: "total_alloc", Type: types.Number},
		{Name: "sys", Type: types.Number},
		{Name: "heap_alloc", Type: types.Number},
		{Name: "heap_sys", Type: types.Number},
		{Name: "heap_idle", Type: types.Number},
		{Name: "heap_in_use", Type: types.Number},
		{Name: "heap_released", Type: types.Number},
		{Name: "heap_objects", Type: types.Number},
		{Name: "stack_in_use", Type: types.Number},
		{Name: "stack_sys", Type: types.Number},
		{Name: "mspan_in_use", Type: types.Number},
		{Name: "mspan_sys", Type: types.Number},
		{Name: "num_gc", Type: types.Number},
		{Name: "next_gc", Type: types.Number},
	},
}

// ModuleInfo type
var moduleInfoType = &types.RecordType{
	Name: "system.ModuleInfo",
	Fields: []types.RecordField{
		{Name: "name", Type: types.String},
		{Name: "description", Type: types.String},
		{Name: "class", Type: types.NewArray(types.String, false)},
	},
}

// memory submodule type
var memoryType = &types.InterfaceType{
	Name: "system.memory",
	Methods: map[string]*types.FunctionType{
		"stats":        types.NewFunction(nil, []types.Type{memStatsType, types.Optional(types.LuaError)}),
		"allocated":    types.NewFunction(nil, []types.Type{types.Number, types.Optional(types.LuaError)}),
		"heap_objects": types.NewFunction(nil, []types.Type{types.Number, types.Optional(types.LuaError)}),
		"set_limit":    types.NewFunction([]types.Type{types.Number}, []types.Type{types.Number, types.Optional(types.LuaError)}),
		"get_limit":    types.NewFunction(nil, []types.Type{types.Number, types.Optional(types.LuaError)}),
	},
}

// gc submodule type
var gcType = &types.InterfaceType{
	Name: "system.gc",
	Methods: map[string]*types.FunctionType{
		"collect":     types.NewFunction(nil, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
		"set_percent": types.NewFunction([]types.Type{types.Number}, []types.Type{types.Number, types.Optional(types.LuaError)}),
		"get_percent": types.NewFunction(nil, []types.Type{types.Number, types.Optional(types.LuaError)}),
	},
}

// runtime submodule type
var runtimeType = &types.InterfaceType{
	Name: "system.runtime",
	Methods: map[string]*types.FunctionType{
		"goroutines": types.NewFunction(nil, []types.Type{types.Number, types.Optional(types.LuaError)}),
		"max_procs":  types.NewFunction([]types.Type{types.Optional(types.Number)}, []types.Type{types.Number, types.Optional(types.LuaError)}),
		"cpu_count":  types.NewFunction(nil, []types.Type{types.Number, types.Optional(types.LuaError)}),
	},
}

// process submodule type
var processSubType = &types.InterfaceType{
	Name: "system.process",
	Methods: map[string]*types.FunctionType{
		"pid":      types.NewFunction(nil, []types.Type{types.Number, types.Optional(types.LuaError)}),
		"hostname": types.NewFunction(nil, []types.Type{types.String, types.Optional(types.LuaError)}),
	},
}

// supervisor submodule type
var supervisorType = &types.InterfaceType{
	Name: "system.supervisor",
	Methods: map[string]*types.FunctionType{
		"state":  types.NewFunction(nil, []types.Type{types.String, types.Optional(types.LuaError)}),
		"states": types.NewFunction(nil, []types.Type{types.NewMap(types.String, types.String, false), types.Optional(types.LuaError)}),
	},
}

// ModuleTypes returns the type manifest for the system module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("system")

	m.DefineType("MemStats", memStatsType)
	m.DefineType("ModuleInfo", moduleInfoType)

	moduleType := &types.InterfaceType{
		Name: "system",
		Fields: map[string]types.Type{
			"memory":     memoryType,
			"gc":         gcType,
			"runtime":    runtimeType,
			"process":    processSubType,
			"supervisor": supervisorType,
		},
		Methods: map[string]*types.FunctionType{
			"exit":    types.NewFunction([]types.Type{types.Optional(types.Number)}, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
			"modules": types.NewFunction(nil, []types.Type{types.NewArray(moduleInfoType, false), types.Optional(types.LuaError)}),
		},
	}

	m.SetExport(moduleType)
	return m
}
