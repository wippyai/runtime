// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wippyai/go-lua/compiler/parse"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
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
	cfg := struct {
		Imports map[string]registry.ID `json:"imports,omitempty"`
		Source  string                 `json:"source"`
	}{
		Source:  "return {}",
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
