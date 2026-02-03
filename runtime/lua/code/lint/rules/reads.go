package rules

import (
	"github.com/wippyai/go-lua/compiler/ast"
	"github.com/wippyai/go-lua/compiler/bind"
	"github.com/wippyai/go-lua/compiler/cfg"
)

// collectReads walks the AST and returns the set of symbol IDs that appear in read position.
func collectReads(stmts []ast.Stmt, bindings *bind.BindingTable) map[cfg.SymbolID]bool {
	reads := make(map[cfg.SymbolID]bool)
	for _, stmt := range stmts {
		collectReadsStmt(stmt, bindings, reads)
	}
	return reads
}

func collectReadsStmt(stmt ast.Stmt, bindings *bind.BindingTable, reads map[cfg.SymbolID]bool) {
	if stmt == nil {
		return
	}

	switch s := stmt.(type) {
	case *ast.AssignStmt:
		// LHS: top-level IdentExpr is a write, but AttrGetExpr.Object is a read
		for _, lhs := range s.Lhs {
			collectReadsLHS(lhs, bindings, reads)
		}
		// RHS: all expressions are reads
		for _, rhs := range s.Rhs {
			collectReadsExpr(rhs, bindings, reads)
		}

	case *ast.LocalAssignStmt:
		// LHS names are writes (declarations), RHS expressions are reads
		for _, expr := range s.Exprs {
			collectReadsExpr(expr, bindings, reads)
		}

	case *ast.FuncCallStmt:
		collectReadsExpr(s.Expr, bindings, reads)

	case *ast.DoBlockStmt:
		for _, child := range s.Stmts {
			collectReadsStmt(child, bindings, reads)
		}

	case *ast.WhileStmt:
		collectReadsExpr(s.Condition, bindings, reads)
		for _, child := range s.Stmts {
			collectReadsStmt(child, bindings, reads)
		}

	case *ast.RepeatStmt:
		for _, child := range s.Stmts {
			collectReadsStmt(child, bindings, reads)
		}
		collectReadsExpr(s.Condition, bindings, reads)

	case *ast.IfStmt:
		collectReadsExpr(s.Condition, bindings, reads)
		for _, child := range s.Then {
			collectReadsStmt(child, bindings, reads)
		}
		for _, child := range s.Else {
			collectReadsStmt(child, bindings, reads)
		}

	case *ast.NumberForStmt:
		collectReadsExpr(s.Init, bindings, reads)
		collectReadsExpr(s.Limit, bindings, reads)
		if s.Step != nil {
			collectReadsExpr(s.Step, bindings, reads)
		}
		for _, child := range s.Stmts {
			collectReadsStmt(child, bindings, reads)
		}

	case *ast.GenericForStmt:
		for _, expr := range s.Exprs {
			collectReadsExpr(expr, bindings, reads)
		}
		for _, child := range s.Stmts {
			collectReadsStmt(child, bindings, reads)
		}

	case *ast.FuncDefStmt:
		if s.Func != nil {
			for _, child := range s.Func.Stmts {
				collectReadsStmt(child, bindings, reads)
			}
		}

	case *ast.ReturnStmt:
		for _, expr := range s.Exprs {
			collectReadsExpr(expr, bindings, reads)
		}
	}
}

// collectReadsLHS handles the left-hand side of an assignment.
// Top-level IdentExpr is a write (skip it), but AttrGetExpr.Object is a read.
func collectReadsLHS(expr ast.Expr, bindings *bind.BindingTable, reads map[cfg.SymbolID]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.IdentExpr:
		// top-level ident on LHS is a write, not a read
	case *ast.AttrGetExpr:
		// t.x = val -> t is read; t[k] = val -> both t and k are reads
		collectReadsExpr(e.Object, bindings, reads)
		collectReadsExpr(e.Key, bindings, reads)
	default:
		collectReadsExpr(expr, bindings, reads)
	}
}

func collectReadsExpr(expr ast.Expr, bindings *bind.BindingTable, reads map[cfg.SymbolID]bool) {
	if expr == nil {
		return
	}

	switch e := expr.(type) {
	case *ast.IdentExpr:
		if sym, ok := bindings.SymbolOf(e); ok {
			reads[sym] = true
		}

	case *ast.FuncCallExpr:
		collectReadsExpr(e.Func, bindings, reads)
		collectReadsExpr(e.Receiver, bindings, reads)
		for _, arg := range e.Args {
			collectReadsExpr(arg, bindings, reads)
		}

	case *ast.AttrGetExpr:
		collectReadsExpr(e.Object, bindings, reads)
		collectReadsExpr(e.Key, bindings, reads)

	case *ast.TableExpr:
		for _, field := range e.Fields {
			collectReadsExpr(field.Key, bindings, reads)
			collectReadsExpr(field.Value, bindings, reads)
		}

	case *ast.FunctionExpr:
		for _, child := range e.Stmts {
			collectReadsStmt(child, bindings, reads)
		}

	case *ast.LogicalOpExpr:
		collectReadsExpr(e.Lhs, bindings, reads)
		collectReadsExpr(e.Rhs, bindings, reads)

	case *ast.RelationalOpExpr:
		collectReadsExpr(e.Lhs, bindings, reads)
		collectReadsExpr(e.Rhs, bindings, reads)

	case *ast.ArithmeticOpExpr:
		collectReadsExpr(e.Lhs, bindings, reads)
		collectReadsExpr(e.Rhs, bindings, reads)

	case *ast.StringConcatOpExpr:
		collectReadsExpr(e.Lhs, bindings, reads)
		collectReadsExpr(e.Rhs, bindings, reads)

	case *ast.UnaryMinusOpExpr:
		collectReadsExpr(e.Expr, bindings, reads)

	case *ast.UnaryNotOpExpr:
		collectReadsExpr(e.Expr, bindings, reads)

	case *ast.UnaryLenOpExpr:
		collectReadsExpr(e.Expr, bindings, reads)

	case *ast.CastExpr:
		collectReadsExpr(e.Expr, bindings, reads)

	case *ast.NonNilAssertExpr:
		collectReadsExpr(e.Expr, bindings, reads)
	}
}
