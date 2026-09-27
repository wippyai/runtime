// SPDX-License-Identifier: MPL-2.0

package contract

import (
	"strings"
	"testing"

	api "github.com/wippyai/runtime/api/contract"
	"github.com/wippyai/runtime/api/registry"
)

func TestBindingSurfaces(t *testing.T) {
	method := func(name, input string) api.MethodDef {
		return api.MethodDef{Name: name, InputSchemas: []api.SchemaDefinition{{Format: "application/schema+json", Definition: `{"type":"` + input + `"}`}}}
	}
	defs := map[string]*api.Definition{
		"s:a": {Methods: []api.MethodDef{method("alpha", "string")}},
		"s:b": {Methods: []api.MethodDef{method("beta", "integer")}},
	}
	bindings := map[string]*api.Binding{
		"s:both":  {Contracts: []api.BoundContract{{Contract: registry.NewID("s", "a")}, {Contract: registry.NewID("s", "b")}}},
		"s:onlyb": {Contracts: []api.BoundContract{{Contract: registry.NewID("s", "b")}}},
	}
	m, diagnostics := BuildTypedCatalogManifest(defs, bindings, nil)
	for _, d := range diagnostics {
		if strings.Contains(d.Message, "conflict") {
			t.Fatalf("unexpected conflict: %+v", d)
		}
	}
	for _, tt := range []struct {
		name, source string
		valid        bool
	}{
		{"default definition", `local c=require("contract"); local d=c.get("s:a"); local i=d:open(); i:alpha("x")`, true},
		{"default excludes incidental", `local c=require("contract"); local d=c.get("s:a"); local i=d:open(); i:beta(1)`, false},
		{"literal merged binding", `local c=require("contract"); local d=c.get("s:a"); local i=d:open("s:both"); i:alpha("x"); i:beta(1)`, true},
		{"direct binding open", `local c=require("contract"); local i, err=c.open("s:both"); if err then return end; i:beta(1)`, true},
		{"unknown binding excludes incidental", `local c=require("contract"); local d=c.get("s:a"); local name: string="s:both"; local i=d:open(name); i:beta(1)`, false},
		{"unrelated binding excludes incidental", `local c=require("contract"); local d=c.get("s:a"); local i=d:open("s:onlyb"); i:beta(1)`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := checkContractSource(t, m, tt.source)
			if (got == "") != tt.valid {
				t.Fatalf("valid=%v diagnostics=%s", tt.valid, got)
			}
		})
	}
}

func TestBindingConflictAndMissingDefinitionAreVisible(t *testing.T) {
	method := func(input string) api.MethodDef {
		return api.MethodDef{Name: "shared", InputSchemas: []api.SchemaDefinition{{Format: "application/schema+json", Definition: `{"type":"` + input + `"}`}}}
	}
	defs := map[string]*api.Definition{
		"s:a": {Methods: []api.MethodDef{method("string")}},
		"s:b": {Methods: []api.MethodDef{method("integer")}},
	}
	bindings := map[string]*api.Binding{"s:conflict": {Contracts: []api.BoundContract{{Contract: registry.NewID("s", "a")}, {Contract: registry.NewID("s", "b")}, {Contract: registry.NewID("s", "missing")}}}}
	m, diagnostics := BuildTypedCatalogManifest(defs, bindings, nil)
	var conflict, missing bool
	for _, d := range diagnostics {
		conflict = conflict || strings.Contains(d.Message, "conflicting method")
		missing = missing || strings.Contains(d.Message, "definition unavailable")
	}
	if !conflict || !missing {
		t.Fatalf("diagnostics: %+v", diagnostics)
	}
	// A conflict never becomes a callable overload chosen arbitrarily.
	got := checkContractSource(t, m, `local c=require("contract"); local i, err=c.open("s:conflict"); if err then return end; i:shared("x")`)
	if got != "" {
		t.Fatalf("conflicted method should stay dynamic with a binding diagnostic: %s", got)
	}
}
