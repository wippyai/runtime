// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"testing"

	lua "github.com/wippyai/go-lua"
)

// TestAmbientBaseModuleNames_AllResolveAsGlobals guards against drift between
// AmbientBaseModuleNames and what the runtime actually installs: every advertised
// ambient name must be reachable as a global after the same base setup CreateState
// performs (OpenBase, BindCachedLibs, LoadCoreModules). The scoped require base
// fallback resolves names through these globals.
func TestAmbientBaseModuleNames_AllResolveAsGlobals(t *testing.T) {
	l := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer l.Close()

	lua.OpenBase(l)
	BindCachedLibs(l)
	if err := LoadCoreModules(l); err != nil {
		t.Fatalf("LoadCoreModules: %v", err)
	}

	names := AmbientBaseModuleNames()
	if len(names) == 0 {
		t.Fatal("AmbientBaseModuleNames returned no names")
	}
	for _, name := range names {
		if l.GetGlobal(name) == lua.LNil {
			t.Fatalf("ambient base name %q is not installed as a runtime global", name)
		}
	}
}
