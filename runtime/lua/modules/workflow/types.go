// SPDX-License-Identifier: MPL-2.0

package workflow

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// WorkflowInfo type
var workflowInfoType = typ.NewRecord().
	Field("workflow_id", typ.String).
	Field("run_id", typ.String).
	Field("workflow_type", typ.String).
	Field("task_queue", typ.String).
	Field("namespace", typ.String).
	Field("attempt", typ.Number).
	Field("history_length", typ.Number).
	Field("history_size", typ.Number).
	Build()

// ModuleTypes returns the type manifest for the workflow module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("workflow")

	m.DefineType("Info", workflowInfoType)

	attrsInputType := typ.NewRecord().
		OptField("search", typ.NewMap(typ.String, typ.Any)).
		OptField("memo", typ.NewMap(typ.String, typ.Any)).
		Build()

	m.DefineType("AttrsInput", attrsInputType)

	moduleType := typ.NewInterface("workflow", []typ.Method{
		{Name: "exec", Type: typ.Func().
			Param("name", typ.String).
			Variadic(typ.Any).
			Returns(typ.Any, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "version", Type: typ.Func().
			Param("p1", typ.String).
			Param("p2", typ.Number).
			Param("p3", typ.Number).
			Returns(typ.Number, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "attrs", Type: typ.Func().
			Param("input", attrsInputType).
			Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "history_length", Type: typ.Func().
			Returns(typ.Number, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "history_size", Type: typ.Func().
			Returns(typ.Number, typ.NewOptional(typ.LuaError)).
			Build()},
		{Name: "info", Type: typ.Func().
			Returns(typ.NewOptional(workflowInfoType), typ.NewOptional(typ.LuaError)).
			Build()},
	})

	m.SetExport(moduleType)
	return m
}
