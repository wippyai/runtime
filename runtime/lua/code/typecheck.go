package code

// TODO: Uncomment when gopher-lua/types/checker is available
// import (
// 	api "github.com/wippyai/runtime/api/runtime/lua"
// 	"github.com/yuin/gopher-lua/types"
// 	"github.com/yuin/gopher-lua/types/checker"
// 	"github.com/yuin/gopher-lua/types/rules"
// )

// TypeCheckConfig configures the type checking system
type TypeCheckConfig struct {
	// Enabled enables type checking during compilation
	Enabled bool

	// Strict treats type errors as compile errors (vs warnings)
	Strict bool

	// RequireAnnotations requires type annotations on all declarations
	RequireAnnotations bool

	// SkipUntyped silently skips type checking for untyped code
	SkipUntyped bool

	// Rules controls which type checking rules are enabled
	Rules TypeCheckRules
}

// TypeCheckRules configures individual type checking rules
type TypeCheckRules struct {
	TypeCheck     bool // type mismatch validation
	NilCheck      bool // nil dereference detection
	Unused        bool // unused variables
	Unreachable   bool // unreachable code
	Exhaustive    bool // exhaustive pattern matching
	Readonly      bool // readonly violations
	Undefined     bool // undefined variables
	MissingReturn bool // missing return statements
}

// DefaultTypeCheckConfig returns the default type check configuration
func DefaultTypeCheckConfig() TypeCheckConfig {
	return TypeCheckConfig{
		Enabled:            false, // disabled until checker package is available
		Strict:             true,
		RequireAnnotations: false,
		SkipUntyped:        true,
		Rules: TypeCheckRules{
			TypeCheck:     true,
			NilCheck:      true,
			Unused:        false,
			Unreachable:   false,
			Exhaustive:    false,
			Readonly:      true,
			Undefined:     true,
			MissingReturn: true,
		},
	}
}

// TODO: Uncomment when gopher-lua/types/checker is available
// TypeChecker wraps the go-lua type checker with wippy configuration
// type TypeChecker struct {
// 	checker *checker.Checker
// 	config  TypeCheckConfig
// }

// NewTypeChecker creates a configured type checker.
// Built-in modules are added as globals so they're always available.
// func NewTypeChecker(cfg TypeCheckConfig, builtins []api.Module) *TypeChecker {
// 	checkerCfg := checker.Config{
// 		Strict:             cfg.Strict,
// 		RequireAnnotations: cfg.RequireAnnotations,
// 		SkipUntyped:        cfg.SkipUntyped,
// 	}

// 	chk := checker.NewWithConfig(checkerCfg)
// 	chk.SetGlobals(types.StandardLibrary())

// 	// Add built-in module types as globals
// 	for _, mod := range builtins {
// 		if def, ok := mod.(*api.ModuleDef); ok && def.Types != nil {
// 			manifest := def.Types()
// 			if manifest != nil && manifest.Export != nil {
// 				chk.SetGlobal(def.Name, manifest.Export)
// 			}
// 		}
// 	}

// 	// Add enabled rules
// 	if cfg.Rules.TypeCheck {
// 		chk.AddRule(rules.NewTypeCheckRule())
// 	}
// 	if cfg.Rules.NilCheck {
// 		chk.AddRule(rules.NewNilCheckRule())
// 	}
// 	if cfg.Rules.Unused {
// 		chk.AddRule(rules.NewUnusedRule())
// 	}
// 	if cfg.Rules.Unreachable {
// 		chk.AddRule(rules.NewUnreachableRule())
// 	}
// 	if cfg.Rules.Exhaustive {
// 		chk.AddRule(rules.NewExhaustivenessRule())
// 	}
// 	if cfg.Rules.Readonly {
// 		chk.AddRule(rules.NewReadonlyRule())
// 	}
// 	if cfg.Rules.Undefined {
// 		chk.AddRule(rules.NewUndefinedRule())
// 	}
// 	if cfg.Rules.MissingReturn {
// 		chk.AddRule(rules.NewMissingReturnRule())
// 	}

// 	return &TypeChecker{
// 		checker: chk,
// 		config:  cfg,
// 	}
// }

// Check performs type checking on Lua source code with provided imports
// func (tc *TypeChecker) Check(source, modulePath string, imports map[string]*types.TypeManifest) (*types.TypeManifest, *types.ErrorList, error) {
// 	return tc.checker.CheckStringWithImports(source, modulePath, imports)
// }

// IsEnabled returns whether type checking is enabled
// func (tc *TypeChecker) IsEnabled() bool {
// 	return tc.config.Enabled
// }

// IsStrict returns whether strict mode is enabled (errors vs warnings)
// func (tc *TypeChecker) IsStrict() bool {
// 	return tc.config.Strict
// }
