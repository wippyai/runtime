package json

import (
	"github.com/yuin/gopher-lua/types/io"
	"github.com/yuin/gopher-lua/types/typ"
)

// ModuleTypes returns the type manifest for the json module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("json")

	// Schema accepts any table structure or string reference
	schemaParam := typ.NewUnion(typ.Any, typ.String)

	moduleType := typ.NewInterface("json", []typ.Method{
		{
			Name: "encode",
			Type: typ.Func().Param("value", typ.Any).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build(),
		},
		{
			Name: "decode",
			Type: typ.Func().Param("str", typ.String).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build(),
		},
		{
			Name: "validate",
			Type: typ.Func().Param("schema", schemaParam).Param("data", typ.Any).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build(),
		},
		{
			Name: "validate_string",
			Type: typ.Func().Param("schema", schemaParam).Param("str", typ.String).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build(),
		},
	})

	m.SetExport(moduleType)
	return m
}
