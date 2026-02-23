// SPDX-License-Identifier: MPL-2.0

package logger

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// ModuleTypes returns the type manifest for the logger module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("logger")

	// Logger type with self-referencing methods
	loggerType := typ.NewInterface("logger.Logger", []typ.Method{
		{Name: "debug", Type: typ.Func().Param("self", typ.Self).Param("msg", typ.String).OptParam("context", typ.Any).Build()},
		{Name: "info", Type: typ.Func().Param("self", typ.Self).Param("msg", typ.String).OptParam("context", typ.Any).Build()},
		{Name: "warn", Type: typ.Func().Param("self", typ.Self).Param("msg", typ.String).OptParam("context", typ.Any).Build()},
		{Name: "error", Type: typ.Func().Param("self", typ.Self).Param("msg", typ.String).OptParam("context", typ.Any).Build()},
		{Name: "with", Type: typ.Func().Param("self", typ.Self).Param("context", typ.Any).Returns(typ.Self).Build()},
		{Name: "named", Type: typ.Func().Param("self", typ.Self).Param("name", typ.String).Returns(typ.Self).Build()},
	})

	m.DefineType("Logger", loggerType)
	m.SetExport(loggerType)

	return m
}
