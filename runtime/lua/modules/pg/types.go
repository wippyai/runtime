// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// ModuleTypes returns the type manifest for the pg module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("pg")

	moduleType := typ.NewInterface("pg", []typ.Method{
		{Name: "join", Type: typ.Func().Param("group", typ.String).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "leave", Type: typ.Func().Param("group", typ.String).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "get_members", Type: typ.Func().Param("group", typ.String).Returns(typ.NewArray(typ.String), typ.NewOptional(typ.LuaError)).Build()},
		{Name: "get_local_members", Type: typ.Func().Param("group", typ.String).Returns(typ.NewArray(typ.String), typ.NewOptional(typ.LuaError)).Build()},
		{Name: "which_groups", Type: typ.Func().Returns(typ.NewArray(typ.String), typ.NewOptional(typ.LuaError)).Build()},
		{Name: "broadcast", Type: typ.Func().Param("group", typ.String).Param("topic", typ.String).OptParam("payload", typ.Any).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "broadcast_local", Type: typ.Func().Param("group", typ.String).Param("topic", typ.String).OptParam("payload", typ.Any).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	})

	m.SetExport(moduleType)
	return m
}
