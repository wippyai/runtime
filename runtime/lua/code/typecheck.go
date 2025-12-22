package code

import (
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/yuin/gopher-lua/parse"
	"github.com/yuin/gopher-lua/types"
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

	diagnostics := types.CheckChunk(
		chunk,
		types.WithStdlib(),
		types.WithEnv(env),
		types.WithSource(modulePath),
	)

	// Build manifest from checked code
	manifest := types.NewManifest(modulePath)

	return manifest, diagnostics, nil
}

// IsEnabled returns whether type checking is enabled
func (tc *TypeChecker) IsEnabled() bool {
	return tc.config.Enabled
}

// IsStrict returns whether strict mode is enabled (errors vs warnings)
func (tc *TypeChecker) IsStrict() bool {
	return tc.config.Strict
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
