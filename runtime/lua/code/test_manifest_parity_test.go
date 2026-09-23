// SPDX-License-Identifier: MPL-2.0

package code_test

import (
	"sort"
	"testing"

	"github.com/wippyai/go-lua/compiler/check/tests/testutil"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/modules/funcs"
	"github.com/wippyai/runtime/runtime/lua/modules/process"
	"github.com/wippyai/runtime/runtime/lua/modules/time"
)

// The go-lua checker tests model wippy modules with testutil manifests. They
// must describe the modules exactly as the runtime declares them, or the
// checker is tested against APIs that do not exist.
func TestCheckerTestManifestsMatchRuntimeModules(t *testing.T) {
	cases := []struct {
		name    string
		runtime *io.Manifest
		test    *io.Manifest
	}{
		{"channel", engine.ChannelModuleTypes(), testutil.ChannelManifest()},
		{"time", time.ModuleTypes(), testutil.TimeManifest()},
		{"funcs", funcs.ModuleTypes(), testutil.FuncsManifest()},
		{"process", process.ModuleTypes(), testutil.ProcessManifest()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.test.Path != tc.runtime.Path {
				t.Errorf("path = %q, want %q", tc.test.Path, tc.runtime.Path)
			}
			if !typ.TypeEquals(tc.test.Export, tc.runtime.Export) {
				t.Errorf("export differs:\n test:    %s\n runtime: %s", tc.test.Export, tc.runtime.Export)
			}
			if got, want := typeNames(tc.test), typeNames(tc.runtime); !equalStrings(got, want) {
				t.Errorf("defined types = %v, want %v", got, want)
			}
			for _, name := range typeNames(tc.runtime) {
				want, _ := tc.runtime.LookupType(name)
				got, ok := tc.test.LookupType(name)
				if ok && !typ.TypeEquals(got, want) {
					t.Errorf("type %s differs:\n test:    %s\n runtime: %s", name, got, want)
				}
			}
		})
	}
}

func typeNames(m *io.Manifest) []string {
	names := make([]string, 0, len(m.Types))
	for name := range m.Types {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
