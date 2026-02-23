// SPDX-License-Identifier: MPL-2.0

package rules_test

import (
	"strings"
	"testing"

	"github.com/wippyai/go-lua/types/diag"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/code/lint"
	_ "github.com/wippyai/runtime/runtime/lua/code/lint/rules"
)

func newLinter() *lint.Linter {
	tc := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true}, nil)
	return lint.New(tc, lint.DefaultRegistry.Clone())
}

func check(t *testing.T, source string) []diag.Diagnostic {
	t.Helper()
	linter := newLinter()
	result := linter.Check(source, "test.lua", nil)
	if result.ParseError != nil {
		t.Fatalf("parse error: %v", result.ParseError)
	}
	return result.Diagnostics
}

func expectDiagnostic(t *testing.T, diags []diag.Diagnostic, substr string) {
	t.Helper()
	for _, d := range diags {
		if strings.Contains(d.Message, substr) {
			return
		}
	}
	t.Errorf("expected diagnostic containing %q, got: %v", substr, diagMessages(diags))
}

func expectNoDiagnostic(t *testing.T, diags []diag.Diagnostic, substr string) {
	t.Helper()
	for _, d := range diags {
		if strings.Contains(d.Message, substr) {
			t.Errorf("unexpected diagnostic containing %q: %s", substr, d.Message)
		}
	}
}

func expectDiagnosticWithCode(t *testing.T, diags []diag.Diagnostic, substr string, code diag.Code) {
	t.Helper()
	for _, d := range diags {
		if strings.Contains(d.Message, substr) {
			if d.Code != code {
				t.Errorf("diagnostic %q: expected code %d, got %d", substr, code, d.Code)
			}
			return
		}
	}
	t.Errorf("expected diagnostic containing %q, got: %v", substr, diagMessages(diags))
}

func diagMessages(diags []diag.Diagnostic) []string {
	msgs := make([]string, len(diags))
	for i, d := range diags {
		msgs[i] = d.Message
	}
	return msgs
}

// =============================================================================
// no-empty-blocks tests (W0001)
// =============================================================================

func TestNoEmptyBlocks_IfEmpty(t *testing.T) {
	diags := check(t, `
		if true then
		end
	`)
	expectDiagnostic(t, diags, "empty if block")
}

func TestNoEmptyBlocks_IfWithBody(t *testing.T) {
	diags := check(t, `
		if true then
			local x = 1
			return x
		end
	`)
	expectNoDiagnostic(t, diags, "empty if block")
}

func TestNoEmptyBlocks_WhileEmpty(t *testing.T) {
	diags := check(t, `
		while true do
		end
	`)
	expectDiagnostic(t, diags, "empty while block")
}

func TestNoEmptyBlocks_ForEmpty(t *testing.T) {
	diags := check(t, `
		for i = 1, 10 do
		end
	`)
	expectDiagnostic(t, diags, "empty for block")
}

func TestNoEmptyBlocks_GenericForEmpty(t *testing.T) {
	diags := check(t, `
		for k, v in pairs({}) do
		end
	`)
	expectDiagnostic(t, diags, "empty for block")
}

// =============================================================================
// no-global-assign tests (W0002)
// =============================================================================

func TestNoGlobalAssign_SimpleGlobal(t *testing.T) {
	diags := check(t, `
		x = 1
	`)
	expectDiagnostic(t, diags, "assignment to global variable 'x'")
}

func TestNoGlobalAssign_LocalOk(t *testing.T) {
	diags := check(t, `
		local x = 1
		x = 2
		return x
	`)
	expectNoDiagnostic(t, diags, "assignment to global")
}

func TestNoGlobalAssign_FunctionGlobal(t *testing.T) {
	diags := check(t, `
		function foo()
		end
	`)
	expectDiagnostic(t, diags, "function 'foo' declared as global")
}

func TestNoGlobalAssign_LocalFunctionOk(t *testing.T) {
	diags := check(t, `
		local function foo()
			return 1
		end
		return foo
	`)
	expectNoDiagnostic(t, diags, "declared as global")
}

func TestNoGlobalAssign_MethodOk(t *testing.T) {
	diags := check(t, `
		local M = {}
		function M:foo()
			return self
		end
		return M
	`)
	expectNoDiagnostic(t, diags, "declared as global")
}

func TestNoGlobalAssign_NestedScope(t *testing.T) {
	diags := check(t, `
		local function outer()
			local x = 1
			local function inner()
				x = 2
				return x
			end
			return inner
		end
		return outer
	`)
	expectNoDiagnostic(t, diags, "assignment to global")
}

// =============================================================================
// no-self-compare tests (W0003)
// =============================================================================

func TestNoSelfCompare_IdentEqual(t *testing.T) {
	diags := check(t, `
		local x = 1
		if x == x then
			return true
		end
	`)
	expectDiagnostic(t, diags, "comparison of identical expressions")
}

func TestNoSelfCompare_IdentNotEqual(t *testing.T) {
	diags := check(t, `
		local x = 1
		if x ~= x then
			return true
		end
	`)
	expectDiagnostic(t, diags, "comparison of identical expressions")
}

func TestNoSelfCompare_AttrGetEqual(t *testing.T) {
	diags := check(t, `
		local t = {}
		if t.x == t.x then
			return true
		end
	`)
	expectDiagnostic(t, diags, "comparison of identical expressions")
}

func TestNoSelfCompare_DifferentVars(t *testing.T) {
	diags := check(t, `
		local x = 1
		local y = 2
		if x == y then
			return true
		end
	`)
	expectNoDiagnostic(t, diags, "comparison of identical expressions")
}

func TestNoSelfCompare_LessThan(t *testing.T) {
	diags := check(t, `
		local x = 1
		if x < x then
			return true
		end
	`)
	expectDiagnostic(t, diags, "result is always false")
}

func TestNoSelfCompare_LessOrEqual(t *testing.T) {
	diags := check(t, `
		local x = 1
		if x <= x then
			return true
		end
	`)
	expectDiagnostic(t, diags, "result is always true")
}

// =============================================================================
// no-unused-vars tests (W0004)
// =============================================================================

func TestNoUnusedVars_Basic(t *testing.T) {
	diags := check(t, `
		local x = 1
	`)
	expectDiagnostic(t, diags, "variable 'x' is declared but never used")
}

func TestNoUnusedVars_UsedInExpr(t *testing.T) {
	diags := check(t, `
		local x = 1
		return x + 1
	`)
	expectNoDiagnostic(t, diags, "is declared but never used")
}

func TestNoUnusedVars_UsedInCall(t *testing.T) {
	diags := check(t, `
		local x = 1
		print(x)
	`)
	expectNoDiagnostic(t, diags, "variable 'x' is declared but never used")
}

func TestNoUnusedVars_UnderscoreSkip(t *testing.T) {
	diags := check(t, `
		local _x = 1
	`)
	expectNoDiagnostic(t, diags, "is declared but never used")
}

func TestNoUnusedVars_LoopVarUnused(t *testing.T) {
	diags := check(t, `
		local sum = 0
		for i = 1, 10 do
			sum = sum + 1
		end
		return sum
	`)
	expectDiagnostic(t, diags, "loop variable 'i' is declared but never used")
}

func TestNoUnusedVars_LoopVarUsed(t *testing.T) {
	diags := check(t, `
		local sum = 0
		for i = 1, 10 do
			sum = sum + i
		end
		return sum
	`)
	expectNoDiagnostic(t, diags, "loop variable 'i'")
}

func TestNoUnusedVars_GenericForVarUnused(t *testing.T) {
	diags := check(t, `
		local t = {1, 2, 3}
		local sum = 0
		for k, v in pairs(t) do
			sum = sum + v
		end
		return sum
	`)
	expectDiagnostic(t, diags, "loop variable 'k' is declared but never used")
}

func TestNoUnusedVars_UsedAsTableKey(t *testing.T) {
	diags := check(t, `
		local found = {}
		local t = {"a", "b", "c"}
		for _, name in ipairs(t) do
			found[name] = true
		end
		return found
	`)
	expectNoDiagnostic(t, diags, "variable 'name' is declared but never used")
	expectNoDiagnostic(t, diags, "loop variable 'name' is declared but never used")
}

func TestNoUnusedVars_UsedInCastExpr(t *testing.T) {
	diags := check(t, `
		local x = 1
		return x :: any
	`)
	expectNoDiagnostic(t, diags, "variable 'x' is declared but never used")
}

func TestNoUnusedVars_UsedInMethodCall(t *testing.T) {
	diags := check(t, `
		local msg = {}
		local val = msg:payload() :: string
		return val
	`)
	expectNoDiagnostic(t, diags, "variable 'msg' is declared but never used")
}

func TestNoUnusedVars_UsedAsTableKeyInAssign(t *testing.T) {
	diags := check(t, `
		local pending = {}
		local pid = "abc"
		pending[pid] = { data = 1 }
		return pending
	`)
	expectNoDiagnostic(t, diags, "variable 'pid' is declared but never used")
}

// =============================================================================
// no-unused-params tests (W0005)
// =============================================================================

func TestNoUnusedParams_Unused(t *testing.T) {
	diags := check(t, `
		local function foo(x)
			return 1
		end
		return foo
	`)
	expectDiagnostic(t, diags, "parameter 'x' is unused")
}

func TestNoUnusedParams_Used(t *testing.T) {
	diags := check(t, `
		local function foo(x)
			return x
		end
		return foo
	`)
	expectNoDiagnostic(t, diags, "parameter 'x' is unused")
}

func TestNoUnusedParams_SelfSkip(t *testing.T) {
	diags := check(t, `
		local M = {}
		function M:foo()
			return 1
		end
		return M
	`)
	expectNoDiagnostic(t, diags, "parameter 'self' is unused")
}

func TestNoUnusedParams_UnderscoreSkip(t *testing.T) {
	diags := check(t, `
		local function foo(_x)
			return 1
		end
		return foo
	`)
	expectNoDiagnostic(t, diags, "is unused")
}

// =============================================================================
// no-unused-imports tests (W0006)
// =============================================================================

func TestNoUnusedImports_Unused(t *testing.T) {
	diags := check(t, `
		local json = require("json")
		return 1
	`)
	expectDiagnostic(t, diags, "imported module 'json' is never used")
}

func TestNoUnusedImports_Used(t *testing.T) {
	diags := check(t, `
		local json = require("json")
		return json.encode({})
	`)
	expectNoDiagnostic(t, diags, "is never used")
}

func TestNoUnusedImports_NotRequire(t *testing.T) {
	diags := check(t, `
		local x = tonumber("1")
	`)
	expectNoDiagnostic(t, diags, "imported module")
}

// =============================================================================
// no-shadowed-vars tests (W0007)
// =============================================================================

func TestNoShadowedVars_InBlock(t *testing.T) {
	diags := check(t, `
		local x = 1
		do
			local x = 2
			print(x)
		end
		return x
	`)
	expectDiagnostic(t, diags, "variable 'x' shadows")
}

func TestNoShadowedVars_InFunction(t *testing.T) {
	diags := check(t, `
		local x = 1
		local function foo()
			local x = 2
			return x
		end
		return foo() + x
	`)
	expectDiagnostic(t, diags, "variable 'x' shadows")
}

func TestNoShadowedVars_NoShadow(t *testing.T) {
	diags := check(t, `
		local x = 1
		local y = 2
		return x + y
	`)
	expectNoDiagnostic(t, diags, "shadows")
}

func TestNoShadowedVars_UnderscoreSkip(t *testing.T) {
	diags := check(t, `
		local _x = 1
		do
			local _x = 2
			print(_x)
		end
		return _x
	`)
	expectNoDiagnostic(t, diags, "shadows")
}

// =============================================================================
// Diagnostic code tests
// =============================================================================

func TestDiagCode_EmptyBlock(t *testing.T) {
	diags := check(t, `
		if true then
		end
	`)
	expectDiagnosticWithCode(t, diags, "empty if block", lint.LintW0001)
}

func TestDiagCode_GlobalAssign(t *testing.T) {
	diags := check(t, `
		x = 1
	`)
	expectDiagnosticWithCode(t, diags, "assignment to global", lint.LintW0002)
}

func TestDiagCode_SelfCompare(t *testing.T) {
	diags := check(t, `
		local x = 1
		if x == x then
			return x
		end
	`)
	expectDiagnosticWithCode(t, diags, "comparison of identical", lint.LintW0003)
}

func TestDiagCode_FormatLintCode(t *testing.T) {
	tests := []struct {
		want string
		code diag.Code
	}{
		{"W0001", lint.LintW0001},
		{"W0002", lint.LintW0002},
		{"W0003", lint.LintW0003},
		{"W0004", lint.LintW0004},
		{"W0005", lint.LintW0005},
		{"W0006", lint.LintW0006},
		{"W0007", lint.LintW0007},
	}
	for _, tt := range tests {
		got := lint.FormatLintCode(tt.code)
		if got != tt.want {
			t.Errorf("FormatLintCode(%d) = %q, want %q", tt.code, got, tt.want)
		}
	}
}

// =============================================================================
// Severity levels tests
// =============================================================================

func TestSeverityLevels_Error(t *testing.T) {
	diags := check(t, `
		local x = 1
		if x == x then
			return x
		end
	`)
	for _, d := range diags {
		if strings.Contains(d.Message, "comparison of identical") {
			if d.Severity != diag.SeverityError {
				t.Errorf("no-self-compare should be Error severity, got %v", d.Severity)
			}
			return
		}
	}
	t.Error("expected self-compare diagnostic")
}

func TestSeverityLevels_Warning(t *testing.T) {
	diags := check(t, `
		x = 1
	`)
	for _, d := range diags {
		if strings.Contains(d.Message, "assignment to global") {
			if d.Severity != diag.SeverityWarning {
				t.Errorf("no-global-assign should be Warning severity, got %v", d.Severity)
			}
			return
		}
	}
	t.Error("expected global assign diagnostic")
}

// =============================================================================
// Rule disable tests
// =============================================================================

func TestRuleDisable(t *testing.T) {
	tc := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true}, nil)
	registry := lint.DefaultRegistry.Clone()
	registry.SetSeverity("no-empty-blocks", lint.SeverityOff)
	linter := lint.New(tc, registry)

	result := linter.Check(`
		if true then
		end
	`, "test.lua", nil)

	for _, d := range result.Diagnostics {
		if strings.Contains(d.Message, "empty if block") {
			t.Error("rule should be disabled")
		}
	}
}

func TestRuleSeverityOverride(t *testing.T) {
	tc := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true}, nil)
	registry := lint.DefaultRegistry.Clone()

	if registry.GetSeverity("no-empty-blocks") != lint.SeverityWarning {
		t.Error("default severity should be Warning")
	}

	registry.SetSeverity("no-empty-blocks", lint.SeverityError)

	if registry.GetSeverity("no-empty-blocks") != lint.SeverityError {
		t.Error("severity should be overridden to Error")
	}

	linter := lint.New(tc, registry)
	result := linter.Check(`
		if true then
		end
	`, "test.lua", nil)

	found := false
	for _, d := range result.Diagnostics {
		if strings.Contains(d.Message, "empty if block") {
			found = true
		}
	}
	if !found {
		t.Error("expected empty block diagnostic")
	}
}
