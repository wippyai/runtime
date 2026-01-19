// Package rules contains built-in lint rules for Lua code analysis.
//
// Adding a new rule:
//
//  1. Create a new file (e.g., my_rule.go)
//  2. Define a struct implementing lint.Rule
//  3. Register it in init()
//
// Example:
//
//	func init() {
//	    lint.Register(&MyRule{})
//	}
//
//	type MyRule struct{}
//
//	func (r *MyRule) Meta() lint.RuleMeta {
//	    return lint.RuleMeta{
//	        Name:            "my-rule",
//	        Code:            "W0099",
//	        Title:           "short description",
//	        Description:     "longer explanation",
//	        DefaultSeverity: lint.SeverityWarning,
//	        Category:        "style",
//	    }
//	}
//
//	func (r *MyRule) Check(ctx *lint.Context) {
//	    // Analyze ctx.AST and call ctx.Report() for issues
//	}
//
// For rules that walk every AST node, also implement lint.NodeVisitor:
//
//	func (r *MyRule) VisitStmt(ctx *lint.Context, stmt ast.Stmt) { ... }
//	func (r *MyRule) VisitExpr(ctx *lint.Context, expr ast.Expr) { ... }
//
// Context provides:
//   - ctx.AST: parsed statements
//   - ctx.Scope: type environment for symbol lookup
//   - ctx.CFG: control flow graph
//   - ctx.Checker: type checker for querying types
//   - ctx.Report(node, severity, message, args...): emit diagnostic
//   - ctx.LookupSymbol(name): check if name is in scope
//
// Built-in rules:
//
// Style:
//   - no-empty-blocks (W0001): empty control flow blocks
//
// Correctness:
//   - no-global-assign (W0002): global variable assignments
package rules
