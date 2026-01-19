package ctx

import (
	"github.com/yuin/gopher-lua/types/io"
	"github.com/yuin/gopher-lua/types/typ"
)

// ModuleTypes returns the type manifest for the ctx module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("ctx")

	moduleType := typ.NewInterface("ctx", []typ.Method{
		{Name: "get", Type: typ.Func().Param("key", typ.Any).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "all", Type: typ.Func().Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
	})

	m.SetExport(moduleType)
	return m
}
