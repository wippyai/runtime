// SPDX-License-Identifier: MPL-2.0

package funcs

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// Future type
var futureType = typ.NewInterface("funcs.Future", []typ.Method{
	{Name: "response", Type: typ.Func().Param("self", typ.Self).Returns(typ.Any).Build()},
	{Name: "channel", Type: typ.Func().Param("self", typ.Self).Returns(typ.Any).Build()},
	{Name: "is_complete", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean).Build()},
	{Name: "is_canceled", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean).Build()},
	{Name: "result", Type: typ.Func().Param("self", typ.Self).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "error", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewOptional(typ.LuaError), typ.Boolean).Build()},
	{Name: "cancel", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
})

// Forward declaration for self-referential Executor type
var executorType typ.Type

func init() {
	executorTypeMethods := []typ.Method{
		{Name: "with_context", Type: typ.Func().Param("self", typ.Self).Param("ctx", typ.Any).Returns(typ.Self).Build()},
		{Name: "with_actor", Type: typ.Func().Param("self", typ.Self).Param("actor", typ.Any).Returns(typ.Self).Build()},
		{Name: "with_scope", Type: typ.Func().Param("self", typ.Self).Param("scope", typ.Any).Returns(typ.Self).Build()},
		{Name: "with_options", Type: typ.Func().Param("self", typ.Self).Param("options", typ.Any).Returns(typ.Self).Build()},
		{Name: "call", Type: typ.Func().Param("self", typ.Self).Param("name", typ.String).Variadic(typ.Any).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "async", Type: typ.Func().Param("self", typ.Self).Param("name", typ.String).Variadic(typ.Any).Returns(futureType, typ.NewOptional(typ.LuaError)).Build()},
	}
	executorType = typ.NewInterface("funcs.Executor", executorTypeMethods)
}

// ModuleTypes returns the type manifest for the funcs module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("funcs")

	m.DefineType("Executor", executorType)
	m.DefineType("Future", futureType)

	moduleType := typ.NewInterface("funcs", []typ.Method{
		{Name: "new", Type: typ.Func().Returns(executorType).Build()},
		{Name: "call", Type: typ.Func().Param("name", typ.String).Variadic(typ.Any).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "async", Type: typ.Func().Param("name", typ.String).Variadic(typ.Any).Returns(futureType, typ.NewOptional(typ.LuaError)).Build()},
	})

	m.SetExport(moduleType)
	return m
}
