package rules

import (
	"github.com/wippyai/runtime/runtime/lua/code/lint"
	"github.com/yuin/gopher-lua/compiler/ast"
)

func init() {
	lint.Register(&NoGlobalAssign{})
}

// NoGlobalAssign detects assignments to undeclared variables (global escapes).
// Uses scope-sensitive AST walking to track local declarations.
type NoGlobalAssign struct{}

func (r *NoGlobalAssign) Meta() lint.RuleMeta {
	return lint.RuleMeta{
		Name:            "no-global-assign",
		Code:            "W0002",
		Title:           "global variable assignment",
		Description:     "Assignments should use local variables; global assignments may cause unintended side effects",
		DefaultSeverity: lint.SeverityWarning,
		Category:        "correctness",
	}
}

func (r *NoGlobalAssign) Check(ctx *lint.Context) {
	tracker := newScopeTracker(ctx)
	tracker.checkBlock(ctx.AST)
}

// scopeTracker maintains a stack of local variable scopes during AST traversal
type scopeTracker struct {
	ctx    *lint.Context
	scopes []map[string]bool
}

func newScopeTracker(ctx *lint.Context) *scopeTracker {
	return &scopeTracker{
		ctx:    ctx,
		scopes: []map[string]bool{make(map[string]bool)},
	}
}

func (s *scopeTracker) pushScope() {
	s.scopes = append(s.scopes, make(map[string]bool))
}

func (s *scopeTracker) popScope() {
	if len(s.scopes) > 1 {
		s.scopes = s.scopes[:len(s.scopes)-1]
	}
}

func (s *scopeTracker) declareLocal(name string) {
	s.scopes[len(s.scopes)-1][name] = true
}

func (s *scopeTracker) isDeclared(name string) bool {
	// Check all enclosing scopes
	for i := len(s.scopes) - 1; i >= 0; i-- {
		if s.scopes[i][name] {
			return true
		}
	}
	// Check builtins from base scope
	if s.ctx.Scope != nil {
		if _, ok := s.ctx.Scope.Lookup(name); ok {
			return true
		}
	}
	return false
}

func (s *scopeTracker) checkBlock(stmts []ast.Stmt) {
	for _, stmt := range stmts {
		s.checkStmt(stmt)
	}
}

func (s *scopeTracker) checkStmt(stmt ast.Stmt) {
	if stmt == nil {
		return
	}

	switch st := stmt.(type) {
	case *ast.LocalAssignStmt:
		// Declare locals, then check RHS expressions
		for _, name := range st.Names {
			s.declareLocal(name)
		}
		for _, expr := range st.Exprs {
			s.checkExpr(expr)
		}

	case *ast.AssignStmt:
		// Check RHS first
		for _, expr := range st.Rhs {
			s.checkExpr(expr)
		}
		// Check LHS for global assignments
		for _, lhs := range st.Lhs {
			s.checkLHS(lhs)
		}

	case *ast.FuncDefStmt:
		// Named function definitions: local function foo() or function M.foo()
		s.checkFuncDef(st)

	case *ast.FuncCallStmt:
		s.checkExpr(st.Expr)

	case *ast.DoBlockStmt:
		s.pushScope()
		s.checkBlock(st.Stmts)
		s.popScope()

	case *ast.WhileStmt:
		s.checkExpr(st.Condition)
		s.pushScope()
		s.checkBlock(st.Stmts)
		s.popScope()

	case *ast.RepeatStmt:
		s.pushScope()
		s.checkBlock(st.Stmts)
		s.checkExpr(st.Condition)
		s.popScope()

	case *ast.IfStmt:
		s.checkExpr(st.Condition)
		s.pushScope()
		s.checkBlock(st.Then)
		s.popScope()
		s.pushScope()
		s.checkBlock(st.Else)
		s.popScope()

	case *ast.NumberForStmt:
		s.checkExpr(st.Init)
		s.checkExpr(st.Limit)
		if st.Step != nil {
			s.checkExpr(st.Step)
		}
		s.pushScope()
		s.declareLocal(st.Name)
		s.checkBlock(st.Stmts)
		s.popScope()

	case *ast.GenericForStmt:
		for _, expr := range st.Exprs {
			s.checkExpr(expr)
		}
		s.pushScope()
		for _, name := range st.Names {
			s.declareLocal(name)
		}
		s.checkBlock(st.Stmts)
		s.popScope()

	case *ast.ReturnStmt:
		for _, expr := range st.Exprs {
			s.checkExpr(expr)
		}

	case *ast.LabelStmt, *ast.GotoStmt, *ast.BreakStmt:
		// No expressions or scopes
	}
}

func (s *scopeTracker) checkFuncDef(fd *ast.FuncDefStmt) {
	// function foo() defines global foo
	// local function foo() handled by LocalAssignStmt wrapping
	// function M.foo() or M:foo() - method on table, not a global
	if fd.Name != nil {
		if ident, ok := fd.Name.Func.(*ast.IdentExpr); ok && fd.Name.Receiver == nil {
			// Plain function foo() - check if foo is declared
			if !s.isDeclared(ident.Value) {
				s.ctx.Report(ident, lint.SeverityWarning,
					"function '%s' declared as global; consider using 'local function'", ident.Value)
			}
		}
	}
	// Check function body
	if fd.Func != nil {
		s.checkFunctionExpr(fd.Func)
	}
}

func (s *scopeTracker) checkLHS(expr ast.Expr) {
	switch e := expr.(type) {
	case *ast.IdentExpr:
		if !s.isDeclared(e.Value) {
			s.ctx.Report(e, lint.SeverityWarning,
				"assignment to global variable '%s'; consider using 'local'", e.Value)
		}
	case *ast.AttrGetExpr:
		// table.field = value is not a global escape
		s.checkExpr(e.Object)
	}
}

func (s *scopeTracker) checkExpr(expr ast.Expr) {
	if expr == nil {
		return
	}

	switch e := expr.(type) {
	case *ast.FunctionExpr:
		s.checkFunctionExpr(e)

	case *ast.FuncCallExpr:
		s.checkExpr(e.Func)
		s.checkExpr(e.Receiver)
		for _, arg := range e.Args {
			s.checkExpr(arg)
		}

	case *ast.AttrGetExpr:
		s.checkExpr(e.Object)
		s.checkExpr(e.Key)

	case *ast.TableExpr:
		for _, field := range e.Fields {
			s.checkExpr(field.Key)
			s.checkExpr(field.Value)
		}

	case *ast.LogicalOpExpr:
		s.checkExpr(e.Lhs)
		s.checkExpr(e.Rhs)

	case *ast.RelationalOpExpr:
		s.checkExpr(e.Lhs)
		s.checkExpr(e.Rhs)

	case *ast.ArithmeticOpExpr:
		s.checkExpr(e.Lhs)
		s.checkExpr(e.Rhs)

	case *ast.StringConcatOpExpr:
		s.checkExpr(e.Lhs)
		s.checkExpr(e.Rhs)

	case *ast.UnaryMinusOpExpr:
		s.checkExpr(e.Expr)

	case *ast.UnaryNotOpExpr:
		s.checkExpr(e.Expr)

	case *ast.UnaryLenOpExpr:
		s.checkExpr(e.Expr)

	case *ast.IdentExpr, *ast.NumberExpr, *ast.StringExpr, *ast.TrueExpr,
		*ast.FalseExpr, *ast.NilExpr, *ast.Comma3Expr:
		// Leaf nodes - no children
	}
}

func (s *scopeTracker) checkFunctionExpr(fn *ast.FunctionExpr) {
	s.pushScope()
	// Declare parameters as locals
	for _, param := range fn.ParList.Names {
		s.declareLocal(param)
	}
	s.checkBlock(fn.Stmts)
	s.popScope()
}
