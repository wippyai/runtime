// SPDX-License-Identifier: MPL-2.0

package ostime

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// DateTable type returned by os.date("*t")
var dateTableType = typ.NewRecord().
	Field("year", typ.Number).
	Field("month", typ.Number).
	Field("day", typ.Number).
	Field("hour", typ.Number).
	Field("min", typ.Number).
	Field("sec", typ.Number).
	Field("wday", typ.Number).
	Field("yday", typ.Number).
	Field("isdst", typ.Boolean).
	Build()

// dateFuncType overloads os.date: "*t" and "!*t" return a date table, every
// other format and no format return the formatted string.
var dateFuncType = typ.NewUnion(
	typ.Func().
		Param("format", typ.NewUnion(typ.LiteralString("*t"), typ.LiteralString("!*t"))).
		OptParam("timestamp", typ.Number).
		Returns(dateTableType).
		Build(),
	typ.Func().
		OptParam("format", typ.String).
		OptParam("timestamp", typ.Number).
		Returns(typ.String).
		Build(),
)

// ModuleTypes returns the type manifest for the os module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("os")

	m.DefineType("DateTable", dateTableType)

	moduleFieldsType := typ.NewRecord().
		Field("platform", typ.String).
		Field("date", dateFuncType).
		Build()

	moduleMethodsType := typ.NewInterface("os", []typ.Method{
		{
			Name: "time",
			Type: typ.Func().OptParam("table", typ.Any).Returns(typ.Number).Build(),
		},
		{
			Name: "clock",
			Type: typ.Func().Returns(typ.Number).Build(),
		},
		{
			Name: "difftime",
			Type: typ.Func().Param("t2", typ.Number).Param("t1", typ.Number).Returns(typ.Number).Build(),
		},
	})

	m.SetExport(typ.NewIntersection(moduleMethodsType, moduleFieldsType))
	return m
}
