// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// Instance type — acquired PG scope resource returned by pg.open().
var instanceType = typ.NewInterface("pg.Instance", []typ.Method{
	{Name: "join", Type: typ.Func().Param("self", typ.Self).Param("group", typ.NewUnion(typ.String, typ.NewArray(typ.String))).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "leave", Type: typ.Func().Param("self", typ.Self).Param("group", typ.NewUnion(typ.String, typ.NewArray(typ.String))).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "get_members", Type: typ.Func().Param("self", typ.Self).Param("group", typ.String).Returns(typ.NewArray(typ.String), typ.NewOptional(typ.LuaError)).Build()},
	{Name: "get_local_members", Type: typ.Func().Param("self", typ.Self).Param("group", typ.String).Returns(typ.NewArray(typ.String), typ.NewOptional(typ.LuaError)).Build()},
	{Name: "which_groups", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewArray(typ.String), typ.NewOptional(typ.LuaError)).Build()},
	{Name: "which_local_groups", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewArray(typ.String), typ.NewOptional(typ.LuaError)).Build()},
	{Name: "broadcast", Type: typ.Func().Param("self", typ.Self).Param("group", typ.String).Param("topic", typ.String).OptParam("payload", typ.Any).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "broadcast_local", Type: typ.Func().Param("self", typ.Self).Param("group", typ.String).Param("topic", typ.String).OptParam("payload", typ.Any).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "events", Type: typ.Func().Param("self", typ.Self).Returns(typ.Any, typ.Any, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "monitor", Type: typ.Func().Param("self", typ.Self).Param("group", typ.String).Returns(typ.Any, typ.NewArray(typ.String), typ.NewOptional(typ.LuaError)).Build()},
	{Name: "release", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean).Build()},
})

// ModuleTypes returns the type manifest for the pg module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("pg")

	m.DefineType("Instance", instanceType)

	moduleType := typ.NewInterface("pg", []typ.Method{
		{Name: "open", Type: typ.Func().Param("id", typ.String).Returns(instanceType, typ.NewOptional(typ.LuaError)).Build()},
	})

	m.SetExport(moduleType)
	return m
}
