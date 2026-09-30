// SPDX-License-Identifier: MPL-2.0

package code_test

import (
	"slices"
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
		runtime *io.Manifest
		test    *io.Manifest
		name    string
	}{
		{engine.ChannelModuleTypes(), testutil.ChannelManifest(), "channel"},
		{time.ModuleTypes(), testutil.TimeManifest(), "time"},
		{funcs.ModuleTypes(), testutil.FuncsManifest(), "funcs"},
		{process.ModuleTypes(), scopedProcessCheckerManifest(t), "process"},
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

// The pinned go-lua fixture predates the optional lookup scope. Extend only
// that signature with an independent expectation; all other fixture types
// still participate in the exact parity comparison above.
func scopedProcessCheckerManifest(t *testing.T) *io.Manifest {
	t.Helper()
	m := testutil.ProcessManifest()
	found := false
	export := typ.Rewrite(m.Export, func(value typ.Type) (typ.Type, bool) {
		iface, ok := value.(*typ.Interface)
		if !ok || iface.Name != "process.registry" {
			return value, false
		}
		methods := slices.Clone(iface.Methods)
		for i, method := range methods {
			if method.Name == "lookup" {
				methods[i].Type = typ.Func().Param("name", typ.String).
					OptParam("scope", typ.Number).
					Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()
				found = true
			}
		}
		return typ.NewInterface(iface.Name, methods), true
	})
	if !found {
		t.Fatal("checker fixture is missing process.registry.lookup")
	}
	m.SetExport(export)
	return m
}

func TestScopedProcessCheckerManifestDoesNotMutateSharedFixture(t *testing.T) {
	before := testutil.ProcessManifest()
	scoped := scopedProcessCheckerManifest(t)
	after := testutil.ProcessManifest()
	if !typ.TypeEquals(before.Export, after.Export) {
		t.Fatal("scoped fixture mutated the shared go-lua types")
	}
	if typ.TypeEquals(scoped.Export, before.Export) {
		t.Fatal("scoped fixture did not extend the lookup signature")
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
