// SPDX-License-Identifier: MPL-2.0

package component

import (
	"testing"

	"github.com/wippyai/runtime/api/registry"
)

func TestBuildImportsEmpty(t *testing.T) {
	imports := BuildImports(nil, nil)
	if len(imports) != 0 {
		t.Errorf("len(imports) = %d, want 0", len(imports))
	}
}

func TestBuildImportsWithImports(t *testing.T) {
	imports := map[string]registry.ID{
		"mylib": {NS: "app", Name: "libs.mylib"},
	}
	result := BuildImports(imports, nil)

	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(result))
	}
	if result[0].Alias != "mylib" {
		t.Errorf("result[0].Alias = %q, want %q", result[0].Alias, "mylib")
	}
	if result[0].ID.Name != "libs.mylib" {
		t.Errorf("result[0].ID.Name = %q, want %q", result[0].ID.Name, "libs.mylib")
	}
}

func TestBuildImportsWithModules(t *testing.T) {
	modules := []string{"json", "base64"}
	result := BuildImports(nil, modules)

	if len(result) != 2 {
		t.Fatalf("len(result) = %d, want 2", len(result))
	}

	found := make(map[string]bool)
	for _, imp := range result {
		found[imp.Alias] = true
		if imp.Alias != imp.ID.Name {
			t.Errorf("for module import, Alias should equal ID.Name: %q != %q", imp.Alias, imp.ID.Name)
		}
	}

	if !found["json"] {
		t.Error("expected json module in imports")
	}
	if !found["base64"] {
		t.Error("expected base64 module in imports")
	}
}

func TestBuildImportsMixed(t *testing.T) {
	imports := map[string]registry.ID{
		"mylib": {NS: "app", Name: "libs.mylib"},
	}
	modules := []string{"json"}
	result := BuildImports(imports, modules)

	if len(result) != 2 {
		t.Fatalf("len(result) = %d, want 2", len(result))
	}
}
