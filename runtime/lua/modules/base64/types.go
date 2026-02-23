// SPDX-License-Identifier: MPL-2.0

package base64

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

func ModuleTypes() *io.Manifest {
	m := io.NewManifest("base64")

	moduleType := typ.NewInterface("base64", []typ.Method{
		{Name: "encode", Type: typ.Func().Param("data", typ.String).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "decode", Type: typ.Func().Param("data", typ.String).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
	})

	m.SetExport(moduleType)
	return m
}
