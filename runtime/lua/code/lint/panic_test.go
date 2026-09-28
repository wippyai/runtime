// SPDX-License-Identifier: MPL-2.0

package lint_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/code/lint"
)

type panickingRule struct{}

func (panickingRule) Meta() lint.RuleMeta {
	return lint.RuleMeta{Name: "panicking-rule", DiagCode: 9101, DefaultSeverity: lint.SeverityWarning}
}

func (panickingRule) Check(*lint.Context) { panic("rule exploded") }

type reportingRule struct{}

func (reportingRule) Meta() lint.RuleMeta {
	return lint.RuleMeta{Name: "reporting-rule", DiagCode: 9102, DefaultSeverity: lint.SeverityWarning}
}

func (reportingRule) Check(ctx *lint.Context) {
	ctx.Reportf(ctx.AST[0], lint.SeverityWarning, "reported")
}

// A rule that panics produces a diagnostic naming the rule and the panic, and
// the remaining rules still run.
func TestPanickingRuleIsReported(t *testing.T) {
	registry := lint.NewRegistry()
	registry.Register(panickingRule{})
	registry.Register(reportingRule{})
	result := lint.New(newTestTypeChecker(), registry).Check("local x = 1\nreturn x\n", "test.lua", nil)
	require.NoError(t, result.ParseError)

	var internal, reported bool
	for _, d := range result.Diagnostics {
		if strings.Contains(d.Message, "panicking-rule") && strings.Contains(d.Message, "rule exploded") {
			internal = true
		}
		if d.Message == "reported" {
			reported = true
		}
	}
	require.True(t, internal, "rule panic was not reported: %v", result.Diagnostics)
	require.True(t, reported, "rules after the panicking rule did not run")
}
