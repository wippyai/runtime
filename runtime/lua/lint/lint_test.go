package lint_test

import (
	"testing"

	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/lint"
	_ "github.com/wippyai/runtime/runtime/lua/lint/rules"
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
