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
//	        DiagCode:        lint.LintCodeBase + 99,
//	        Title:           "short description",
//	        Description:     "longer explanation",
//	        DefaultSeverity: lint.SeverityWarning,
//	        Category:        "style",
//	    }
//	}
//
//	func (r *MyRule) Check(ctx *lint.Context) {
//	    // Analyze ctx.AST and call ctx.Reportf() for issues
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
//   - ctx.CFG: control flow graph with bindings
//   - ctx.Reportf(node, severity, message, args...): emit diagnostic
//   - ctx.LookupSymbol(name): check if name is in scope
//
// Built-in rules:
//
// Style:
//   - no-empty-blocks  (W0001): empty control flow blocks
//   - no-unused-params (W0005): unused function parameters
//   - no-shadowed-vars (W0007): variable shadowing outer scope
//
// Correctness:
//   - no-global-assign  (W0002): global variable assignments
//   - no-self-compare   (W0003): self-comparison (always true/false)
//   - no-unused-vars    (W0004): unused local variables
//   - no-unused-imports (W0006): unused module imports
package rules
