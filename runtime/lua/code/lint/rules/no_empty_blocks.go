// Package rules contains built-in lint rules
package rules

import (
	"github.com/wippyai/runtime/runtime/lua/code/lint"
	"github.com/yuin/gopher-lua/compiler/ast"
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
		Title:           "empty block",
		Description:     "Control flow blocks (if, while, for) should not be empty",
		DefaultSeverity: lint.SeverityWarning,
		Category:        "style",
	}
}

func (r *NoEmptyBlocks) Check(ctx *lint.Context) {
	for _, stmt := range ctx.AST {
		r.checkStmt(ctx, stmt)
	}
}

func (r *NoEmptyBlocks) checkStmt(ctx *lint.Context, stmt ast.Stmt) {
	if stmt == nil {
		return
	}

	switch s := stmt.(type) {
	case *ast.IfStmt:
		if len(s.Then) == 0 {
			ctx.Report(s, lint.SeverityWarning, "empty if block")
		}
		for _, child := range s.Then {
			r.checkStmt(ctx, child)
		}
		for _, child := range s.Else {
			r.checkStmt(ctx, child)
		}

	case *ast.WhileStmt:
		if len(s.Stmts) == 0 {
			ctx.Report(s, lint.SeverityWarning, "empty while block")
		}
		for _, child := range s.Stmts {
			r.checkStmt(ctx, child)
		}

	case *ast.RepeatStmt:
		if len(s.Stmts) == 0 {
			ctx.Report(s, lint.SeverityWarning, "empty repeat block")
		}
		for _, child := range s.Stmts {
			r.checkStmt(ctx, child)
		}

	case *ast.NumberForStmt:
		if len(s.Stmts) == 0 {
			ctx.Report(s, lint.SeverityWarning, "empty for block")
		}
		for _, child := range s.Stmts {
			r.checkStmt(ctx, child)
		}

	case *ast.GenericForStmt:
		if len(s.Stmts) == 0 {
			ctx.Report(s, lint.SeverityWarning, "empty for block")
		}
		for _, child := range s.Stmts {
			r.checkStmt(ctx, child)
		}

	case *ast.DoBlockStmt:
		for _, child := range s.Stmts {
			r.checkStmt(ctx, child)
		}

	case *ast.FuncDefStmt:
		if s.Func != nil {
			for _, child := range s.Func.Stmts {
				r.checkStmt(ctx, child)
			}
		}
	}
}
