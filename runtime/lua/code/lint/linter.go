// SPDX-License-Identifier: MPL-2.0

package lint

import (
	"github.com/wippyai/go-lua/compiler/ast"
	"github.com/wippyai/go-lua/compiler/cfg"
	"github.com/wippyai/go-lua/compiler/parse"
	"github.com/wippyai/go-lua/types/diag"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/runtime/runtime/lua/code"
)

// Linter coordinates type checking and lint rule execution
type Linter struct {
	typeChecker *code.TypeChecker
	registry    *Registry
}

// New creates a linter with the given type checker and rule registry
func New(tc *code.TypeChecker, registry *Registry) *Linter {
	if registry == nil {
		registry = DefaultRegistry
	}
	return &Linter{
		typeChecker: tc,
		registry:    registry,
	}
}

// Result contains the output of linting
type Result struct {
	ParseError  error
	Manifest    *io.Manifest
	Diagnostics []diag.Diagnostic
}

// Check parses and analyzes Lua source code
func (l *Linter) Check(source, entryID string, imports map[string]*io.Manifest) *Result {
	// Parse
	stmts, parseErr := parse.ParseString(source, entryID)
	if parseErr != nil {
		return &Result{ParseError: parseErr}
	}

	return l.CheckParsed(stmts, entryID, imports)
}

// CheckParsed analyzes already-parsed AST
func (l *Linter) CheckParsed(stmts []ast.Stmt, entryID string, imports map[string]*io.Manifest) (result *Result) {
	return l.CheckParsedWithTypecheck(stmts, entryID, imports, true)
}

// CheckParsedWithTypecheck analyzes parsed AST with optional type checking.
func (l *Linter) CheckParsedWithTypecheck(stmts []ast.Stmt, entryID string, imports map[string]*io.Manifest, enableTypecheck bool) (result *Result) {
	result = &Result{}

	// Recover from type checker panics
	defer func() {
		if r := recover(); r != nil {
			result.Diagnostics = append(result.Diagnostics, diag.Diagnostic{
				Code:     9999,
				Severity: diag.SeverityError,
				Message:  "type checker internal error (skipped)",
				Position: diag.Position{Line: 1, Column: 1},
			})
		}
	}()

	// Phase 1: Type checking (core errors)
	if enableTypecheck && l.typeChecker != nil && l.typeChecker.IsEnabled() {
		manifest, diagnostics := l.typeChecker.CheckParsed(stmts, entryID, imports)
		result.Manifest = manifest
		result.Diagnostics = diagnostics
	}

	// Phase 2: Lint rules (style/quality warnings)
	if l.registry != nil {
		lintDiags := l.runRules(stmts, entryID)
		result.Diagnostics = append(result.Diagnostics, lintDiags...)
	}

	return result
}

// runRules executes all enabled lint rules
func (l *Linter) runRules(stmts []ast.Stmt, entryID string) []diag.Diagnostic {
	if len(stmts) == 0 {
		return nil
	}

	// Get global types for symbol resolution
	globalTypes := l.typeChecker.GlobalTypes()

	// Collect global names for CFG builder
	globalNames := make([]string, 0, len(globalTypes))
	for name := range globalTypes {
		globalNames = append(globalNames, name)
	}

	// Build CFG with known globals for proper symbol resolution
	chunkCFG := cfg.BuildBlock(stmts, globalNames...)

	// Create collector for lint diagnostics
	collector := diag.NewCollector(entryID)

	// Build context for rules
	var scopeEnv = l.typeChecker.BuildEnv()
	ctx := &Context{
		Source:      entryID,
		AST:         stmts,
		Scope:       scopeEnv,
		GlobalTypes: globalTypes,
		CFG:         chunkCFG,
		Collector:   collector,
	}

	// Run each enabled rule
	for _, rule := range l.registry.EnabledRules() {
		l.runRule(rule, ctx)
	}

	return collector.All()
}

// runRule executes a single rule with error recovery
func (l *Linter) runRule(rule Rule, ctx *Context) {
	defer func() {
		if r := recover(); r != nil {
			_ = r
		}
	}()

	ctx.ruleCode = rule.Meta().DiagCode

	// Check if rule is a node visitor
	if visitor, ok := rule.(NodeVisitor); ok {
		l.walkAST(visitor, ctx)
		return
	}

	// Standard rule - just call Check
	rule.Check(ctx)
}

// walkAST walks the AST and calls visitor methods
func (l *Linter) walkAST(visitor NodeVisitor, ctx *Context) {
	for _, stmt := range ctx.AST {
		l.walkStmt(visitor, ctx, stmt)
	}
}

func (l *Linter) walkStmt(visitor NodeVisitor, ctx *Context, stmt ast.Stmt) {
	if stmt == nil {
		return
	}

	visitor.VisitStmt(ctx, stmt)

	// Walk children
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		for _, expr := range s.Lhs {
			l.walkExpr(visitor, ctx, expr)
		}
		for _, expr := range s.Rhs {
			l.walkExpr(visitor, ctx, expr)
		}
	case *ast.LocalAssignStmt:
		for _, expr := range s.Exprs {
			l.walkExpr(visitor, ctx, expr)
		}
	case *ast.FuncCallStmt:
		l.walkExpr(visitor, ctx, s.Expr)
	case *ast.DoBlockStmt:
		for _, child := range s.Stmts {
			l.walkStmt(visitor, ctx, child)
		}
	case *ast.WhileStmt:
		l.walkExpr(visitor, ctx, s.Condition)
		for _, child := range s.Stmts {
			l.walkStmt(visitor, ctx, child)
		}
	case *ast.RepeatStmt:
		for _, child := range s.Stmts {
			l.walkStmt(visitor, ctx, child)
		}
		l.walkExpr(visitor, ctx, s.Condition)
	case *ast.IfStmt:
		l.walkExpr(visitor, ctx, s.Condition)
		for _, child := range s.Then {
			l.walkStmt(visitor, ctx, child)
		}
		for _, child := range s.Else {
			l.walkStmt(visitor, ctx, child)
		}
	case *ast.NumberForStmt:
		l.walkExpr(visitor, ctx, s.Init)
		l.walkExpr(visitor, ctx, s.Limit)
		if s.Step != nil {
			l.walkExpr(visitor, ctx, s.Step)
		}
		for _, child := range s.Stmts {
			l.walkStmt(visitor, ctx, child)
		}
	case *ast.GenericForStmt:
		for _, expr := range s.Exprs {
			l.walkExpr(visitor, ctx, expr)
		}
		for _, child := range s.Stmts {
			l.walkStmt(visitor, ctx, child)
		}
	case *ast.FuncDefStmt:
		// Walk function body
		if s.Func != nil {
			for _, child := range s.Func.Stmts {
				l.walkStmt(visitor, ctx, child)
			}
		}
	case *ast.ReturnStmt:
		for _, expr := range s.Exprs {
			l.walkExpr(visitor, ctx, expr)
		}
	}
}

func (l *Linter) walkExpr(visitor NodeVisitor, ctx *Context, expr ast.Expr) {
	if expr == nil {
		return
	}

	visitor.VisitExpr(ctx, expr)

	// Walk children
	switch e := expr.(type) {
	case *ast.FuncCallExpr:
		l.walkExpr(visitor, ctx, e.Func)
		l.walkExpr(visitor, ctx, e.Receiver)
		for _, arg := range e.Args {
			l.walkExpr(visitor, ctx, arg)
		}
	case *ast.AttrGetExpr:
		l.walkExpr(visitor, ctx, e.Object)
		l.walkExpr(visitor, ctx, e.Key)
	case *ast.TableExpr:
		for _, field := range e.Fields {
			l.walkExpr(visitor, ctx, field.Key)
			l.walkExpr(visitor, ctx, field.Value)
		}
	case *ast.FunctionExpr:
		for _, child := range e.Stmts {
			l.walkStmt(visitor, ctx, child)
		}
	case *ast.LogicalOpExpr:
		l.walkExpr(visitor, ctx, e.Lhs)
		l.walkExpr(visitor, ctx, e.Rhs)
	case *ast.RelationalOpExpr:
		l.walkExpr(visitor, ctx, e.Lhs)
		l.walkExpr(visitor, ctx, e.Rhs)
	case *ast.ArithmeticOpExpr:
		l.walkExpr(visitor, ctx, e.Lhs)
		l.walkExpr(visitor, ctx, e.Rhs)
	case *ast.StringConcatOpExpr:
		l.walkExpr(visitor, ctx, e.Lhs)
		l.walkExpr(visitor, ctx, e.Rhs)
	case *ast.UnaryMinusOpExpr:
		l.walkExpr(visitor, ctx, e.Expr)
	case *ast.UnaryNotOpExpr:
		l.walkExpr(visitor, ctx, e.Expr)
	case *ast.UnaryLenOpExpr:
		l.walkExpr(visitor, ctx, e.Expr)
	}
}

// Clone creates a copy of the linter for parallel use
func (l *Linter) Clone() *Linter {
	return &Linter{
		typeChecker: l.typeChecker.Clone(),
		registry:    l.registry.Clone(),
	}
}

// BuiltinManifest returns the manifest for a builtin module by name.
func (l *Linter) BuiltinManifest(name string) *io.Manifest {
	if l.typeChecker == nil {
		return nil
	}
	return l.typeChecker.BuiltinManifest(name)
}

// ClearCache removes memoized results from the type checker.
// Call after each file in batch operations to prevent memory accumulation.
func (l *Linter) ClearCache() {
	if l.typeChecker == nil {
		return
	}
	l.typeChecker.ClearCache()
}
