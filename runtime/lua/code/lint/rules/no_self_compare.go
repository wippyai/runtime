// SPDX-License-Identifier: MPL-2.0

package rules

import (
	"github.com/wippyai/go-lua/compiler/ast"
	"github.com/wippyai/runtime/runtime/lua/code/lint"
)

func init() {
	lint.Register(&NoSelfCompare{})
}

// NoSelfCompare detects comparisons of a value with itself (x == x, x ~= x).
type NoSelfCompare struct{}

func (r *NoSelfCompare) Meta() lint.RuleMeta {
	return lint.RuleMeta{
		Name:            "no-self-compare",
		Code:            "W0003",
		DiagCode:        lint.LintW0003,
		Title:           "self comparison",
		Description:     "Comparing a value with itself is always true/false and likely a bug",
		DefaultSeverity: lint.SeverityError,
		Category:        "correctness",
	}
}

func (r *NoSelfCompare) Check(_ *lint.Context) {}

func (r *NoSelfCompare) VisitStmt(_ *lint.Context, _ ast.Stmt) {}

func (r *NoSelfCompare) VisitExpr(ctx *lint.Context, expr ast.Expr) {
	e, ok := expr.(*ast.RelationalOpExpr)
	if !ok {
		return
	}
	if isSameExpr(e.Lhs, e.Rhs) {
		ctx.Reportf(e, lint.SeverityError,
			"comparison of identical expressions; result is always %s",
			alwaysResult(e.Operator))
	}
}

func isSameExpr(a, b ast.Expr) bool {
	if a == nil || b == nil {
		return false
	}

	switch va := a.(type) {
	case *ast.IdentExpr:
		if vb, ok := b.(*ast.IdentExpr); ok {
			return va.Value == vb.Value
		}

	case *ast.AttrGetExpr:
		if vb, ok := b.(*ast.AttrGetExpr); ok {
			return isSameExpr(va.Object, vb.Object) && isSameExpr(va.Key, vb.Key)
		}

	case *ast.StringExpr:
		if vb, ok := b.(*ast.StringExpr); ok {
			return va.Value == vb.Value
		}

	case *ast.NumberExpr:
		if vb, ok := b.(*ast.NumberExpr); ok {
			return va.Value == vb.Value
		}
	}

	return false
}

func alwaysResult(op string) string {
	switch op {
	case "==", "<=", ">=":
		return "true"
	case "~=", "<", ">":
		return "false"
	default:
		return "constant"
	}
}
