// SPDX-License-Identifier: MPL-2.0

package contract

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
	api "github.com/wippyai/runtime/api/contract"
	regapi "github.com/wippyai/runtime/api/registry"
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

func TestOpenThroughHelperPreservesErrorCorrelation(t *testing.T) {
	m, _ := sampleContractManifest()
	got := checkContractSource(t, m, `
local contract = require("contract")
local function open_service()
  local def, get_err = contract.get("sample:service")
  if get_err or not def then return nil, get_err or "missing definition" end
  return def:open()
end
local instance, err = open_service()
if err then return end
instance:query({id = "one"})
`)
	if got != "" {
		t.Fatalf("successful helper open should have a present instance: %s", got)
	}
}

func TestValidatedEnumIsAcceptedByTypedContractMethod(t *testing.T) {
	def := &api.Definition{Methods: []api.MethodDef{{
		Name:         "list",
		InputSchemas: []api.SchemaDefinition{{Format: "application/schema+json", Definition: `{"type":"object","properties":{"filters":{"type":"object","properties":{"status":{"type":"string"},"enabled":{"type":"boolean"},"class":{"type":"string"},"task_implementation_id":{"type":"string"},"schedule_type":{"type":"string"}}},"pagination":{"type":"object","properties":{"limit":{"type":"integer"},"offset":{"type":"integer"}}},"ordering":{"type":"object","properties":{"field":{"type":"string","enum":["created_at","updated_at","next_run_at"]},"direction":{"type":"string","enum":["ASC","DESC"]}}}}}`}},
	}}}
	m, _ := BuildTypedManifest(map[string]*api.Definition{"sample:cron": def}, nil)
	got := checkContractSource(t, m, `
local contract = require("contract")
local req = {query = function(self: any, key: string): string? return "created_at" end}
local def = contract.get("sample:cron")
local service, open_err = def:with_actor({}):with_scope({}):open()
if open_err then return end
local order_by = req:query("order_by") or "created_at"
local fields = {"created_at", "updated_at", "next_run_at"}
local valid = false
for _, field in ipairs(fields) do
  if order_by == field then valid = true; break end
end
if not valid then return end
local request = {filters={status=nil, enabled=nil, class=nil, task_implementation_id=nil, schedule_type=nil}, pagination={limit=10,offset=0}, ordering = {field = order_by, direction="ASC"}}
service:list(request)
`)
	if got != "" {
		t.Fatalf("validated enum rejected by contract input: %s", got)
	}
}

func TestTypedContractMethodReceiverAfterInterfaceAssignment(t *testing.T) {
	m, _ := sampleContractManifest()
	got := checkContractSource(t, m, `
local contract = require("contract")
local def = contract.get("sample:service")
local opener: contract.Contract = def
if true then opener = opener:with_actor({}):with_scope({}) end
local instance = opener:open()
instance:query({id = "one"})
`)
	if strings.Contains(got, "method receiver:") {
		t.Fatalf("equivalent contract receiver rejected: %s", got)
	}
}

func TestSameDefinitionProducesCompatibleReceiverTypes(t *testing.T) {
	definition := &api.Definition{Methods: []api.MethodDef{{Name: "query"}}}
	definitions := map[string]*api.Definition{"sample:service": definition}
	a, _ := BuildTypedCatalogManifest(definitions, nil, nil)
	b, _ := BuildTypedCatalogManifest(definitions, map[string]*api.Binding{
		"sample:binding": {Contracts: []api.BoundContract{{Contract: regapi.ParseID("sample:service")}}},
	}, nil)
	name := manifestTypePrefix("sample:service") + "Contract"
	first, _ := a.LookupType(name)
	second, _ := b.LookupType(name)
	joined := io.NewManifest("joined")
	joined.SetExport(typ.NewRecord().Field("opener", typ.NewUnion(first, second)).Build())
	tc := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true, Strict: true}, nil)
	_, diagnostics, err := tc.Check(`local joined = require("joined"); local instance = joined.opener:open()`, "receiver.lua", map[string]*io.Manifest{"joined": joined})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range diagnostics {
		if strings.Contains(d.Message, "method receiver:") {
			t.Fatalf("same-definition receiver rejected: %s", d.Message)
		}
	}
}

func TestOpenSchemaObjectAllowsDynamicNestedContextField(t *testing.T) {
	def := &api.Definition{Methods: []api.MethodDef{{Name: "get_context", OutputSchemas: []api.SchemaDefinition{{Format: "application/schema+json", Definition: `{"type":"object","properties":{"context":{"type":"object"}},"required":["context"]}`}}}}}
	m, _ := BuildTypedManifest(map[string]*api.Definition{"sample:context": def}, nil)
	got := checkContractSource(t, m, `local c = require("contract"); local d = c.get("sample:context"); local i = d:open(); local result = i:get_context({}); local enabled = result.context.nested.enabled`)
	if strings.Contains(got, "cannot index type nil") {
		t.Fatalf("open context lost dynamic field: %s", got)
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
local instance, open_err = def:open()
if open_err then return end
local first, second, err = instance:run()
local value: string = first
local count: number = second
`); diagnostics != "" {
		t.Fatalf("multi-output contract result: %s", diagnostics)
	}
}

func TestContractOpenClusterNamedMethodsAndDynamicBoundary(t *testing.T) {
	names := []string{"append_event", "write", "pull", "upsert", "delete", "grant_subtree", "purge", "resolve"}
	methods := make([]api.MethodDef, len(names))
	for i, name := range names {
		methods[i] = api.MethodDef{Name: name,
			InputSchemas:  []api.SchemaDefinition{{Format: "application/schema+json", Definition: `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`}},
			OutputSchemas: []api.SchemaDefinition{{Format: "application/schema+json", Definition: `{"type":"object","properties":{"success":{"type":"boolean"}},"required":["success"]}`}},
		}
	}
	manifest, gaps := BuildTypedManifest(map[string]*api.Definition{"sample:cluster": {Methods: methods}}, nil)
	if len(gaps) != 0 {
		t.Fatalf("coverage gaps: %+v", gaps)
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			prefix := `local contract = require("contract"); local def = contract.get("sample:cluster"); local inst, open_err = def:open(); if open_err then return end; `
			if got := checkContractSource(t, manifest, prefix+fmt.Sprintf(`local result, err = inst:%s({id="x"}); local ok: boolean = result.success`, name)); got != "" {
				t.Fatalf("named call rejected: %s", got)
			}
			if got := checkContractSource(t, manifest, prefix+fmt.Sprintf(`inst:%s({id=42})`, name)); got == "" {
				t.Fatal("schema-invalid named call was accepted")
			}
		})
	}
	if got := checkContractSource(t, manifest, `local contract = require("contract"); local def = contract.get("sample:cluster"); local inst, open_err = def:open(); if open_err then return end; local function invoke(target: string) inst[target](inst, {id=42}) end`); got != "" {
		t.Fatalf("computed method lookup must stay dynamic: %s", got)
	}
}
