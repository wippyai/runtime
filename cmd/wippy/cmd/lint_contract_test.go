// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"strings"
	"testing"

	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/service/di"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/code/cache"
	"github.com/wippyai/runtime/runtime/lua/code/lint"
)

func contractEntry(id string, kind regapi.Kind, data string) regapi.Entry {
	return regapi.Entry{ID: regapi.ParseID(id), Kind: kind, Data: payload.NewPayload([]byte(data), payload.JSON)}
}

func TestCatalogManifestTypesOrdinaryLintEntries(t *testing.T) {
	definition := contractEntry("sample:svc", di.Definition, `{"methods":[{"name":"run","input_schemas":[{"format":"application/schema+json","definition":{"type":"string"}}],"output_schemas":[{"format":"application/schema+json","definition":{"type":"boolean"}}]}]}`)
	catalog := collectContractCatalog([]regapi.Entry{definition})
	entry := makeLuaEntry(regapi.NewID("sample", "caller"), map[string]regapi.ID{"contract": regapi.NewID("", "contract")})
	entry.Data = payload.NewPayload(map[string]any{"source": `local contract = require("contract"); local d = contract.get("sample:svc"); local i = d:open(); i:run(42); return {}`, "imports": map[string]regapi.ID{"contract": regapi.NewID("", "contract")}}, payload.Golang)
	checker := code.NewTypeCheckerWithManifests(code.TypeCheckConfig{Enabled: true, Strict: true}, nil, map[string]*io.Manifest{"contract": catalog.manifest})
	linter := lint.New(checker, lint.NewRegistry())
	result := lintEntries([]regapi.Entry{entry}, map[regapi.ID]bool{entry.ID: true}, linter, lintCache{}, lintConfig{minSeverity: severityWarning, workers: 1, imports: newImportResolution([]regapi.Entry{entry}, false)}, nil)
	if result.ErrorCount == 0 {
		t.Fatalf("expected typed input rejection through normal lint path: %+v", result.Diagnostics)
	}
	found := false
	for _, d := range result.Diagnostics {
		if strings.Contains(d.Message, "expected string") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing typed argument diagnostic: %+v", result.Diagnostics)
	}
}

func TestLiteralMissingContractUseSiteWarning(t *testing.T) {
	catalog := collectContractCatalog(nil)
	entry := makeLuaEntry(regapi.NewID("sample", "caller"), map[string]regapi.ID{"contract": regapi.NewID("", "contract")})
	entry.Data = payload.NewPayload(map[string]any{"source": "local contract = require(\"contract\")\nlocal d = contract.get(\"missing:id\")\nreturn {}", "imports": map[string]regapi.ID{"contract": regapi.NewID("", "contract")}}, payload.Golang)
	checker := code.NewTypeCheckerWithManifests(code.TypeCheckConfig{Enabled: true, Strict: true}, nil, map[string]*io.Manifest{"contract": catalog.manifest})
	result := lintEntries([]regapi.Entry{entry}, map[regapi.ID]bool{entry.ID: true}, lint.New(checker, lint.NewRegistry()), lintCache{}, lintConfig{catalog: catalog, minSeverity: severityWarning, workers: 1, imports: newImportResolution([]regapi.Entry{entry}, false)}, nil)
	if result.ErrorCount != 0 {
		t.Fatalf("generic get fallback lost: %+v", result.Diagnostics)
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.EntryID == entry.ID.String() && diagnostic.Severity == "warning" && diagnostic.Line == 2 && strings.Contains(diagnostic.Message, "definition unavailable in this lint catalog") {
			return
		}
	}
	t.Fatalf("missing use-site warning: %+v", result.Diagnostics)
}

func TestContractCatalogReadsAllKindsAndFingerprintsRawConstraints(t *testing.T) {
	definition := contractEntry("sample:svc", di.Definition, `{"methods":[{"name":"run","input_schemas":[{"format":"application/schema+json","definition":{"type":"string","minLength":2}}]}]}`)
	binding := contractEntry("sample:binding", di.Binding, `{"contracts":[{"contract":"sample:svc","methods":{"run":"impl:run"}}]}`)
	other := contractEntry("impl:run", "function.lua", `{"source":"return function() end"}`)
	c := collectContractCatalog([]regapi.Entry{other, binding, definition})
	if c.definitions["sample:svc"] == nil || c.bindings["sample:binding"] == nil || c.manifest == nil {
		t.Fatalf("catalog missing entries: %+v", c)
	}
	if !c.selectedBoundFunction(other.ID, []string{"sample"}) {
		t.Fatal("bound function omitted from selected binding closure")
	}
	if c.selectedBoundFunction(other.ID, []string{"other"}) {
		t.Fatal("unselected binding leaked into closure")
	}
	changedSchema := contractEntry("sample:svc", di.Definition, `{"methods":[{"name":"run","input_schemas":[{"format":"application/schema+json","definition":{"type":"string","minLength":3}}]}]}`)
	schemaCatalog := collectContractCatalog([]regapi.Entry{changedSchema, binding, other})
	if c.fingerprint == schemaCatalog.fingerprint {
		t.Fatal("unsupported raw constraint must change fingerprint")
	}
	changedBinding := contractEntry("sample:binding", di.Binding, `{"contracts":[{"contract":"sample:svc","methods":{"run":"impl:other"}}]}`)
	bindingCatalog := collectContractCatalog([]regapi.Entry{definition, changedBinding, other})
	if c.fingerprint == bindingCatalog.fingerprint {
		t.Fatal("binding mapping must change fingerprint")
	}
	levels := [][]regapi.Entry{{other}}
	data := map[regapi.ID]entryData{other.ID: {Source: "return function() end"}}
	fingerprint := func(catalog *contractCatalog) lintFingerprints {
		return computeLintFingerprints(levels, data, lintCache{catalogHash: catalog.fingerprint, cfg: cache.Config{ToolchainIdentity: "test-toolchain"}})
	}
	baseFP, schemaFP, bindingFP := fingerprint(c), fingerprint(schemaCatalog), fingerprint(bindingCatalog)
	if baseFP.compile[other.ID] != schemaFP.compile[other.ID] || baseFP.compile[other.ID] != bindingFP.compile[other.ID] {
		t.Fatal("catalog-only changes must not invalidate bytecode")
	}
	if baseFP.typecheck[other.ID] == schemaFP.typecheck[other.ID] || baseFP.typecheck[other.ID] == bindingFP.typecheck[other.ID] {
		t.Fatal("catalog-only changes must invalidate typecheck cache")
	}
}

func TestCatalogResourcesResolveReferencesAndInvalidateFingerprint(t *testing.T) {
	definition := contractEntry("sample:svc", di.Definition, `{"methods":[{"name":"run","output_schemas":[{"format":"application/schema+json","definition":{"$ref":"sample:schema"}}]}]}`)
	resource := contractEntry("sample:schema", "contract.schema", `{"type":"string"}`)
	base := collectContractCatalog([]regapi.Entry{definition, resource})
	if len(base.resources) != 1 {
		t.Fatalf("missing catalog resource: %+v", base.resources)
	}
	for _, diagnostic := range base.diagnostics {
		if strings.Contains(diagnostic.Message, "unresolved reference") {
			t.Fatalf("resource was not supplied to translator: %+v", base.diagnostics)
		}
	}
	changed := collectContractCatalog([]regapi.Entry{definition, contractEntry("sample:schema", "contract.schema", `{"type":"number"}`)})
	if changed.fingerprint == base.fingerprint {
		t.Fatal("resource change must invalidate catalog fingerprint")
	}
}

func TestCatalogCoverageSeverity(t *testing.T) {
	c := collectContractCatalog([]regapi.Entry{contractEntry("sample:svc", di.Definition, `{"methods":[{"name":"run"}]}`)})
	warnings := &LintResult{}
	appendCatalogCoverage(warnings, c, false, []string{"sample"}, severityWarning)
	if warnings.WarningCount == 0 || warnings.ErrorCount != 0 {
		t.Fatalf("default coverage: %+v", warnings)
	}
	errors := &LintResult{}
	appendCatalogCoverage(errors, c, true, []string{"sample"}, severityWarning)
	if errors.ErrorCount == 0 {
		t.Fatalf("strict coverage: %+v", errors)
	}
	filtered := &LintResult{}
	appendCatalogCoverage(filtered, c, true, []string{"other"}, severityWarning)
	if len(filtered.Diagnostics) != 0 {
		t.Fatalf("namespace filter leaked: %+v", filtered)
	}
}

func TestBindingFunctionClosureKeepsImportDependenciesButFiltersReports(t *testing.T) {
	root := makeLuaEntry(regapi.NewID("selected", "caller"), nil)
	dependency := makeLuaEntry(regapi.NewID("impl", "helper"), nil)
	bound := makeLuaEntry(regapi.NewID("impl", "run"), map[string]regapi.ID{"helper": dependency.ID})
	binding := contractEntry("selected:binding", di.Binding, `{"contracts":[{"contract":"selected:svc","methods":{"run":"impl:run"}}]}`)
	catalog := collectContractCatalog([]regapi.Entry{root, dependency, bound, binding})
	expanded, report := expandLuaEntriesForCatalog([]regapi.Entry{root, dependency, bound}, []regapi.Entry{root}, catalog, []string{"selected"})
	seen := map[regapi.ID]bool{}
	for _, entry := range expanded {
		seen[entry.ID] = true
	}
	if !seen[root.ID] || !seen[bound.ID] || !seen[dependency.ID] {
		t.Fatalf("closure missing entries: %v", seen)
	}
	if !report[root.ID] || report[bound.ID] || report[dependency.ID] {
		t.Fatalf("reporting filter leaked dependencies: %v", report)
	}
}
