// SPDX-License-Identifier: MPL-2.0

package registry

import (
	typio "github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

var loaderEntryType = typ.NewRecord().
	Field("id", typ.String).
	Field("kind", typ.String).
	Field("meta", typ.NewMap(typ.String, typ.Unknown)).
	Field("data", typ.Unknown).
	Build()

var loaderInstanceType = typ.NewInterface("registry.Loader", []typ.Method{
	{Name: "load_directory", Type: typ.Func().
		Param("self", typ.Self).
		Param("directory", typ.String).
		Returns(typ.NewArray(loaderEntryType), typ.NewOptional(typ.LuaError)).Build()},
	{Name: "load_file", Type: typ.Func().
		Param("self", typ.Self).
		Param("path", typ.String).
		Returns(typ.NewArray(loaderEntryType), typ.NewOptional(typ.LuaError)).Build()},
})

// LoaderTypes returns the type manifest for the source loader module.
func LoaderTypes() *typio.Manifest {
	m := typio.NewManifest(loaderModuleName)
	m.DefineType("Entry", loaderEntryType)
	m.DefineType("Loader", loaderInstanceType)
	m.SetExport(typ.NewInterface(loaderModuleName, []typ.Method{
		{Name: "new", Type: typ.Func().Param("filesystem", typ.String).
			Returns(loaderInstanceType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "load_sources", Type: typ.Func().
			Returns(typ.NewArray(loaderEntryType), typ.NewOptional(typ.LuaError)).Build()},
	}))
	return m
}
