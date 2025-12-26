package workflow

import (
	"github.com/yuin/gopher-lua/types"
)

// WorkflowInfo type
var workflowInfoType = &types.RecordType{
	Name: "workflow.Info",
	Fields: []types.RecordField{
		{Name: "workflow_id", Type: types.String},
		{Name: "run_id", Type: types.String},
		{Name: "workflow_type", Type: types.String},
		{Name: "task_queue", Type: types.String},
		{Name: "namespace", Type: types.String},
		{Name: "attempt", Type: types.Number},
		{Name: "history_length", Type: types.Number},
		{Name: "history_size", Type: types.Number},
	},
}

// ModuleTypes returns the type manifest for the workflow module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("workflow")

	m.DefineType("Info", workflowInfoType)

	attrsInputType := &types.RecordType{
		Name: "workflow.AttrsInput",
		Fields: []types.RecordField{
			{Name: "search", Type: types.Optional(&types.MapType{Key: types.String, Value: types.Any})},
			{Name: "memo", Type: types.Optional(&types.MapType{Key: types.String, Value: types.Any})},
		},
	}

	m.DefineType("AttrsInput", attrsInputType)

	moduleType := &types.InterfaceType{
		Name: "workflow",
		Methods: map[string]*types.FunctionType{
			"call":           {Params: []types.Type{types.String}, Variadic: types.Any, Returns: []types.Type{types.Any, types.Optional(types.LuaError)}},
			"version":        types.NewFunction([]types.Type{types.String, types.Number, types.Number}, []types.Type{types.Number, types.Optional(types.LuaError)}),
			"attrs":          types.NewFunction([]types.Type{attrsInputType}, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
			"history_length": types.NewFunction(nil, []types.Type{types.Number, types.Optional(types.LuaError)}),
			"history_size":   types.NewFunction(nil, []types.Type{types.Number, types.Optional(types.LuaError)}),
			"info":           types.NewFunction(nil, []types.Type{types.Optional(workflowInfoType), types.Optional(types.LuaError)}),
		},
	}

	m.SetExport(moduleType)
	return m
}
