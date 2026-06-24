// SPDX-License-Identifier: MPL-2.0

package component

import (
	"testing"

	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"
)

func TestExecutableAmbientModules_IncludesProcess(t *testing.T) {
	found := false
	for _, m := range ExecutableAmbientModules() {
		if m == processmod.Module {
			found = true
		}
	}
	if !found {
		t.Fatal("ExecutableAmbientModules must include the process module")
	}
}

func TestExecutableAmbientModuleNames_MatchModules(t *testing.T) {
	mods := ExecutableAmbientModules()
	names := ExecutableAmbientModuleNames()
	if len(names) != len(mods) {
		t.Fatalf("name count %d does not match module count %d", len(names), len(mods))
	}
	for i, m := range mods {
		if names[i] != m.Name {
			t.Fatalf("name %d = %q, want module name %q", i, names[i], m.Name)
		}
	}
}
