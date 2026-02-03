package uuid

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

var uuidInfoType = typ.NewRecord().
	Field("version", typ.Number).
	Field("variant", typ.String).
	OptField("timestamp", typ.Number).
	OptField("node", typ.String).
	Build()

func ModuleTypes() *io.Manifest {
	m := io.NewManifest("uuid")

	m.DefineType("Info", uuidInfoType)

	moduleType := typ.NewInterface("uuid", []typ.Method{
		{Name: "v1", Type: typ.Func().
			Returns(typ.String, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "v3", Type: typ.Func().
			Param("namespace", typ.String).
			Param("name", typ.String).
			Returns(typ.String, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "v4", Type: typ.Func().
			Returns(typ.String, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "v5", Type: typ.Func().
			Param("namespace", typ.String).
			Param("name", typ.String).
			Returns(typ.String, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "v7", Type: typ.Func().
			Returns(typ.String, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "validate", Type: typ.Func().
			Param("id", typ.String).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "version", Type: typ.Func().
			Param("id", typ.String).
			Returns(typ.Number, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "variant", Type: typ.Func().
			Param("id", typ.String).
			Returns(typ.String, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "parse", Type: typ.Func().
			Param("id", typ.String).
			Returns(uuidInfoType, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "format", Type: typ.Func().
			Param("id", typ.String).
			OptParam("format", typ.String).
			Returns(typ.String, typ.NewOptional(typ.LuaError)).
			Build()},
	})

	m.SetExport(moduleType)
	return m
}
