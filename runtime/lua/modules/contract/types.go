package contract

import (
	"github.com/yuin/gopher-lua/types/io"
	"github.com/yuin/gopher-lua/types/typ"
)

var schemaDefinitionType typ.Type

var methodDefinitionType typ.Type

var contractType typ.Type

func init() {
	schemaDefinitionType = typ.NewRecord().
		Field("format", typ.String).
		OptField("definition", typ.Any).
		Build()

	methodDefinitionType = typ.NewRecord().
		Field("name", typ.String).
		Field("description", typ.String).
		OptField("input_schemas", typ.NewArray(schemaDefinitionType)).
		OptField("output_schemas", typ.NewArray(schemaDefinitionType)).
		Build()

	contractType = typ.NewInterface("contract.Contract", []typ.Method{
		{Name: "id", Type: typ.Func().Param("self", typ.Self).Returns(typ.String).Build()},
		{Name: "methods", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewArray(methodDefinitionType)).Build()},
		{Name: "method", Type: typ.Func().Param("self", typ.Self).Param("name", typ.String).Returns(methodDefinitionType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "implementations", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewArray(typ.String), typ.NewOptional(typ.LuaError)).Build()},
		{Name: "open", Type: typ.Func().Param("self", typ.Self).OptParam("name", typ.String).OptParam("options", typ.Any).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "with_context", Type: typ.Func().Param("self", typ.Self).Param("ctx", typ.Any).Returns(typ.Self).Build()},
		{Name: "with_actor", Type: typ.Func().Param("self", typ.Self).Param("actor", typ.Any).Returns(typ.Self).Build()},
		{Name: "with_scope", Type: typ.Func().Param("self", typ.Self).Param("scope", typ.Any).Returns(typ.Self).Build()},
	})
}

func ModuleTypes() *io.Manifest {
	m := io.NewManifest("contract")

	m.DefineType("Contract", contractType)
	m.DefineType("MethodDefinition", methodDefinitionType)
	m.DefineType("SchemaDefinition", schemaDefinitionType)

	moduleType := typ.NewInterface("contract", []typ.Method{
		{Name: "get", Type: typ.Func().Param("name", typ.String).Returns(contractType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "open", Type: typ.Func().Param("name", typ.String).OptParam("options", typ.Any).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "find_implementations", Type: typ.Func().Param("name", typ.String).Returns(typ.NewArray(typ.String), typ.NewOptional(typ.LuaError)).Build()},
		{Name: "is", Type: typ.Func().Param("value", typ.Any).Param("name", typ.String).Returns(typ.Boolean).Build()},
	})

	m.SetExport(moduleType)
	return m
}
