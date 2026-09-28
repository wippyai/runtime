// SPDX-License-Identifier: MPL-2.0

package code

import (
	"strings"
	"testing"

	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
	api "github.com/wippyai/runtime/api/runtime/lua"
)

func TestBuiltinManifestOverrideReachesAllCheckerSurfaces(t *testing.T) {
	generic := io.NewManifest("contract")
	generic.SetExport(typ.NewRecord().Field("get", typ.Func().Returns(typ.String).Build()).Build())
	override := io.NewManifest("contract")
	override.DefineType("Special", typ.Integer)
	override.SetExport(typ.NewRecord().Field("get", typ.Func().Returns(typ.Integer).Build()).Build())
	checker := NewTypeCheckerWithManifests(TypeCheckConfig{Enabled: true, Strict: true}, []*api.ModuleDef{{Name: "contract", Types: func() *io.Manifest { return generic }}}, map[string]*io.Manifest{"contract": override})
	for _, tc := range []*TypeChecker{checker, checker.Clone()} {
		if tc.BuiltinManifest("contract") != override || tc.db.Manifest("contract") != override || tc.GlobalTypes()["contract"] == nil {
			t.Fatal("override missing from manifest database or value environment")
		}
		_, diagnostics, err := tc.Check(`local value: contract.Special = contract.get(); local wrong: string = value`, "override.lua", nil)
		if err != nil {
			t.Fatal(err)
		}
		if !HasErrors(diagnostics) {
			t.Fatalf("expected only wrong assignment after qualified type resolved: %v", diagnostics)
		}
		if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "cannot assign integer to string") {
			t.Fatalf("qualified type or value override did not resolve cleanly: %v", diagnostics)
		}
	}
}
