// SPDX-License-Identifier: MPL-2.0

package yaml

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

var encodeOptionsType typ.Type

func init() {
	encodeOptionsType = typ.NewRecord().
		Field("field_order", typ.NewArray(typ.String)).
		Field("sort_unordered", typ.Boolean).
		Build()
}

func ModuleTypes() *io.Manifest {
	m := io.NewManifest("yaml")

	m.DefineType("EncodeOptions", encodeOptionsType)

	moduleType := typ.NewInterface("yaml", []typ.Method{
		{Name: "encode", Type: typ.Func().Param("value", typ.Any).OptParam("options", encodeOptionsType).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "decode", Type: typ.Func().Param("str", typ.String).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
	})

	m.SetExport(moduleType)
	return m
}
