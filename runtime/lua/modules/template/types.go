// SPDX-License-Identifier: MPL-2.0

package template

import (
	typio "github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// Set type
var setType = typ.NewInterface("template.Set", []typ.Method{
	{
		Name: "render",
		Type: typ.Func().
			Param("self", typ.Self).
			Param("name", typ.String).
			OptParam("data", typ.Any).
			Returns(typ.String, typ.NewOptional(typ.LuaError)).
			Build(),
	},
	{
		Name: "release",
		Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean).Build(),
	},
})

// ModuleTypes returns the type manifest for the template module.
func ModuleTypes() *typio.Manifest {
	m := typio.NewManifest("templates")

	m.DefineType("Set", setType)

	moduleType := typ.NewInterface("templates", []typ.Method{
		{
			Name: "get",
			Type: typ.Func().Param("name", typ.String).Returns(setType, typ.NewOptional(typ.LuaError)).Build(),
		},
	})

	m.SetExport(moduleType)
	return m
}
