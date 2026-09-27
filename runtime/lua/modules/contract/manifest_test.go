// SPDX-License-Identifier: MPL-2.0

package contract

import (
	"strings"
	"testing"

	"github.com/wippyai/go-lua/types/io"
	api "github.com/wippyai/runtime/api/contract"
	"github.com/wippyai/runtime/runtime/lua/code"
)

func sampleContractManifest() (*io.Manifest, []ManifestDiagnostic) {
	definition := &api.Definition{Methods: []api.MethodDef{{
		Name:          "query",
		InputSchemas:  []api.SchemaDefinition{{Format: "application/schema+json", Definition: `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`}},
		OutputSchemas: []api.SchemaDefinition{{Format: "application/schema+json", Definition: `{"type":["object","null"],"properties":{"ok":{"type":"boolean"}},"required":["ok"]}`}},
	}}}
	return BuildTypedManifest(map[string]*api.Definition{"sample:service": definition}, nil)
}

func checkContractSource(t *testing.T, manifest *io.Manifest, source string) string {
	t.Helper()
	tc := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true, Strict: true}, nil)
	_, diagnostics, err := tc.Check(source, "contract_fixture.lua", map[string]*io.Manifest{"contract": manifest})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, d := range diagnostics {
		out = append(out, d.Message)
	}
	return strings.Join(out, "\n")
}

func TestTypedContractManifestCheckerBehavior(t *testing.T) {
	m, gaps := sampleContractManifest()
	if len(gaps) != 0 {
		t.Fatalf("unexpected coverage gaps: %+v", gaps)
	}
	valid := checkContractSource(t, m, `
local contract = require("contract")
local def, get_err = contract.get("sample:service")
if get_err then return end
local instance, open_err = def:with_actor({}):with_scope({}):with_context({}):with_options({}):open()
if open_err then return end
local value, call_err = instance:query({ id = "one" })
if call_err then return end
if value then
  local ok: boolean = value.ok
end
`)
	if valid != "" {
		t.Fatalf("valid typed call: %s", valid)
	}
	for _, tt := range []struct{ name, source string }{
		{"wrong input", `local c = require("contract"); local d = c.get("sample:service"); local i = d:open(); i:query({id = 42})`},
		{"missing required input", `local c = require("contract"); local d = c.get("sample:service"); local i = d:open(); i:query({})`},
		{"unknown method", `local c = require("contract"); local d = c.get("sample:service"); local i = d:open(); i:missing({id = "x"})`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := checkContractSource(t, m, tt.source); got == "" {
				t.Fatal("expected checker diagnostic")
			}
		})
	}
	dynamic := checkContractSource(t, m, `
local c = require("contract")
local id: string = "sample:service"
local d = c.get(id)
local i = d:open()
i:query({ id = 42 })
`)
	if dynamic != "" {
		t.Fatalf("dynamic fallback must remain compatible: %s", dynamic)
	}
}

func TestManifestMultipleOutputsAndUnspecifiedInputs(t *testing.T) {
	def := &api.Definition{Methods: []api.MethodDef{{Name: "run", OutputSchemas: []api.SchemaDefinition{
		{Format: "application/schema+json", Definition: `{"type":"string"}`},
		{Format: "application/schema+json", Definition: `{"type":"number"}`},
	}}}}
	manifest, gaps := BuildTypedManifest(map[string]*api.Definition{"sample:ambiguous": def}, nil)
	if len(gaps) != 1 || !strings.Contains(gaps[0].Message, "unspecified") {
		t.Fatalf("gaps = %+v", gaps)
	}
	if diagnostics := checkContractSource(t, manifest, `
local contract = require("contract")
local def = contract.get("sample:ambiguous")
local instance = def:open()
local first, second, err = instance:run()
local value: string = first
local count: number = second
`); diagnostics != "" {
		t.Fatalf("multi-output contract result: %s", diagnostics)
	}
}
