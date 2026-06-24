// SPDX-License-Identifier: MPL-2.0

package component

import (
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"
)

// ExecutableAmbientModules are the modules injected into every executable Lua
// kind (function, process, workflow) on top of the engine base, and therefore
// require-able without an explicit import declaration. It is the single source
// the kind factories and lint share so the two cannot drift.
func ExecutableAmbientModules() []*luaapi.ModuleDef {
	return []*luaapi.ModuleDef{processmod.Module}
}

// ExecutableAmbientModuleNames returns the names of ExecutableAmbientModules.
func ExecutableAmbientModuleNames() []string {
	mods := ExecutableAmbientModules()
	names := make([]string, 0, len(mods))
	for _, m := range mods {
		names = append(names, m.Name)
	}
	return names
}
