package io

import (
	typio "github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// ModuleTypes returns the type manifest for the io module.
func ModuleTypes() *typio.Manifest {
	m := typio.NewManifest("io")

	moduleType := typ.NewInterface("io", []typ.Method{
		{
			Name: "write",
			Type: typ.Func().Variadic(typ.Any).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build(),
		},
		{
			Name: "print",
			Type: typ.Func().Variadic(typ.Any).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build(),
		},
		{
			Name: "eprint",
			Type: typ.Func().Variadic(typ.Any).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build(),
		},
		{
			Name: "read",
			Type: typ.Func().OptParam("n", typ.Number).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build(),
		},
		{
			Name: "readline",
			Type: typ.Func().Returns(typ.String, typ.NewOptional(typ.LuaError)).Build(),
		},
		{
			Name: "flush",
			Type: typ.Func().Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build(),
		},
		{
			Name: "args",
			Type: typ.Func().Returns(typ.NewArray(typ.String)).Build(),
		},
	})

	m.SetExport(moduleType)
	return m
}
