// SPDX-License-Identifier: MPL-2.0

package rules

import (
	"strings"

	"github.com/wippyai/go-lua/compiler/ast"
	"github.com/wippyai/go-lua/compiler/bind"
	"github.com/wippyai/go-lua/compiler/cfg"
	"github.com/wippyai/runtime/runtime/lua/code/lint"
)

func init() {
	lint.Register(&NoUnusedVars{})
}

// NoUnusedVars detects local variables that are declared but never read.
type NoUnusedVars struct{}

func (r *NoUnusedVars) Meta() lint.RuleMeta {
	return lint.RuleMeta{
		Name:            "no-unused-vars",
		Code:            "W0004",
		DiagCode:        lint.LintW0004,
		Title:           "unused variable",
		Description:     "Local variables should be used after declaration",
		DefaultSeverity: lint.SeverityWarning,
		Category:        "correctness",
	}
}

func (r *NoUnusedVars) Check(ctx *lint.Context) {
	if ctx.CFG == nil {
		return
	}

	bindings := ctx.CFG.Bindings()
	if bindings == nil {
		return
	}

	reads := collectReads(ctx.AST, bindings)
	r.walkStmts(ctx, ctx.AST, bindings, reads)
}

func (r *NoUnusedVars) walkStmts(ctx *lint.Context, stmts []ast.Stmt, bindings *bind.BindingTable, reads map[cfg.SymbolID]bool) {
	for _, stmt := range stmts {
		r.walkStmt(ctx, stmt, bindings, reads)
	}
}

func (r *NoUnusedVars) walkStmt(ctx *lint.Context, stmt ast.Stmt, bindings *bind.BindingTable, reads map[cfg.SymbolID]bool) {
	if stmt == nil {
		return
	}

	switch s := stmt.(type) {
	case *ast.LocalAssignStmt:
		for i, sym := range bindings.LocalSymbols(s) {
			name := bindings.Name(sym)
			if strings.HasPrefix(name, "_") {
				continue
			}
			if reads[sym] {
				continue
			}
			ctx.Reportf(lint.NamePos(s, i), lint.SeverityWarning,
				"variable '%s' is declared but never used", name)
		}
		for _, expr := range s.Exprs {
			r.walkExpr(ctx, expr, bindings, reads)
		}

	case *ast.NumberForStmt:
		if sym, ok := bindings.NumForSymbol(s); ok {
			name := bindings.Name(sym)
			if !strings.HasPrefix(name, "_") && !reads[sym] {
				ctx.Reportf(lint.NumForNamePos(s), lint.SeverityWarning,
					"loop variable '%s' is declared but never used", name)
			}
		}
		r.walkStmts(ctx, s.Stmts, bindings, reads)

	case *ast.GenericForStmt:
		for i, sym := range bindings.GenericForSymbols(s) {
			name := bindings.Name(sym)
			if !strings.HasPrefix(name, "_") && !reads[sym] {
				ctx.Reportf(lint.GenForNamePos(s, i), lint.SeverityWarning,
					"loop variable '%s' is declared but never used", name)
			}
		}
		r.walkStmts(ctx, s.Stmts, bindings, reads)

	case *ast.DoBlockStmt:
		r.walkStmts(ctx, s.Stmts, bindings, reads)
	case *ast.WhileStmt:
		r.walkStmts(ctx, s.Stmts, bindings, reads)
	case *ast.RepeatStmt:
		r.walkStmts(ctx, s.Stmts, bindings, reads)
	case *ast.IfStmt:
		r.walkStmts(ctx, s.Then, bindings, reads)
		r.walkStmts(ctx, s.Else, bindings, reads)
	case *ast.FuncDefStmt:
		if s.Func != nil {
			r.walkStmts(ctx, s.Func.Stmts, bindings, reads)
		}
	case *ast.AssignStmt:
		for _, expr := range s.Rhs {
			r.walkExpr(ctx, expr, bindings, reads)
		}
	case *ast.ReturnStmt:
		for _, expr := range s.Exprs {
			r.walkExpr(ctx, expr, bindings, reads)
		}
	}
}

func (r *NoUnusedVars) walkExpr(ctx *lint.Context, expr ast.Expr, bindings *bind.BindingTable, reads map[cfg.SymbolID]bool) {
	if expr == nil {
		return
	}
	if fn, ok := expr.(*ast.FunctionExpr); ok {
		r.walkStmts(ctx, fn.Stmts, bindings, reads)
	}
}
