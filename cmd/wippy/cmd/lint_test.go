// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/wippyai/go-lua/compiler/ast"
	"github.com/wippyai/go-lua/compiler/parse"
	"github.com/wippyai/go-lua/types/diag"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/code/lint"
	"github.com/wippyai/runtime/runtime/lua/component"
	"github.com/wippyai/runtime/runtime/lua/engine"
	transcoder "github.com/wippyai/runtime/system/payload"
	payloadjson "github.com/wippyai/runtime/system/payload/json"
)

// ambientRequireBuiltins assembles the require allowlist the same way
// createLinter does, so these tests track the runtime ambient set.
func ambientRequireBuiltins() []string {
	return append(engine.AmbientBaseModuleNames(), component.ExecutableAmbientModuleNames()...)
}

func makeLuaEntry(id registry.ID, imports map[string]registry.ID) registry.Entry {
	return makeLuaSourceEntry(id, imports, "return {}")
}

func makeLuaSourceEntry(id registry.ID, imports map[string]registry.ID, source string) registry.Entry {
	cfg := struct {
		Imports map[string]registry.ID `json:"imports,omitempty"`
		Source  string                 `json:"source"`
	}{
		Source:  source,
		Imports: imports,
	}
	payloadjson.Register(transcoder.GlobalTranscoder())
	raw, err := json.Marshal(cfg)
	if err != nil {
		panic(err)
	}
	return registry.Entry{
		ID:   id,
		Kind: registry.Kind("library.lua"),
		Data: payload.NewPayload(raw, payload.JSON),
	}
}

func TestLintBuiltinInventoryIncludesUntypedModules(t *testing.T) {
	typed := &luaapi.ModuleDef{Name: "typed", Types: func() *io.Manifest { return io.NewManifest("typed") }}
	untyped := &luaapi.ModuleDef{Name: "untyped"}
	names, manifests := lintBuiltinInventory([]*luaapi.ModuleDef{typed, untyped})
	if len(names) != 2 || names[0] != "typed" || names[1] != "untyped" {
		t.Fatalf("unexpected builtin inventory: %v", names)
	}
	if manifests["typed"] == nil {
		t.Fatal("typed module manifest missing")
	}
	if _, ok := manifests["untyped"]; ok {
		t.Fatal("untyped module must not fabricate a manifest")
	}
}

func TestExtractEntryDataUsesRuntimeLibraryMethod(t *testing.T) {
	payloadjson.Register(transcoder.GlobalTranscoder())
	raw, err := json.Marshal(map[string]any{"source": "return {}", "method": "ignored"})
	if err != nil {
		t.Fatal(err)
	}
	entry := registry.Entry{
		ID:   registry.NewID("app", "library"),
		Kind: luaapi.Library,
		Data: payload.NewPayload(raw, payload.JSON),
	}
	if method := extractEntryData(entry).Method; method != "" {
		t.Fatalf("library method = %q, want empty runtime method", method)
	}
}

func TestExpandLuaEntriesByImports_IncludesDeps(t *testing.T) {
	depID := registry.NewID("ns.dep", "dep")
	rootID := registry.NewID("ns.root", "root")
	dep := makeLuaEntry(depID, nil)
	root := makeLuaEntry(rootID, map[string]registry.ID{"dep": depID})

	all := []registry.Entry{root, dep}
	selected := []registry.Entry{root}

	expanded, reportSet := expandLuaEntriesByImports(all, selected)
	if !reportSet[rootID] {
		t.Fatalf("expected reportSet to include root")
	}
	if reportSet[depID] {
		t.Fatalf("did not expect reportSet to include dependency")
	}
	seen := map[registry.ID]bool{}
	for _, e := range expanded {
		seen[e.ID] = true
	}
	if !seen[rootID] || !seen[depID] {
		t.Fatalf("expected expanded entries to include root and dep; got %v", seen)
	}
}

func runRequireDeclarations(t *testing.T, source string, imports map[string]registry.ID, builtins []string) []string {
	t.Helper()
	stmts, err := parse.ParseString(source, "ns.test:entry")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if imports == nil {
		imports = map[string]registry.ID{}
	}
	builtinSet := make(map[string]struct{}, len(builtins))
	for _, b := range builtins {
		builtinSet[b] = struct{}{}
	}
	diags := lintRequireDeclarations(stmts, "ns.test:entry", entryData{Imports: imports}, builtinSet)
	msgs := make([]string, 0, len(diags))
	for _, d := range diags {
		msgs = append(msgs, d.Message)
	}
	return msgs
}

func TestLintRequireDeclarations_DeclaredImportIsClean(t *testing.T) {
	msgs := runRequireDeclarations(t,
		`local dep = require("dep"); return dep`,
		map[string]registry.ID{"dep": registry.NewID("ns.dep", "dep")}, nil)
	if len(msgs) != 0 {
		t.Fatalf("expected no diagnostics for declared import, got %v", msgs)
	}
}

func TestLintRequireDeclarations_BuiltinModuleIsClean(t *testing.T) {
	msgs := runRequireDeclarations(t,
		`local j = require("json"); return j`,
		nil, []string{"json"})
	if len(msgs) != 0 {
		t.Fatalf("expected no diagnostics for builtin module, got %v", msgs)
	}
}

func TestLintRequireDeclarations_UndeclaredModuleReported(t *testing.T) {
	msgs := runRequireDeclarations(t,
		`local x = require("mystery"); return x`,
		nil, nil)
	if len(msgs) != 1 {
		t.Fatalf("expected exactly one diagnostic, got %v", msgs)
	}
	if !strings.Contains(msgs[0], `require("mystery")`) {
		t.Fatalf("diagnostic should name the offending module, got %q", msgs[0])
	}
}

func TestLintRequireDeclarations_DynamicRequireIgnored(t *testing.T) {
	msgs := runRequireDeclarations(t,
		`local name = "dep"; local x = require(name); return x`,
		nil, nil)
	if len(msgs) != 0 {
		t.Fatalf("dynamic require(var) must not be flagged statically, got %v", msgs)
	}
}

func TestLintRequireDeclarations_DetectedInsideFunctionBody(t *testing.T) {
	msgs := runRequireDeclarations(t,
		`return function() return require("mystery").run() end`,
		nil, nil)
	if len(msgs) != 1 {
		t.Fatalf("expected require nested in a function body to be detected, got %v", msgs)
	}
}

func TestLintRequireDeclarations_AmbientModulesNeedNoDeclaration(t *testing.T) {
	builtins := ambientRequireBuiltins()
	if len(builtins) == 0 {
		t.Fatal("ambient require builtins are empty")
	}
	for _, name := range builtins {
		msgs := runRequireDeclarations(t, `local m = require("`+name+`"); return m`, nil, builtins)
		if len(msgs) != 0 {
			t.Fatalf("ambient module %q must not require a declaration, got %v", name, msgs)
		}
	}
}

func TestLintRequireDeclarations_NonAmbientRegisteredModuleMustBeDeclared(t *testing.T) {
	builtins := ambientRequireBuiltins()
	for _, name := range builtins {
		if name == "json" {
			t.Fatalf("test assumes json is not ambient, but it is in %v", builtins)
		}
	}

	// json is a registered module with type info but is not ambient at runtime;
	// an undeclared require must be flagged at lint time.
	flagged := runRequireDeclarations(t, `local j = require("json"); return j`, nil, builtins)
	if len(flagged) != 1 {
		t.Fatalf("undeclared require(\"json\") must be flagged, got %v", flagged)
	}

	// Declaring it clears the diagnostic.
	clean := runRequireDeclarations(t, `local j = require("json"); return j`,
		map[string]registry.ID{"json": registry.NewID("wippy.json", "json")}, builtins)
	if len(clean) != 0 {
		t.Fatalf("declared json import must clear the diagnostic, got %v", clean)
	}
}

func TestLintOneEntryRendersParseErrors(t *testing.T) {
	typeChecker := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true, Strict: true}, nil)
	linter := lint.New(typeChecker, lint.NewRegistry())
	entry := registry.Entry{ID: registry.NewID("app", "broken"), Kind: luaapi.Library}
	data := entryData{Source: "local M = {}\nlocal interface = 1\nreturn M\n"}
	result := lintOneEntry(entry, data, linter, map[registry.ID]*io.Manifest{}, severityWarning, lintCache{}, lintFingerprints{})
	if result == nil || result.errors != 1 || len(result.diagnostics) != 1 {
		t.Fatalf("parse failure must produce exactly one error, got %+v", result)
	}
	if result.diagnostics[0].Line != 2 {
		t.Fatalf("parse error line = %d, want 2", result.diagnostics[0].Line)
	}
	if result.diagnostics[0].Code != "P0001" {
		t.Fatalf("parse error code = %q, want P0001", result.diagnostics[0].Code)
	}
	if len(result.rich) != 1 {
		t.Fatalf("parse error must be rendered like every other diagnostic, rich = %d", len(result.rich))
	}
	rich := result.rich[0]
	if rich.Diag.Severity != diag.SeverityError || rich.Diag.Position.Line != 2 || rich.Source == nil {
		t.Fatalf("rendered parse error lacks position or source: %+v", rich.Diag)
	}
	rendered := renderRichDiag(rich, true)
	hasCode := strings.Contains(rendered, "error[P0001]")
	hasLocation := strings.Contains(rendered, "app:broken:2:")
	hasSource := strings.Contains(rendered, "interface")
	if !hasCode || !hasLocation || !hasSource {
		t.Fatalf("rendered parse error must show its code, location, and source, got %q", rendered)
	}

	lintResult := &LintResult{
		Diagnostics:     result.diagnostics,
		RichDiagnostics: result.rich,
		TotalEntries:    1,
		ErrorCount:      1,
	}
	filtered := filterByCode(lintResult, []string{"P0001"})
	if len(filtered.Diagnostics) != 1 || len(filtered.RichDiagnostics) != 1 || filtered.ErrorCount != 1 {
		t.Fatalf("P0001 filter dropped parse error: %+v", filtered)
	}
	if got := filterByCode(lintResult, []string{"E0000"}); len(got.Diagnostics) != 0 || len(got.RichDiagnostics) != 0 {
		t.Fatalf("parse error leaked through E0000 filter: %+v", got)
	}
}

func TestParseErrorResultUsesSafeFallbackPositions(t *testing.T) {
	tests := []struct {
		err         error
		name        string
		wantMessage string
		source      diag.SourceLines
		wantLine    int
		wantColumn  int
	}{
		{
			name:        "EOF uses last source line",
			err:         &parse.Error{Pos: ast.Position{Line: parse.EOF}, Message: "unexpected EOF"},
			source:      diag.ParseSource("local function broken()\nreturn 1"),
			wantLine:    2,
			wantColumn:  1,
			wantMessage: "unexpected EOF",
		},
		{
			name:        "missing parser column stays one based",
			err:         &parse.Error{Pos: ast.Position{Line: 2}, Message: "unexpected token"},
			source:      diag.ParseSource("first\nsecond"),
			wantLine:    2,
			wantColumn:  1,
			wantMessage: "unexpected token",
		},
		{
			name:        "opaque error uses origin",
			err:         errors.New("parser failed"),
			source:      nil,
			wantLine:    1,
			wantColumn:  1,
			wantMessage: "parser failed",
		},
	}

	id := registry.NewID("app", "broken")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseErrorResult(id, tt.err, tt.source)
			if len(result.diagnostics) != 1 || len(result.rich) != 1 || result.errors != 1 {
				t.Fatalf("parse error result shape = %+v", result)
			}
			got := result.diagnostics[0]
			if got.Code != parseErrorCode || got.Message != tt.wantMessage ||
				got.Line != tt.wantLine || got.Column != tt.wantColumn {
				t.Fatalf("parse error diagnostic = %+v, want %s at %d:%d", got, tt.wantMessage, tt.wantLine, tt.wantColumn)
			}
			if richDiagnosticCode(result.rich[0]) != parseErrorCode {
				t.Fatalf("rich parse code = %q, want %s", richDiagnosticCode(result.rich[0]), parseErrorCode)
			}
		})
	}
}
