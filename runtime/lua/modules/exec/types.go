package exec

import (
	"github.com/yuin/gopher-lua/types/io"
	"github.com/yuin/gopher-lua/types/typ"
)

var executorType typ.Type
var processType typ.Type

func init() {
	processType = typ.NewInterface("exec.Process", []typ.Method{
		{Name: "start", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "wait", Type: typ.Func().Param("self", typ.Self).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "signal", Type: typ.Func().Param("self", typ.Self).Param("sig", typ.Number).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "write_stdin", Type: typ.Func().Param("self", typ.Self).Param("data", typ.String).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "stdout_stream", Type: typ.Func().Param("self", typ.Self).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "stderr_stream", Type: typ.Func().Param("self", typ.Self).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "close", Type: typ.Func().Param("self", typ.Self).OptParam("force", typ.Boolean).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	})

	executorType = typ.NewInterface("exec.Executor", []typ.Method{
		{Name: "exec", Type: typ.Func().Param("self", typ.Self).Param("cmd", typ.String).OptParam("opts", typ.Any).Returns(processType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "release", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	})
}

func ModuleTypes() *io.Manifest {
	m := io.NewManifest("exec")

	m.DefineType("Executor", executorType)
	m.DefineType("Process", processType)

	moduleType := typ.NewInterface("exec", []typ.Method{
		{Name: "get", Type: typ.Func().Param("cmd", typ.String).Returns(executorType, typ.NewOptional(typ.LuaError)).Build()},
	})

	m.SetExport(moduleType)
	return m
}
