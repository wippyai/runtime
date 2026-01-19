// Package lint provides a plugin architecture for Lua code analysis.
//
// Architecture:
//
//	┌─────────────────────────────────────────────────────────┐
//	│                     wippy lint                          │
//	└─────────────────────────────────────────────────────────┘
//	                          │
//	                          ▼
//	┌─────────────────────────────────────────────────────────┐
//	│                    Linter                               │
//	│  - Coordinates type checking + lint rules               │
//	│  - Merges diagnostics from all sources                  │
//	└─────────────────────────────────────────────────────────┘
//	          │                              │
//	          ▼                              ▼
//	┌──────────────────┐          ┌──────────────────────────┐
//	│  TypeChecker     │          │    RuleRegistry          │
//	│  (core errors)   │          │    (lint rules)          │
//	└──────────────────┘          └──────────────────────────┘
//	                                         │
//	                    ┌────────────────────┼────────────────────┐
//	                    ▼                    ▼                    ▼
//	             ┌──────────┐         ┌──────────┐         ┌──────────┐
//	             │  Rule 1  │         │  Rule 2  │         │  Rule N  │
//	             └──────────┘         └──────────┘         └──────────┘
//
// Separation of Concerns:
//   - TypeChecker: Core type errors (E0001-E0099) - always errors
//   - Lint Rules: Style/quality warnings (W0001+) - configurable severity
//
// Rule Interface:
//   - Each rule receives full context (AST, scope, types)
//   - Rules are stateless and independent
//   - Rules declare their own metadata (name, docs, default severity)
package lint

import (
	"github.com/yuin/gopher-lua/compiler/ast"
	"github.com/yuin/gopher-lua/compiler/check"
	"github.com/yuin/gopher-lua/types/cfg"
	"github.com/yuin/gopher-lua/types/diag"
	"github.com/yuin/gopher-lua/types/scope"
	"github.com/yuin/gopher-lua/types/typ"
)

// Severity levels for lint rules
type Severity int

const (
	SeverityOff     Severity = iota // Rule disabled
	SeverityHint                    // Informational
	SeverityWarning                 // Should fix
	SeverityError                   // Must fix
)

// RuleMeta contains metadata about a lint rule
type RuleMeta struct {
	// Name is the unique identifier (e.g., "no-unused-vars")
	Name string

	// Code is the diagnostic code (e.g., "W0001")
	Code string

	// Title is a short description
	Title string

	// Description explains what the rule checks
	Description string

	// DefaultSeverity is the severity when not configured
	DefaultSeverity Severity

	// Category groups related rules (e.g., "style", "correctness", "performance")
	Category string
}

// Context provides rule access to analysis results
type Context struct {
	// Source file identifier
	Source string

	// AST is the parsed syntax tree
	AST []ast.Stmt

	// Scope is the resolved scope at chunk level
	Scope *scope.State

	// CFG is the control flow graph (may be nil for simple rules)
	CFG *cfg.CFG

	// Checker provides type queries
	Checker *check.Checker

	// Collector receives diagnostics
	Collector *diag.Collector
}

// TypeAt returns the type of an expression at a CFG point
func (c *Context) TypeAt(point cfg.Point, expr ast.Expr) typ.Type {
	if c.Checker == nil || c.CFG == nil {
		return typ.Unknown
	}
	return c.Checker.SynthAt(c.CFG, point, expr, c.Scope)
}

// LookupSymbol returns the type of a symbol in scope
func (c *Context) LookupSymbol(name string) (typ.Type, bool) {
	if c.Scope == nil {
		return nil, false
	}
	return c.Scope.Lookup(name)
}

// Report adds a diagnostic at the given node
func (c *Context) Report(node diag.PositionHolder, severity Severity, format string, args ...any) {
	if c.Collector == nil {
		return
	}
	switch severity {
	case SeverityError:
		c.Collector.Add(node, diag.ErrNoHandler, format, args...)
	case SeverityWarning:
		c.Collector.AddWarning(node, diag.ErrNoHandler, format, args...)
	case SeverityHint:
		c.Collector.AddHint(node, diag.ErrNoHandler, format, args...)
	}
}

// Rule is the interface that lint rules must implement
type Rule interface {
	// Meta returns the rule's metadata
	Meta() RuleMeta

	// Check analyzes the code and reports diagnostics via context
	Check(ctx *Context)
}

// NodeVisitor is an optional interface for rules that walk the AST
type NodeVisitor interface {
	Rule

	// VisitStmt is called for each statement
	VisitStmt(ctx *Context, stmt ast.Stmt)

	// VisitExpr is called for each expression
	VisitExpr(ctx *Context, expr ast.Expr)
}
