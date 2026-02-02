package rules

import (
	"strings"

	"github.com/wippyai/runtime/runtime/lua/code/lint"
	"github.com/wippyai/go-lua/compiler/ast"
	"github.com/wippyai/go-lua/compiler/bind"
	"github.com/wippyai/go-lua/compiler/cfg"
)

func init() {
	lint.Register(&NoUnusedParams{})
}

// NoUnusedParams detects unused function parameters.
type NoUnusedParams struct{}

func (r *NoUnusedParams) Meta() lint.RuleMeta {
	return lint.RuleMeta{
		Name:            "no-unused-params",
		Code:            "W0005",
		DiagCode:        lint.LintW0005,
		Title:           "unused parameter",
		Description:     "Function parameters should be used in the function body",
		DefaultSeverity: lint.SeverityHint,
		Category:        "style",
	}
}

func (r *NoUnusedParams) Check(ctx *lint.Context) {
	if ctx.CFG == nil {
		return
	}

	bindings := ctx.CFG.Bindings()
	if bindings == nil {
		return
	}

	reads := collectReads(ctx.AST, bindings)
	r.walkFunctions(ctx, ctx.AST, bindings, reads)
}

func (r *NoUnusedParams) walkFunctions(ctx *lint.Context, stmts []ast.Stmt, bindings *bind.BindingTable, reads map[cfg.SymbolID]bool) {
	for _, stmt := range stmts {
		r.walkFuncStmt(ctx, stmt, bindings, reads)
	}
}

func (r *NoUnusedParams) walkFuncStmt(ctx *lint.Context, stmt ast.Stmt, bindings *bind.BindingTable, reads map[cfg.SymbolID]bool) {
	if stmt == nil {
		return
	}

	switch s := stmt.(type) {
	case *ast.FuncDefStmt:
		if s.Func != nil {
			r.checkFunc(ctx, s.Func, bindings, reads)
			r.walkFunctions(ctx, s.Func.Stmts, bindings, reads)
		}
	case *ast.LocalAssignStmt:
		for _, expr := range s.Exprs {
			r.walkFuncExpr(ctx, expr, bindings, reads)
		}
	case *ast.AssignStmt:
		for _, expr := range s.Rhs {
			r.walkFuncExpr(ctx, expr, bindings, reads)
		}
	case *ast.DoBlockStmt:
		r.walkFunctions(ctx, s.Stmts, bindings, reads)
	case *ast.WhileStmt:
		r.walkFunctions(ctx, s.Stmts, bindings, reads)
	case *ast.RepeatStmt:
		r.walkFunctions(ctx, s.Stmts, bindings, reads)
	case *ast.IfStmt:
		r.walkFunctions(ctx, s.Then, bindings, reads)
		r.walkFunctions(ctx, s.Else, bindings, reads)
	case *ast.NumberForStmt:
		r.walkFunctions(ctx, s.Stmts, bindings, reads)
	case *ast.GenericForStmt:
		r.walkFunctions(ctx, s.Stmts, bindings, reads)
	case *ast.ReturnStmt:
		for _, expr := range s.Exprs {
			r.walkFuncExpr(ctx, expr, bindings, reads)
		}
	}
}

func (r *NoUnusedParams) walkFuncExpr(ctx *lint.Context, expr ast.Expr, bindings *bind.BindingTable, reads map[cfg.SymbolID]bool) {
	if expr == nil {
		return
	}
	if fn, ok := expr.(*ast.FunctionExpr); ok {
		r.checkFunc(ctx, fn, bindings, reads)
		r.walkFunctions(ctx, fn.Stmts, bindings, reads)
	}
}

func (r *NoUnusedParams) checkFunc(ctx *lint.Context, fn *ast.FunctionExpr, bindings *bind.BindingTable, reads map[cfg.SymbolID]bool) {
	params := bindings.ParamSymbols(fn)
	for _, sym := range params {
		name := bindings.Name(sym)
		if name == "self" {
			continue
		}
		if strings.HasPrefix(name, "_") {
			continue
		}
		if name == "..." {
			continue
		}
		if reads[sym] {
			continue
		}
		ctx.Reportf(fn, lint.SeverityHint,
			"parameter '%s' is unused", name)
	}
}
