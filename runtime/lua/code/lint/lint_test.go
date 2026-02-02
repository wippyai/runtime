package lint_test

import (
	"testing"

	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/code/lint"
	_ "github.com/wippyai/runtime/runtime/lua/code/lint/rules"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

func newTestTypeChecker() *code.TypeChecker {
	return code.NewTypeChecker(code.TypeCheckConfig{Enabled: true}, nil)
}

func TestLinterBasic(t *testing.T) {
	tc := newTestTypeChecker()
	linter := lint.New(tc, nil)

	result := linter.Check(`
		local x = 1
		return x
	`, "test.lua", nil)

	if result.ParseError != nil {
		t.Fatalf("unexpected parse error: %v", result.ParseError)
	}
}

func TestLinterEmptyBlock(t *testing.T) {
	tc := newTestTypeChecker()
	linter := lint.New(tc, nil)

	result := linter.Check(`
		if true then
		end
	`, "test.lua", nil)

	if result.ParseError != nil {
		t.Fatalf("unexpected parse error: %v", result.ParseError)
	}

	found := false
	for _, d := range result.Diagnostics {
		if d.Message == "empty if block" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'empty if block' warning")
	}
}

func TestLinterGlobalAssign(t *testing.T) {
	tc := newTestTypeChecker()
	linter := lint.New(tc, nil)

	result := linter.Check(`
		x = 1
	`, "test.lua", nil)

	if result.ParseError != nil {
		t.Fatalf("unexpected parse error: %v", result.ParseError)
	}

	found := false
	for _, d := range result.Diagnostics {
		if d.Message == "assignment to global variable 'x'; consider using 'local'" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected global assignment warning, got: %v", result.Diagnostics)
	}
}

func TestProcessIntersectionWithUnknown(t *testing.T) {
	// Test that reproduces the "expected function, got unknown & fun()" bug
	tc := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true, Strict: true}, nil)

	// Add process module with intersection of Unknown
	// (simulating a module loading bug)
	processOptionsType := typ.NewRecord().
		Field("trap_links", typ.Boolean).
		Build()

	moduleMethodsType := typ.NewInterface("process", []typ.Method{
		{Name: "get_options", Type: typ.Func().Returns(processOptionsType).Build()},
	})

	intersectionWithUnknown := typ.NewIntersection(typ.Unknown, moduleMethodsType)

	processManifest := io.NewManifest("process")
	processManifest.SetExport(intersectionWithUnknown)

	tc.AddBuiltinManifest("process", processManifest)

	linter := lint.New(tc, nil)

	result := linter.Check(`
local function main()
    local opts = process.get_options()
    return opts
end
`, "test.lua", nil)

	if result.ParseError != nil {
		t.Fatalf("unexpected parse error: %v", result.ParseError)
	}

	for _, d := range result.Diagnostics {
		t.Logf("diagnostic [%v]: %s", d.Severity, d.Message)
	}

	// Check for the specific error message we saw
	for _, d := range result.Diagnostics {
		if d.Message == "expected function, got unknown & fun() -> {trap_links: boolean}" {
			t.Errorf("BUG REPRODUCED: %s", d.Message)
		}
	}
}

func TestMultiEntryLint(t *testing.T) {
	// Test checking multiple entries with shared TypeChecker
	// (simulating the actual lint command flow)
	tc := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true, Strict: true}, nil)

	// Add process module with CORRECT intersection (no Unknown)
	processOptionsType := typ.NewRecord().
		Field("trap_links", typ.Boolean).
		Build()

	moduleMethodsType := typ.NewInterface("process", []typ.Method{
		{Name: "get_options", Type: typ.Func().Returns(processOptionsType).Build()},
		{Name: "pid", Type: typ.Func().Returns(typ.String).Build()},
	})

	moduleFieldsType := typ.NewRecord().
		Field("event", typ.NewRecord().Build()).
		Build()

	processManifest := io.NewManifest("process")
	processManifest.SetExport(typ.NewIntersection(moduleMethodsType, moduleFieldsType))

	tc.AddBuiltinManifest("process", processManifest)

	linter := lint.New(tc, nil)

	// First entry
	result1 := linter.Check(`
local function main()
    local pid = process.pid()
    return pid
end
`, "entry1.lua", nil)

	t.Logf("Entry 1 diagnostics: %d", len(result1.Diagnostics))
	for _, d := range result1.Diagnostics {
		t.Logf("  [%v]: %s", d.Severity, d.Message)
	}

	// Second entry - reusing same linter
	result2 := linter.Check(`
local function main()
    local opts = process.get_options()
    return opts
end
`, "entry2.lua", nil)

	t.Logf("Entry 2 diagnostics: %d", len(result2.Diagnostics))
	for _, d := range result2.Diagnostics {
		t.Logf("  [%v]: %s", d.Severity, d.Message)
	}

	// Third entry with import
	result3 := linter.Check(`
local function main()
    local opts = process.get_options()
    local pid = process.pid()
    return opts, pid
end
`, "entry3.lua", map[string]*io.Manifest{
		"entry1": result1.Manifest,
		"entry2": result2.Manifest,
	})

	t.Logf("Entry 3 diagnostics: %d", len(result3.Diagnostics))
	for _, d := range result3.Diagnostics {
		t.Logf("  [%v]: %s", d.Severity, d.Message)
	}

	// Check for the specific error
	for _, d := range result3.Diagnostics {
		if d.Message == "expected function, got unknown & fun() -> {trap_links: boolean}" {
			t.Errorf("BUG REPRODUCED in entry3: %s", d.Message)
		}
	}
}

func TestRegistryDisableRule(t *testing.T) {
	registry := lint.DefaultRegistry.Clone()
	registry.SetSeverity("no-empty-blocks", lint.SeverityOff)

	tc := newTestTypeChecker()
	linter := lint.New(tc, registry)

	result := linter.Check(`
		if true then
		end
	`, "test.lua", nil)

	if result.ParseError != nil {
		t.Fatalf("unexpected parse error: %v", result.ParseError)
	}

	for _, d := range result.Diagnostics {
		if d.Message == "empty if block" {
			t.Error("rule should be disabled")
		}
	}
}

func TestConfigParseSeverity(t *testing.T) {
	tests := []struct {
		input    string
		expected lint.Severity
	}{
		{"off", lint.SeverityOff},
		{"0", lint.SeverityOff},
		{"hint", lint.SeverityHint},
		{"1", lint.SeverityHint},
		{"warning", lint.SeverityWarning},
		{"warn", lint.SeverityWarning},
		{"2", lint.SeverityWarning},
		{"error", lint.SeverityError},
		{"err", lint.SeverityError},
		{"3", lint.SeverityError},
		{"unknown", lint.SeverityWarning}, // default
	}

	for _, tt := range tests {
		got := lint.ParseSeverity(tt.input)
		if got != tt.expected {
			t.Errorf("ParseSeverity(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}
