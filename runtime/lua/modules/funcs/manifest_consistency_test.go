// SPDX-License-Identifier: MPL-2.0

package funcs_test

import (
	"testing"

	"github.com/wippyai/go-lua/types/typ"
	"github.com/wippyai/runtime/runtime/lua/modules/funcs"
	"github.com/wippyai/runtime/runtime/lua/modules/future"
)

// TestFutureManifestMatchesRegisteredMethods proves the funcs.Future type manifest
// and the methods the future package actually registers describe the same set.
//
// The Future type is declared in the funcs package's manifest, but its methods are
// registered in the separate future package. A method present in the manifest but
// absent from future.Methods is a phantom: the type system advertises
// Future:<method>, but the runtime never registers it, so calling it fails.
func TestFutureManifestMatchesRegisteredMethods(t *testing.T) {
	target, ok := funcs.ModuleTypes().LookupType("Future")
	if !ok {
		t.Fatal("Future type not found in funcs manifest")
	}

	iface, ok := target.(*typ.Interface)
	if !ok {
		t.Fatalf("Future manifest type is %T, want *typ.Interface", target)
	}

	registered := future.Methods

	manifest := make(map[string]bool, len(iface.Methods))
	for _, m := range iface.Methods {
		manifest[m.Name] = true
		if _, found := registered[m.Name]; !found {
			t.Errorf("funcs.Future manifest declares %q but the future package does not register it (phantom method; calling it fails at runtime)", m.Name)
		}
	}

	for name := range registered {
		if !manifest[name] {
			t.Errorf("future package registers %q but the funcs.Future manifest omits it (undocumented method)", name)
		}
	}
}
