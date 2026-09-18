// SPDX-License-Identifier: MPL-2.0

package toml

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// ModuleTypes describes the toml Lua surface.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("toml")

	moduleType := typ.NewInterface("toml", []typ.Method{
		{Name: "encode", Type: typ.Func().Param("value", typ.Any).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "decode", Type: typ.Func().Param("str", typ.String).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
	})

	m.SetExport(moduleType)
	return m
}
