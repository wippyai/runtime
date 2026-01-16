package code

import (
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/yuin/gopher-lua/compiler/ast"
	"github.com/yuin/gopher-lua/compiler/parse"
	"github.com/yuin/gopher-lua/types"
	"github.com/yuin/gopher-lua/types/subtype"

	_ "github.com/yuin/gopher-lua/types/stmt"  // register statement handlers
	_ "github.com/yuin/gopher-lua/types/synth" // register synthesizers
)

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

	// DisableCache disables the subtype cache (useful for debugging)
	DisableCache bool

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
		Enabled:            false,
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

// TypeChecker wraps the go-lua type checker with wippy configuration
type TypeChecker struct {
	config   TypeCheckConfig
	builtins map[string]types.Type
}

// NewTypeChecker creates a configured type checker.
// Built-in modules are added as globals so they're always available.
func NewTypeChecker(cfg TypeCheckConfig, builtinMods []*api.ModuleDef) *TypeChecker {
	// Apply cache setting
	if cfg.DisableCache {
		subtype.SetCacheEnabled(false)
	}

	// Skip building builtins map if type checking is disabled
	if !cfg.Enabled {
		return &TypeChecker{
			config:   cfg,
			builtins: nil,
		}
	}

	builtins := make(map[string]types.Type)

	for _, mod := range builtinMods {
		if mod.Types != nil {
			manifest := mod.Types()
			if manifest != nil && manifest.Export != nil {
				builtins[mod.Name] = manifest.Export
			}
		}
	}

	return &TypeChecker{
		config:   cfg,
		builtins: builtins,
	}
}

// ClearCache clears the subtype cache between sessions.
// Call this when types may have been reallocated.
func ClearTypeCache() {
	subtype.ClearCache()
}

// SetTypeCacheEnabled enables or disables the subtype cache.
func SetTypeCacheEnabled(enabled bool) {
	subtype.SetCacheEnabled(enabled)
}

// Check performs type checking on Lua source code with provided imports
func (tc *TypeChecker) Check(source, modulePath string, imports map[string]*types.TypeManifest) (*types.TypeManifest, []types.Diagnostic, error) {
	chunk, err := parse.ParseString(source, modulePath)
	if err != nil {
		return nil, nil, err
	}

	env := types.NewEnv()

	// Add built-in module types
	for name, t := range tc.builtins {
		env = env.WithSymbol(name, t)
	}

	// Add imports from dependencies
	for alias, manifest := range imports {
		if manifest != nil && manifest.Export != nil {
			env = env.WithSymbol(alias, manifest.Export)
		}
		// Also add exported types
		if manifest != nil {
			for typeName, t := range manifest.ExportedTypes {
				env = env.WithType(typeName, t)
			}
		}
	}

	// Use CheckChunkWithContext to get context for extracting module type
	result := types.CheckChunkWithContext(
		chunk,
		types.WithStdlib(),
		types.WithEnv(env),
		types.WithSource(modulePath),
	)
	// Release session resources after type checking
	defer result.Context.Release()

	// Build manifest from checked code
	manifest := types.NewManifest(modulePath)

	// Extract module return type from the last return statement
	if result.Context != nil {
		exportType := extractModuleReturnType(chunk, result.Context)
		if exportType != nil {
			manifest.SetExport(exportType)
		}
	}

	return manifest, result.Diagnostics, nil
}

// IsEnabled returns whether type checking is enabled
func (tc *TypeChecker) IsEnabled() bool {
	return tc.config.Enabled
}

// IsStrict returns whether strict mode is enabled (errors vs warnings)
func (tc *TypeChecker) IsStrict() bool {
	return tc.config.Strict
}

// AddBuiltin adds a module to the type checker's built-in environment
func (tc *TypeChecker) AddBuiltin(mod *api.ModuleDef) {
	if tc.builtins == nil || mod == nil || mod.Types == nil {
		return
	}
	manifest := mod.Types()
	if manifest != nil && manifest.Export != nil {
		tc.builtins[mod.Name] = manifest.Export
	}
}

// BuildEnv creates an environment with all builtin modules
func (tc *TypeChecker) BuildEnv() *types.Env {
	env := types.NewEnv()
	for name, t := range tc.builtins {
		env = env.WithSymbol(name, t)
	}
	return env
}

// HasErrors checks if any diagnostic is an error
func HasErrors(diagnostics []types.Diagnostic) bool {
	for _, d := range diagnostics {
		if d.Severity == types.SeverityError {
			return true
		}
	}
	return false
}

// FilterErrors returns only error-level diagnostics
func FilterErrors(diagnostics []types.Diagnostic) []types.Diagnostic {
	var errors []types.Diagnostic
	for _, d := range diagnostics {
		if d.Severity == types.SeverityError {
			errors = append(errors, d)
		}
	}
	return errors
}

// extractModuleReturnType finds the module's return statement and extracts its type.
// For modules like: local M = {}; function M.foo() end; return M
// This extracts the type of M as the module's export type.
func extractModuleReturnType(chunk []ast.Stmt, ctx *types.Context) types.Type {
	// Find return statements at module level
	for i := len(chunk) - 1; i >= 0; i-- {
		if ret, ok := chunk[i].(*ast.ReturnStmt); ok {
			if len(ret.Exprs) == 0 {
				return nil
			}
			// Synthesize the type of the first return expression
			typ, _ := ctx.Synth(ret.Exprs[0])
			return typ
		}
	}
	return nil
}
