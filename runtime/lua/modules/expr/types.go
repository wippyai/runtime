// SPDX-License-Identifier: MPL-2.0

package expr

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// Program type
var programType = typ.NewInterface("expr.Program", []typ.Method{
	{Name: "run", Type: typ.Func().Param("self", typ.Self).OptParam("context", typ.Any).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
})

// ModuleTypes returns the type manifest for the expr module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("expr")

	m.DefineType("Program", programType)

	moduleType := typ.NewInterface("expr", []typ.Method{
		{Name: "compile", Type: typ.Func().Param("text", typ.String).OptParam("context", typ.Any).Returns(programType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "eval", Type: typ.Func().Param("text", typ.String).OptParam("context", typ.Any).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
	})

	m.SetExport(moduleType)
	return m
}
