// SPDX-License-Identifier: MPL-2.0

package lint

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/go-lua/compiler/ast"
	"github.com/wippyai/go-lua/types/diag"
)

// --- SeverityString ---

func TestSeverityString(t *testing.T) {
	tests := []struct {
		expected string
		severity Severity
	}{
		{severity: SeverityOff, expected: "off"},
		{severity: SeverityHint, expected: "hint"},
		{severity: SeverityWarning, expected: "warning"},
		{severity: SeverityError, expected: "error"},
		{severity: Severity(99), expected: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, SeverityString(tt.severity))
		})
	}
}

// --- FormatLintCode ---

func TestFormatLintCode(t *testing.T) {
	tests := []struct {
		expected string
		code     diag.Code
	}{
		{code: LintW0001, expected: "W0001"},
		{code: LintW0007, expected: "W0007"},
		{code: LintCodeBase + 42, expected: "W0042"},
		{code: LintCodeBase + 100, expected: "W0100"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, FormatLintCode(tt.code))
		})
	}
}

// --- Config.Merge ---

func TestConfigMerge_NilParent(t *testing.T) {
	child := Config{
		Rules: map[string]RuleConfig{
			"rule-a": {Severity: "error"},
		},
	}
	result := child.Merge(nil)
	assert.Equal(t, "error", result.Rules["rule-a"].Severity)
}

func TestConfigMerge_ChildOverridesParent(t *testing.T) {
	parent := &Config{
		Rules: map[string]RuleConfig{
			"rule-a": {Severity: "warning"},
			"rule-b": {Severity: "hint"},
		},
	}
	child := Config{
		Rules: map[string]RuleConfig{
			"rule-a": {Severity: "error"},
		},
	}
	result := child.Merge(parent)
	assert.Equal(t, "error", result.Rules["rule-a"].Severity)
	assert.Equal(t, "hint", result.Rules["rule-b"].Severity)
}

func TestConfigMerge_ParentRulesPreserved(t *testing.T) {
	parent := &Config{
		Rules: map[string]RuleConfig{
			"parent-only": {Severity: "warning"},
		},
	}
	child := Config{
		Rules: map[string]RuleConfig{
			"child-only": {Severity: "error"},
		},
	}
	result := child.Merge(parent)
	assert.Equal(t, "warning", result.Rules["parent-only"].Severity)
	assert.Equal(t, "error", result.Rules["child-only"].Severity)
}

func TestConfigMerge_EmptyChild(t *testing.T) {
	parent := &Config{
		Rules: map[string]RuleConfig{
			"rule-a": {Severity: "hint"},
		},
	}
	child := Config{Rules: make(map[string]RuleConfig)}
	result := child.Merge(parent)
	assert.Equal(t, "hint", result.Rules["rule-a"].Severity)
}

// --- Config.Apply ---

func TestConfigApply_NilConfig(t *testing.T) {
	var cfg *Config
	reg := NewRegistry()
	cfg.Apply(reg) // should not panic
}

func TestConfigApply_NilRegistry(t *testing.T) {
	cfg := &Config{Rules: map[string]RuleConfig{"x": {Severity: "error"}}}
	cfg.Apply(nil) // should not panic
}

func TestConfigApply_SetsSeverity(t *testing.T) {
	reg := NewRegistry()
	rule := &fakeRule{name: "test-rule", defaultSeverity: SeverityWarning}
	reg.Register(rule)

	cfg := &Config{
		Rules: map[string]RuleConfig{
			"test-rule": {Severity: "error"},
		},
	}
	cfg.Apply(reg)
	assert.Equal(t, SeverityError, reg.GetSeverity("test-rule"))
}

// --- DefaultConfig ---

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	require.NotNil(t, cfg)
	assert.NotNil(t, cfg.Rules)
	assert.Empty(t, cfg.Rules)
}

// --- LoadConfig ---

func TestLoadConfig_Simple(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "lint.json")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`{"rules":{"no-empty-blocks":{"severity":"error"}}}`), 0o644))

	cfg, err := LoadConfig(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "error", cfg.Rules["no-empty-blocks"].Severity)
}

func TestLoadConfig_NonExistentFile(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path.json")
	assert.Error(t, err)
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`not json`), 0o644))

	_, err := LoadConfig(cfgPath)
	assert.Error(t, err)
}

func TestLoadConfig_WithExtends(t *testing.T) {
	dir := t.TempDir()

	parentPath := filepath.Join(dir, "parent.json")
	require.NoError(t, os.WriteFile(parentPath, []byte(`{"rules":{"rule-a":{"severity":"hint"},"rule-b":{"severity":"warning"}}}`), 0o644))

	childPath := filepath.Join(dir, "child.json")
	require.NoError(t, os.WriteFile(childPath, []byte(`{"extends":["parent.json"],"rules":{"rule-a":{"severity":"error"}}}`), 0o644))

	cfg, err := LoadConfig(childPath)
	require.NoError(t, err)
	assert.Equal(t, "error", cfg.Rules["rule-a"].Severity)
	assert.Equal(t, "warning", cfg.Rules["rule-b"].Severity)
}

func TestLoadConfig_ExtendsMissingFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "child.json")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`{"extends":["nonexistent.json"]}`), 0o644))

	_, err := LoadConfig(cfgPath)
	assert.Error(t, err)
}

// --- Registry ---

type fakeRule struct {
	name            string
	category        string
	defaultSeverity Severity
}

func (r *fakeRule) Meta() RuleMeta {
	return RuleMeta{
		Name:            r.name,
		Category:        r.category,
		DefaultSeverity: r.defaultSeverity,
	}
}

func (r *fakeRule) Check(_ *Context) {}

func TestRegistry_RegisterNil(t *testing.T) {
	reg := NewRegistry()
	reg.Register(nil)
	assert.Empty(t, reg.Rules())
}

func TestRegistry_RegisterAndRetrieve(t *testing.T) {
	reg := NewRegistry()
	rule := &fakeRule{name: "test-rule", defaultSeverity: SeverityWarning}
	reg.Register(rule)

	rules := reg.Rules()
	require.Len(t, rules, 1)
	assert.Equal(t, "test-rule", rules[0].Meta().Name)
}

func TestRegistry_RegisterDuplicate(t *testing.T) {
	reg := NewRegistry()
	r1 := &fakeRule{name: "dup", defaultSeverity: SeverityWarning}
	r2 := &fakeRule{name: "dup", defaultSeverity: SeverityError}

	reg.Register(r1)
	reg.Register(r2)

	rules := reg.Rules()
	require.Len(t, rules, 1)
	assert.Equal(t, SeverityError, rules[0].Meta().DefaultSeverity)
}

func TestRegistry_GetSeverity_Default(t *testing.T) {
	reg := NewRegistry()
	rule := &fakeRule{name: "test", defaultSeverity: SeverityHint}
	reg.Register(rule)

	assert.Equal(t, SeverityHint, reg.GetSeverity("test"))
}

func TestRegistry_GetSeverity_Override(t *testing.T) {
	reg := NewRegistry()
	rule := &fakeRule{name: "test", defaultSeverity: SeverityHint}
	reg.Register(rule)
	reg.SetSeverity("test", SeverityError)

	assert.Equal(t, SeverityError, reg.GetSeverity("test"))
}

func TestRegistry_GetSeverity_Unknown(t *testing.T) {
	reg := NewRegistry()
	assert.Equal(t, SeverityOff, reg.GetSeverity("nonexistent"))
}

func TestRegistry_IsEnabled(t *testing.T) {
	reg := NewRegistry()
	rule := &fakeRule{name: "test", defaultSeverity: SeverityWarning}
	reg.Register(rule)

	assert.True(t, reg.IsEnabled("test"))
	reg.SetSeverity("test", SeverityOff)
	assert.False(t, reg.IsEnabled("test"))
}

func TestRegistry_EnabledRules(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&fakeRule{name: "enabled", defaultSeverity: SeverityWarning})
	reg.Register(&fakeRule{name: "disabled", defaultSeverity: SeverityWarning})
	reg.SetSeverity("disabled", SeverityOff)

	enabled := reg.EnabledRules()
	require.Len(t, enabled, 1)
	assert.Equal(t, "enabled", enabled[0].Meta().Name)
}

func TestRegistry_RulesByCategory(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&fakeRule{name: "r1", category: "style"})
	reg.Register(&fakeRule{name: "r2", category: "style"})
	reg.Register(&fakeRule{name: "r3", category: "correctness"})

	byCategory := reg.RulesByCategory()
	assert.Len(t, byCategory["style"], 2)
	assert.Len(t, byCategory["correctness"], 1)
}

func TestRegistry_Categories(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&fakeRule{name: "r1", category: "style"})
	reg.Register(&fakeRule{name: "r2", category: "correctness"})
	reg.Register(&fakeRule{name: "r3", category: "performance"})

	cats := reg.Categories()
	assert.Equal(t, []string{"correctness", "performance", "style"}, cats)
}

func TestRegistry_Clone(t *testing.T) {
	reg := NewRegistry()
	rule := &fakeRule{name: "test", defaultSeverity: SeverityWarning}
	reg.Register(rule)
	reg.SetSeverity("test", SeverityError)

	clone := reg.Clone()
	assert.Equal(t, SeverityError, clone.GetSeverity("test"))

	// Modifying clone does not affect original
	clone.SetSeverity("test", SeverityHint)
	assert.Equal(t, SeverityError, reg.GetSeverity("test"))
	assert.Equal(t, SeverityHint, clone.GetSeverity("test"))
}

func TestRegistry_Clone_PreservesOrder(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&fakeRule{name: "a"})
	reg.Register(&fakeRule{name: "b"})
	reg.Register(&fakeRule{name: "c"})

	clone := reg.Clone()
	rules := clone.Rules()
	require.Len(t, rules, 3)
	assert.Equal(t, "a", rules[0].Meta().Name)
	assert.Equal(t, "b", rules[1].Meta().Name)
	assert.Equal(t, "c", rules[2].Meta().Name)
}

// --- Context.LookupSymbol ---

func TestContext_LookupSymbol_NilGlobalTypes(t *testing.T) {
	ctx := &Context{}
	_, ok := ctx.LookupSymbol("anything")
	assert.False(t, ok)
}

// --- Context.Reportf ---

func TestContext_Reportf_NilCollector(t *testing.T) {
	ctx := &Context{}
	ctx.Reportf(nil, SeverityError, "should not panic") // should not panic
}

// --- NamePos ---

func TestNamePos_InRange(t *testing.T) {
	stmt := &ast.LocalAssignStmt{
		NamePositions: []ast.Position{
			{Line: 5, Column: 10, EndLine: 5, EndColumn: 20},
		},
	}
	pos := NamePos(stmt, 0)
	np, ok := pos.(*namePos)
	require.True(t, ok)
	assert.Equal(t, 5, np.Line())
	assert.Equal(t, 10, np.Column())
	assert.Equal(t, 5, np.LastLine())
	assert.Equal(t, 20, np.LastColumn())
}

func TestNamePos_OutOfRange(t *testing.T) {
	stmt := &ast.LocalAssignStmt{}
	pos := NamePos(stmt, 5)
	assert.Equal(t, stmt, pos)
}

func TestNumForNamePos_HasPosition(t *testing.T) {
	stmt := &ast.NumberForStmt{
		NamePosition: ast.Position{Line: 3, Column: 5, EndLine: 3, EndColumn: 8},
	}
	pos := NumForNamePos(stmt)
	np, ok := pos.(*namePos)
	require.True(t, ok)
	assert.Equal(t, 3, np.Line())
}

func TestNumForNamePos_NoPosition(t *testing.T) {
	stmt := &ast.NumberForStmt{}
	pos := NumForNamePos(stmt)
	assert.Equal(t, stmt, pos)
}

func TestGenForNamePos_InRange(t *testing.T) {
	stmt := &ast.GenericForStmt{
		NamePositions: []ast.Position{
			{Line: 7, Column: 1, EndLine: 7, EndColumn: 5},
		},
	}
	pos := GenForNamePos(stmt, 0)
	np, ok := pos.(*namePos)
	require.True(t, ok)
	assert.Equal(t, 7, np.Line())
}

func TestGenForNamePos_OutOfRange(t *testing.T) {
	stmt := &ast.GenericForStmt{}
	pos := GenForNamePos(stmt, 0)
	assert.Equal(t, stmt, pos)
}
