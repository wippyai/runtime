// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
	luatty "github.com/wippyai/runtime/runtime/lua/modules/tty"
)

var executorType typ.Type
var processType typ.Type
var terminalCompletionType typ.Type
var terminalSessionType typ.Type
var ptyOptionsType typ.Type
var processOptionsType typ.Type

func init() {
	ptyOptionsType = typ.NewRecord().
		OptField("width", typ.Integer).
		OptField("height", typ.Integer).
		OptField("term", typ.String).
		Build()
	processOptionsType = typ.NewRecord().
		OptField("work_dir", typ.String).
		OptField("env", typ.NewMap(typ.String, typ.String)).
		OptField("pty", ptyOptionsType).
		Build()
	terminalCompletionType = typ.NewInterface("exec.TerminalCompletionChannel", []typ.Method{
		{Name: "receive", Type: typ.Func().Param("self", typ.Self).
			Returns(typ.Boolean, typ.Boolean).Build()},
		{Name: "case_receive", Type: typ.Func().Param("self", typ.Self).
			Returns(typ.Any).Build()},
	})
	terminalSessionType = typ.NewInterface("exec.TerminalSession", []typ.Method{
		{Name: "send", Type: typ.Func().Param("self", typ.Self).
			Param("event", luatty.EventType()).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "close", Type: typ.Func().Param("self", typ.Self).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "done", Type: typ.Func().Param("self", typ.Self).
			Returns(terminalCompletionType).Build()},
		{Name: "status", Type: typ.Func().Param("self", typ.Self).
			Returns(typ.NewUnion(typ.LiteralString("running"), typ.LiteralString("done")), typ.NewOptional(typ.LuaError)).Build()},
	})
	processType = typ.NewInterface("exec.Process", []typ.Method{
		{Name: "start", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "wait", Type: typ.Func().Param("self", typ.Self).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "signal", Type: typ.Func().Param("self", typ.Self).Param("sig", typ.Number).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "write_stdin", Type: typ.Func().Param("self", typ.Self).Param("data", typ.String).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "stdout_stream", Type: typ.Func().Param("self", typ.Self).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "stderr_stream", Type: typ.Func().Param("self", typ.Self).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "close", Type: typ.Func().Param("self", typ.Self).OptParam("force", typ.Boolean).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "resize", Type: typ.Func().Param("self", typ.Self).Param("width", typ.Integer).Param("height", typ.Integer).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "attach_terminal", Type: typ.Func().Param("self", typ.Self).
			Returns(terminalSessionType, typ.NewOptional(typ.LuaError)).Build()},
	})

	executorType = typ.NewInterface("exec.Executor", []typ.Method{
		{Name: "exec", Type: typ.Func().Param("self", typ.Self).
			Param("cmd", typ.String).
			OptParam("opts", processOptionsType).
			Returns(processType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "release", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	})
}

func ModuleTypes() *io.Manifest {
	m := io.NewManifest("exec")

	m.DefineType("Executor", executorType)
	m.DefineType("Process", processType)
	m.DefineType("TerminalCompletionChannel", terminalCompletionType)
	m.DefineType("TerminalSession", terminalSessionType)
	m.DefineType("PTYOptions", ptyOptionsType)
	m.DefineType("ProcessOptions", processOptionsType)

	moduleType := typ.NewInterface("exec", []typ.Method{
		{Name: "get", Type: typ.Func().Param("id", typ.String).Returns(executorType, typ.NewOptional(typ.LuaError)).Build()},
	})

	m.SetExport(moduleType)
	return m
}
