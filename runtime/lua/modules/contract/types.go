package contract

import (
	"github.com/yuin/gopher-lua/types"
)

// SchemaDefinition type
var schemaDefinitionType = &types.RecordType{
	Name: "contract.SchemaDefinition",
	Fields: []types.RecordField{
		{Name: "format", Type: types.String},
		{Name: "definition", Type: types.Optional(types.Any)},
	},
}

// MethodDefinition type
var methodDefinitionType = &types.RecordType{
	Name: "contract.MethodDefinition",
	Fields: []types.RecordField{
		{Name: "name", Type: types.String},
		{Name: "description", Type: types.String},
		{Name: "input_schemas", Type: types.Optional(types.NewArray(schemaDefinitionType, false))},
		{Name: "output_schemas", Type: types.Optional(types.NewArray(schemaDefinitionType, false))},
	},
}

// Forward declaration for self-referential type
var contractType *types.InterfaceType

func init() {
	// Contract type (self-referential via with_context/with_actor/with_scope)
	contractType = &types.InterfaceType{
		Name:    "contract.Contract",
		Methods: map[string]*types.FunctionType{},
	}
	contractType.Methods["id"] = types.NewFunction(nil, []types.Type{types.String})
	contractType.Methods["methods"] = types.NewFunction(nil, []types.Type{types.NewArray(methodDefinitionType, false)})
	contractType.Methods["method"] = types.NewFunction([]types.Type{types.String}, []types.Type{methodDefinitionType, types.Optional(types.LuaError)})
	contractType.Methods["implementations"] = types.NewFunction(nil, []types.Type{types.NewArray(types.String, false), types.Optional(types.LuaError)})
	contractType.Methods["open"] = types.NewFunction([]types.Type{types.Optional(types.String), types.Optional(types.Any)}, []types.Type{types.Any, types.Optional(types.LuaError)})
	contractType.Methods["with_context"] = types.NewFunction([]types.Type{types.Any}, []types.Type{contractType})
	contractType.Methods["with_actor"] = types.NewFunction([]types.Type{types.Any}, []types.Type{contractType, types.Optional(types.LuaError)})
	contractType.Methods["with_scope"] = types.NewFunction([]types.Type{types.Any}, []types.Type{contractType, types.Optional(types.LuaError)})
}

// ModuleTypes returns the type manifest for the contract module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("contract")

	m.DefineType("Contract", contractType)
	m.DefineType("MethodDefinition", methodDefinitionType)
	m.DefineType("SchemaDefinition", schemaDefinitionType)

	moduleType := &types.InterfaceType{
		Name: "contract",
		Methods: map[string]*types.FunctionType{
			"get":                  types.NewFunction([]types.Type{types.String}, []types.Type{contractType, types.Optional(types.LuaError)}),
			"open":                 types.NewFunction([]types.Type{types.String, types.Optional(types.Any)}, []types.Type{types.Any, types.Optional(types.LuaError)}),
			"find_implementations": types.NewFunction([]types.Type{types.String}, []types.Type{types.NewArray(types.String, false), types.Optional(types.LuaError)}),
			"is":                   types.NewFunction([]types.Type{types.Any, types.String}, []types.Type{types.Boolean}),
		},
	}

	m.SetExport(moduleType)
	return m
}
