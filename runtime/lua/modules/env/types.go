package env

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// ModuleTypes returns the type manifest for the env module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("env")

	moduleType := typ.NewInterface("env", []typ.Method{
		{Name: "get", Type: typ.Func().Param("key", typ.String).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "set", Type: typ.Func().Param("key", typ.String).Param("value", typ.String).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "get_all", Type: typ.Func().Returns(typ.NewMap(typ.String, typ.String), typ.NewOptional(typ.LuaError)).Build()},
	})

	m.SetExport(moduleType)
	return m
}
