package rules

import (
	"strings"

	"github.com/wippyai/runtime/runtime/lua/code/lint"
	"github.com/wippyai/go-lua/compiler/ast"
	"github.com/wippyai/go-lua/compiler/bind"
	"github.com/wippyai/go-lua/compiler/cfg"
)

func init() {
	lint.Register(&NoShadowedVars{})
}

// NoShadowedVars detects variable declarations that shadow outer scope variables.
type NoShadowedVars struct{}

func (r *NoShadowedVars) Meta() lint.RuleMeta {
	return lint.RuleMeta{
		Name:            "no-shadowed-vars",
		Code:            "W0007",
		DiagCode:        lint.LintW0007,
		Title:           "shadowed variable",
		Description:     "Variable declarations should not shadow outer scope variables",
		DefaultSeverity: lint.SeverityHint,
		Category:        "style",
	}
}

func (r *NoShadowedVars) Check(ctx *lint.Context) {
	if ctx.CFG == nil {
		return
	}

	bindings := ctx.CFG.Bindings()
	if bindings == nil {
		return
	}

	// Build a set of names that have multiple local/param symbols (potential shadows)
	allSymbols := bindings.AllSymbols()
	nameCount := make(map[string]int)
	for _, sym := range allSymbols {
		kind, ok := bindings.Kind(sym)
		if !ok || (kind != cfg.SymbolLocal && kind != cfg.SymbolParam) {
			continue
		}
		name := bindings.Name(sym)
		if strings.HasPrefix(name, "_") {
			continue
		}
		nameCount[name]++
	}

	// Only check names that appear more than once
	shadowNames := make(map[string]bool)
	for name, count := range nameCount {
		if count > 1 {
			shadowNames[name] = true
		}
	}
	if len(shadowNames) == 0 {
		return
	}

	// Walk AST to find inner declarations that shadow outer ones.
	// Track declared names at each scope level.
	r.walkBlock(ctx, ctx.AST, bindings, shadowNames, nil)
}

func (r *NoShadowedVars) walkBlock(ctx *lint.Context, stmts []ast.Stmt, bindings *bind.BindingTable, shadowNames map[string]bool, outerNames map[string]bool) {
	// Collect names declared in this scope
	localNames := make(map[string]bool)
	for k, v := range outerNames {
		localNames[k] = v
	}

	for _, stmt := range stmts {
		r.walkStmt(ctx, stmt, bindings, shadowNames, localNames)
	}
}

func (r *NoShadowedVars) walkStmt(ctx *lint.Context, stmt ast.Stmt, bindings *bind.BindingTable, shadowNames map[string]bool, scopeNames map[string]bool) {
	if stmt == nil {
		return
	}

	switch s := stmt.(type) {
	case *ast.LocalAssignStmt:
		for i, sym := range bindings.LocalSymbols(s) {
			name := bindings.Name(sym)
			if !shadowNames[name] {
				continue
			}
			if scopeNames[name] {
				ctx.Reportf(lint.NamePos(s, i), lint.SeverityHint,
					"variable '%s' shadows a variable in an outer scope", name)
			}
			scopeNames[name] = true
		}
		// Walk RHS expressions for nested functions
		for _, expr := range s.Exprs {
			r.walkExprForFunctions(ctx, expr, bindings, shadowNames, scopeNames)
		}

	case *ast.DoBlockStmt:
		r.walkBlock(ctx, s.Stmts, bindings, shadowNames, scopeNames)

	case *ast.WhileStmt:
		r.walkBlock(ctx, s.Stmts, bindings, shadowNames, scopeNames)

	case *ast.RepeatStmt:
		r.walkBlock(ctx, s.Stmts, bindings, shadowNames, scopeNames)

	case *ast.IfStmt:
		r.walkBlock(ctx, s.Then, bindings, shadowNames, scopeNames)
		r.walkBlock(ctx, s.Else, bindings, shadowNames, scopeNames)

	case *ast.NumberForStmt:
		forScope := make(map[string]bool)
		for k, v := range scopeNames {
			forScope[k] = v
		}
		if sym, ok := bindings.NumForSymbol(s); ok {
			name := bindings.Name(sym)
			if shadowNames[name] && forScope[name] {
				ctx.Reportf(lint.NumForNamePos(s), lint.SeverityHint,
					"variable '%s' shadows a variable in an outer scope", name)
			}
			forScope[name] = true
		}
		r.walkBlock(ctx, s.Stmts, bindings, shadowNames, forScope)

	case *ast.GenericForStmt:
		forScope := make(map[string]bool)
		for k, v := range scopeNames {
			forScope[k] = v
		}
		for i, sym := range bindings.GenericForSymbols(s) {
			name := bindings.Name(sym)
			if shadowNames[name] && forScope[name] {
				ctx.Reportf(lint.GenForNamePos(s, i), lint.SeverityHint,
					"variable '%s' shadows a variable in an outer scope", name)
			}
			forScope[name] = true
		}
		r.walkBlock(ctx, s.Stmts, bindings, shadowNames, forScope)

	case *ast.FuncDefStmt:
		if s.Func != nil {
			r.walkFunction(ctx, s.Func, bindings, shadowNames, scopeNames)
		}

	case *ast.ReturnStmt:
		for _, expr := range s.Exprs {
			r.walkExprForFunctions(ctx, expr, bindings, shadowNames, scopeNames)
		}

	case *ast.AssignStmt:
		for _, expr := range s.Rhs {
			r.walkExprForFunctions(ctx, expr, bindings, shadowNames, scopeNames)
		}

	case *ast.FuncCallStmt:
		r.walkExprForFunctions(ctx, s.Expr, bindings, shadowNames, scopeNames)
	}
}

func (r *NoShadowedVars) walkFunction(ctx *lint.Context, fn *ast.FunctionExpr, bindings *bind.BindingTable, shadowNames map[string]bool, outerNames map[string]bool) {
	fnScope := make(map[string]bool)
	for k, v := range outerNames {
		fnScope[k] = v
	}

	// Check params
	for _, sym := range bindings.ParamSymbols(fn) {
		name := bindings.Name(sym)
		if !shadowNames[name] {
			continue
		}
		if fnScope[name] {
			ctx.Reportf(fn, lint.SeverityHint,
				"variable '%s' shadows a variable in an outer scope", name)
		}
		fnScope[name] = true
	}

	r.walkBlock(ctx, fn.Stmts, bindings, shadowNames, fnScope)
}

func (r *NoShadowedVars) walkExprForFunctions(ctx *lint.Context, expr ast.Expr, bindings *bind.BindingTable, shadowNames map[string]bool, scopeNames map[string]bool) {
	if expr == nil {
		return
	}
	if fn, ok := expr.(*ast.FunctionExpr); ok {
		r.walkFunction(ctx, fn, bindings, shadowNames, scopeNames)
	}
}
