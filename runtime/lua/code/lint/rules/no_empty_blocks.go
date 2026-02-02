package rules

import (
	"github.com/wippyai/runtime/runtime/lua/code/lint"
	"github.com/wippyai/go-lua/compiler/ast"
)

func init() {
	lint.Register(&NoEmptyBlocks{})
}

// NoEmptyBlocks warns about empty control flow blocks
type NoEmptyBlocks struct{}

func (r *NoEmptyBlocks) Meta() lint.RuleMeta {
	return lint.RuleMeta{
		Name:            "no-empty-blocks",
		Code:            "W0001",
		DiagCode:        lint.LintW0001,
		Title:           "empty block",
		Description:     "Control flow blocks (if, while, for) should not be empty",
		DefaultSeverity: lint.SeverityWarning,
		Category:        "style",
	}
}

func (r *NoEmptyBlocks) Check(_ *lint.Context) {}

func (r *NoEmptyBlocks) VisitStmt(ctx *lint.Context, stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.IfStmt:
		if len(s.Then) == 0 {
			ctx.Reportf(s, lint.SeverityWarning, "empty if block")
		}
	case *ast.WhileStmt:
		if len(s.Stmts) == 0 {
			ctx.Reportf(s, lint.SeverityWarning, "empty while block")
		}
	case *ast.RepeatStmt:
		if len(s.Stmts) == 0 {
			ctx.Reportf(s, lint.SeverityWarning, "empty repeat block")
		}
	case *ast.NumberForStmt:
		if len(s.Stmts) == 0 {
			ctx.Reportf(s, lint.SeverityWarning, "empty for block")
		}
	case *ast.GenericForStmt:
		if len(s.Stmts) == 0 {
			ctx.Reportf(s, lint.SeverityWarning, "empty for block")
		}
	}
}

func (r *NoEmptyBlocks) VisitExpr(_ *lint.Context, _ ast.Expr) {}
