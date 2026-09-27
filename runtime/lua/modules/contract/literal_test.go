// SPDX-License-Identifier: MPL-2.0

package contract

import (
	"testing"

	"github.com/wippyai/go-lua/types/io"
	api "github.com/wippyai/runtime/api/contract"
	"github.com/wippyai/runtime/runtime/lua/code"
)

func TestContractLiteralDispatchAcrossAliasesAndImports(t *testing.T) {
	contractManifest, _ := sampleContractManifest()
	checker := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true, Strict: true}, nil)
	ids, diagnostics, err := checker.Check(`return { SERVICE = "sample:service" }`, "ids", nil)
	if err != nil || code.HasErrors(diagnostics) {
		t.Fatalf("constant module: %v %v", err, diagnostics)
	}
	imports := map[string]*io.Manifest{"contract": contractManifest, "ids": ids}
	for _, tt := range []struct {
		name, source string
		typed        bool
	}{
		{"literal", `local c=require("contract"); local d=c.get("sample:service"); local i=d:open(); i:query({id=42})`, true},
		{"alias", `local c=require("contract"); local id="sample:service"; local d=c.get(id); local i=d:open(); i:query({id=42})`, true},
		{"import", `local c=require("contract"); local ids=require("ids"); local d=c.get(ids.SERVICE); local i=d:open(); i:query({id=42})`, true},
		{"mutated import field", `local c=require("contract"); local ids=require("ids"); ids.SERVICE="other:service"; local d=c.get(ids.SERVICE); local i=d:open(); i:query({id=42})`, false},
		{"reassigned import alias", `local c=require("contract"); local ids=require("ids"); local id=ids.SERVICE; id="other:service"; local d=c.get(id); local i=d:open(); i:query({id=42})`, false},
		{"broad", `local c=require("contract"); local id: string="sample:service"; local d=c.get(id); local i=d:open(); i:query({id=42})`, false},
		{"reassigned", `local c=require("contract"); local id="sample:service"; id="other:service"; local d=c.get(id); local i=d:open(); i:query({id=42})`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, diags, err := checker.Check(tt.source, tt.name, imports)
			if err != nil {
				t.Fatal(err)
			}
			if code.HasErrors(diags) != tt.typed {
				t.Fatalf("typed=%v diagnostics=%v", tt.typed, diags)
			}
		})
	}
}

func TestContractLiteralOverloadsAreOrderIndependent(t *testing.T) {
	defs := map[string]*api.Definition{
		"z:last":  {Methods: []api.MethodDef{{Name: "last", InputSchemas: []api.SchemaDefinition{{Format: "application/schema+json", Definition: `{"type":"string"}`}}}}},
		"a:first": {Methods: []api.MethodDef{{Name: "first", InputSchemas: []api.SchemaDefinition{{Format: "application/schema+json", Definition: `{"type":"integer"}`}}}}},
	}
	manifest, _ := BuildTypedManifest(defs, nil)
	valid := checkContractSource(t, manifest, `local c=require("contract"); local a=c.get("a:first"); local x=a:open(); x:first(1); local z=c.get("z:last"); local y=z:open(); y:last("ok")`)
	if valid != "" {
		t.Fatalf("overload selection: %s", valid)
	}
	invalid := checkContractSource(t, manifest, `local c=require("contract"); local z=c.get("z:last"); local y=z:open(); y:last(1)`)
	if invalid == "" {
		t.Fatal("literal overload must reject the other contract's input")
	}
}
