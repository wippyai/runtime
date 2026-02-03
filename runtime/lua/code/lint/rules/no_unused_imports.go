package rules

import (
	"strings"

	"github.com/wippyai/go-lua/compiler/ast"
	"github.com/wippyai/go-lua/compiler/bind"
	"github.com/wippyai/go-lua/compiler/cfg"
	"github.com/wippyai/runtime/runtime/lua/code/lint"
)

func init() {
	lint.Register(&NoUnusedImports{})
}

// NoUnusedImports detects local X = require("module") where X is never used.
type NoUnusedImports struct{}

func (r *NoUnusedImports) Meta() lint.RuleMeta {
	return lint.RuleMeta{
		Name:            "no-unused-imports",
		Code:            "W0006",
		DiagCode:        lint.LintW0006,
		Title:           "unused import",
		Description:     "Imported modules should be used",
		DefaultSeverity: lint.SeverityWarning,
		Category:        "correctness",
	}
}

func (r *NoUnusedImports) Check(ctx *lint.Context) {
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

func (r *NoUnusedImports) walkStmts(ctx *lint.Context, stmts []ast.Stmt, bindings *bind.BindingTable, reads map[cfg.SymbolID]bool) {
	for _, stmt := range stmts {
		r.walkStmt(ctx, stmt, bindings, reads)
	}
}

func (r *NoUnusedImports) walkStmt(ctx *lint.Context, stmt ast.Stmt, bindings *bind.BindingTable, reads map[cfg.SymbolID]bool) {
	if stmt == nil {
		return
	}

	switch s := stmt.(type) {
	case *ast.LocalAssignStmt:
		if !hasRequireCall(s.Exprs) {
			return
		}
		for i, sym := range bindings.LocalSymbols(s) {
			name := bindings.Name(sym)
			if strings.HasPrefix(name, "_") {
				continue
			}
			if reads[sym] {
				continue
			}
			ctx.Reportf(lint.NamePos(s, i), lint.SeverityWarning,
				"imported module '%s' is never used", name)
		}

	case *ast.DoBlockStmt:
		r.walkStmts(ctx, s.Stmts, bindings, reads)
	case *ast.WhileStmt:
		r.walkStmts(ctx, s.Stmts, bindings, reads)
	case *ast.RepeatStmt:
		r.walkStmts(ctx, s.Stmts, bindings, reads)
	case *ast.IfStmt:
		r.walkStmts(ctx, s.Then, bindings, reads)
		r.walkStmts(ctx, s.Else, bindings, reads)
	case *ast.NumberForStmt:
		r.walkStmts(ctx, s.Stmts, bindings, reads)
	case *ast.GenericForStmt:
		r.walkStmts(ctx, s.Stmts, bindings, reads)
	case *ast.FuncDefStmt:
		if s.Func != nil {
			r.walkStmts(ctx, s.Func.Stmts, bindings, reads)
		}
	}
}

// hasRequireCall checks if any expression in the list is a require() call.
func hasRequireCall(exprs []ast.Expr) bool {
	for _, expr := range exprs {
		if call, ok := expr.(*ast.FuncCallExpr); ok {
			if ident, ok := call.Func.(*ast.IdentExpr); ok && ident.Value == "require" {
				return true
			}
		}
	}
	return false
}
