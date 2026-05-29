// SPDX-License-Identifier: MPL-2.0

package excel

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// Workbook type
var workbookType = typ.NewInterface("excel.Workbook", []typ.Method{
	{Name: "new_sheet", Type: typ.Func().Param("self", typ.Self).Param("name", typ.String).Returns(typ.Number, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "get_sheet_list", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewArray(typ.String), typ.NewOptional(typ.LuaError)).Build()},
	{Name: "get_rows", Type: typ.Func().Param("self", typ.Self).Param("sheet", typ.String).Returns(typ.NewArray(typ.NewArray(typ.String)), typ.NewOptional(typ.LuaError)).Build()},
	{Name: "set_cell_value", Type: typ.Func().Param("self", typ.Self).Param("sheet", typ.String).Param("cell", typ.String).Param("value", typ.Any).Returns(typ.NewOptional(typ.LuaError)).Build()},
	{Name: "write_to", Type: typ.Func().Param("self", typ.Self).Param("dest", typ.Any).Returns(typ.NewOptional(typ.LuaError)).Build()},
	{Name: "close", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewOptional(typ.LuaError)).Build()},
})

// ModuleTypes returns the type manifest for the excel module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("excel")

	m.DefineType("Workbook", workbookType)

	moduleType := typ.NewInterface("excel", []typ.Method{
		{Name: "new", Type: typ.Func().Returns(workbookType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "open", Type: typ.Func().Param("source", typ.Any).Returns(workbookType, typ.NewOptional(typ.LuaError)).Build()},
	})

	m.SetExport(moduleType)
	return m
}
