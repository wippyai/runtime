// SPDX-License-Identifier: MPL-2.0

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
	"fmt"

	"github.com/wippyai/go-lua/compiler/ast"
	"github.com/wippyai/go-lua/compiler/cfg"
	"github.com/wippyai/go-lua/compiler/check/scope"
	basecfg "github.com/wippyai/go-lua/types/cfg"
	"github.com/wippyai/go-lua/types/diag"
	"github.com/wippyai/go-lua/types/typ"
)

// Severity levels for lint rules
type Severity int

const (
	SeverityOff     Severity = iota // Rule disabled
	SeverityHint                    // Informational
	SeverityWarning                 // Should fix
	SeverityError                   // Must fix
)

// LintCodeBase is the base value for lint rule diagnostic codes.
// Type checker codes use E-prefix (0-9999), lint rules use W-prefix (10000+).
const LintCodeBase diag.Code = 10000

// Lint rule diagnostic codes
const (
	LintW0001 = LintCodeBase + 1 // no-empty-blocks
	LintW0002 = LintCodeBase + 2 // no-global-assign
	LintW0003 = LintCodeBase + 3 // no-self-compare
	LintW0004 = LintCodeBase + 4 // no-unused-vars
	LintW0005 = LintCodeBase + 5 // no-unused-params
	LintW0006 = LintCodeBase + 6 // no-unused-imports
	LintW0007 = LintCodeBase + 7 // no-shadowed-vars
)

// FormatLintCode formats a lint code with W-prefix (e.g., W0001).
func FormatLintCode(code diag.Code) string {
	return fmt.Sprintf("W%04d", code-LintCodeBase)
}

// RuleMeta contains metadata about a lint rule
type RuleMeta struct {
	Name            string
	Code            string
	Title           string
	Description     string
	Category        string
	DiagCode        diag.Code
	DefaultSeverity Severity
}

// Context provides rule access to analysis results
type Context struct {
	Scope       *scope.State
	GlobalTypes map[string]typ.Type
	CFG         *cfg.Graph
	Collector   *diag.Collector
	Source      string
	AST         []ast.Stmt
	ruleCode    diag.Code
}

// TypeAt returns the type of an expression at a CFG point.
// Currently returns Unknown - type queries require a full session.
func (c *Context) TypeAt(_ basecfg.Point, _ ast.Expr) typ.Type {
	return typ.Unknown
}

// LookupSymbol returns the type of a symbol in scope.
// Returns the type from GlobalTypes if available.
func (c *Context) LookupSymbol(name string) (typ.Type, bool) {
	if c.GlobalTypes == nil {
		return nil, false
	}
	t, ok := c.GlobalTypes[name]
	return t, ok
}

// Report adds a diagnostic at the given node
func (c *Context) Reportf(node diag.PositionHolder, severity Severity, format string, args ...any) {
	if c.Collector == nil {
		return
	}
	code := c.ruleCode
	if code == 0 {
		code = diag.ErrNoHandler
	}
	switch severity {
	case SeverityError:
		c.Collector.Add(node, code, format, args...)
	case SeverityWarning:
		c.Collector.AddWarning(node, code, format, args...)
	case SeverityHint:
		c.Collector.AddHint(node, code, format, args...)
	}
}

// NamePos creates a diag.PositionHolder from a LocalAssignStmt and name index.
// Falls back to the statement position if the index is out of range.
func NamePos(s *ast.LocalAssignStmt, i int) diag.PositionHolder {
	if i < len(s.NamePositions) {
		return posFromToken(s.NamePositions[i])
	}
	return s
}

// NumForNamePos creates a diag.PositionHolder for a NumberForStmt's loop variable.
func NumForNamePos(s *ast.NumberForStmt) diag.PositionHolder {
	if s.NamePosition.Line > 0 {
		return posFromToken(s.NamePosition)
	}
	return s
}

// GenForNamePos creates a diag.PositionHolder from a GenericForStmt and name index.
func GenForNamePos(s *ast.GenericForStmt, i int) diag.PositionHolder {
	if i < len(s.NamePositions) {
		return posFromToken(s.NamePositions[i])
	}
	return s
}

func posFromToken(pos ast.Position) *namePos {
	return &namePos{line: pos.Line, col: pos.Column, lastLine: pos.EndLine, lastCol: pos.EndColumn}
}

type namePos struct {
	line, col, lastLine, lastCol int
}

func (p *namePos) Line() int       { return p.line }
func (p *namePos) Column() int     { return p.col }
func (p *namePos) LastLine() int   { return p.lastLine }
func (p *namePos) LastColumn() int { return p.lastCol }

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
