// SPDX-License-Identifier: MPL-2.0

package replayer

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// ModuleTypes returns the type manifest for the replayer module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("replayer")

	moduleType := typ.NewInterface("replayer", []typ.Method{
		{Name: "replay_json_file", Type: typ.Func().
			Param("workflow_id", typ.Any).
			Param("history_json_path", typ.Any).
			Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
	})

	m.SetExport(moduleType)
	return m
}
