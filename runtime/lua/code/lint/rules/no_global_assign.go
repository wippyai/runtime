package rules

import (
	"github.com/wippyai/runtime/runtime/lua/code/lint"
	"github.com/wippyai/go-lua/compiler/cfg"
)

func init() {
	lint.Register(&NoGlobalAssign{})
}

// NoGlobalAssign detects assignments to undeclared variables (global escapes).
type NoGlobalAssign struct{}

func (r *NoGlobalAssign) Meta() lint.RuleMeta {
	return lint.RuleMeta{
		Name:            "no-global-assign",
		Code:            "W0002",
		DiagCode:        lint.LintW0002,
		Title:           "global variable assignment",
		Description:     "Assignments should use local variables; global assignments may cause unintended side effects",
		DefaultSeverity: lint.SeverityWarning,
		Category:        "correctness",
	}
}

func (r *NoGlobalAssign) Check(ctx *lint.Context) {
	if ctx.CFG == nil {
		return
	}

	// Check variable assignments
	ctx.CFG.EachAssign(func(_ cfg.Point, info *cfg.AssignInfo) {
		if info.IsLocal {
			return
		}

		for _, target := range info.Targets {
			if target.Kind != cfg.TargetIdent {
				continue
			}

			// Skip builtins
			if _, isBuiltin := ctx.LookupSymbol(target.Name); isBuiltin {
				continue
			}

			// Symbol kind check: SymbolGlobal means it was never declared locally
			if kind, ok := ctx.CFG.SymbolKind(target.Symbol); ok && kind != cfg.SymbolGlobal {
				continue
			}

			ctx.Reportf(target.Expr, lint.SeverityWarning,
				"assignment to global variable '%s'; consider using 'local'", target.Name)
		}
	})

	// Check global function definitions (function foo() ... end)
	ctx.CFG.EachFuncDef(func(_ cfg.Point, info *cfg.FuncDefInfo) {
		if info.TargetKind != cfg.FuncDefGlobal {
			return
		}

		// Skip builtins
		if _, isBuiltin := ctx.LookupSymbol(info.Name); isBuiltin {
			return
		}

		ctx.Reportf(info.FuncExpr, lint.SeverityWarning,
			"function '%s' declared as global; consider using 'local function'", info.Name)
	})
}
