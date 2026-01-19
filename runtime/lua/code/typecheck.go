package code

import (
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/yuin/gopher-lua/compiler/ast"
	"github.com/yuin/gopher-lua/compiler/check"
	"github.com/yuin/gopher-lua/compiler/parse"
	"github.com/yuin/gopher-lua/compiler/stdlib"
	"github.com/yuin/gopher-lua/types/db"
	"github.com/yuin/gopher-lua/types/diag"
	"github.com/yuin/gopher-lua/types/io"
	"github.com/yuin/gopher-lua/types/scope"
	"github.com/yuin/gopher-lua/types/typ"
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
	config           TypeCheckConfig
	builtins         map[string]typ.Type
	builtinManifests map[string]*io.Manifest
	base             *scope.State
	db               *db.DB
}

// NewTypeChecker creates a configured type checker.
// Built-in modules are added as globals so they're always available.
// The Enabled flag controls whether checking runs at compile time, not initialization.
func NewTypeChecker(cfg TypeCheckConfig, builtinMods []*api.ModuleDef) *TypeChecker {
	builtins := make(map[string]typ.Type)
	manifests := make(map[string]*io.Manifest)

	for _, mod := range builtinMods {
		if mod.Types != nil {
			manifest := mod.Types()
			if manifest != nil {
				manifests[mod.Name] = manifest
				if manifest.Export != nil {
					builtins[mod.Name] = manifest.Export
				}
			}
		}
	}

	// Build base scope with stdlib and builtins
	base := scope.New()

	// Register builtin types for type casting (string(), integer(), etc.)
	base = base.WithType("any", typ.Any)
	base = base.WithType("nil", typ.Nil)
	base = base.WithType("boolean", typ.Boolean)
	base = base.WithType("bool", typ.Boolean)
	base = base.WithType("number", typ.Number)
	base = base.WithType("integer", typ.Integer)
	base = base.WithType("int", typ.Integer)
	base = base.WithType("string", typ.String)
	base = base.WithType("table", typ.NewRecord().Build())

	for name, t := range stdlib.Library() {
		base = base.WithSymbol(name, t)
	}
	for name, t := range builtins {
		base = base.WithSymbol(name, t)
	}

	return &TypeChecker{
		config:           cfg,
		builtins:         builtins,
		builtinManifests: manifests,
		base:             base,
		db:               db.New(),
	}
}

// CheckParsed performs type checking on a parsed AST with provided imports
func (tc *TypeChecker) CheckParsed(chunk []ast.Stmt, entryID string, imports map[string]*io.Manifest) (*io.Manifest, []diag.Diagnostic) {
	// Build scope with imports
	s := tc.base
	for alias, manifest := range imports {
		if manifest != nil && manifest.Export != nil {
			s = s.WithSymbol(alias, manifest.Export)
		}
		if manifest != nil {
			// Add exported types
			for typeName, t := range manifest.Types {
				s = s.WithType(typeName, t)
			}
			// Add globals from dependencies
			for name, t := range manifest.Globals {
				s = s.WithSymbol(name, t)
			}
		}
	}

	// Create checker with context
	ctx := db.NewContext(tc.db)
	checker := check.New(ctx)
	checker.SetSourceName(entryID)
	checker.SetBaseScope(s)
	checker.SetImports(tc.builtinManifests)

	// Check the chunk
	diagnostics := checker.Check(chunk)

	// Build manifest from checked code
	manifest := io.NewManifest(entryID)
	if exportType := checker.ExportType(); exportType != nil {
		manifest.SetExport(exportType)
	}

	return manifest, diagnostics
}

// Check performs type checking on Lua source code with provided imports
func (tc *TypeChecker) Check(source, entryID string, imports map[string]*io.Manifest) (*io.Manifest, []diag.Diagnostic, error) {
	chunk, err := parse.ParseString(source, entryID)
	if err != nil {
		return nil, nil, err
	}

	// Build scope with imports
	s := tc.base
	for alias, manifest := range imports {
		if manifest != nil && manifest.Export != nil {
			s = s.WithSymbol(alias, manifest.Export)
		}
		if manifest != nil {
			// Add exported types
			for typeName, t := range manifest.Types {
				s = s.WithType(typeName, t)
			}
			// Add globals from dependencies
			for name, t := range manifest.Globals {
				s = s.WithSymbol(name, t)
			}
		}
	}

	// Create checker with context
	ctx := db.NewContext(tc.db)
	checker := check.New(ctx)
	checker.SetSourceName(entryID)
	checker.SetBaseScope(s)
	checker.SetImports(tc.builtinManifests)

	// Check the chunk
	diagnostics := checker.Check(chunk)

	// Build manifest from checked code
	manifest := io.NewManifest(entryID)
	if exportType := checker.ExportType(); exportType != nil {
		manifest.SetExport(exportType)
	}

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

// AddBuiltin adds a module to the type checker's built-in environment
func (tc *TypeChecker) AddBuiltin(mod *api.ModuleDef) {
	if tc.builtins == nil || mod == nil || mod.Types == nil {
		return
	}
	manifest := mod.Types()
	if manifest != nil {
		tc.builtinManifests[mod.Name] = manifest
		if manifest.Export != nil {
			tc.builtins[mod.Name] = manifest.Export
			tc.base = tc.base.WithSymbol(mod.Name, manifest.Export)
		}
	}
}

// AddBuiltinManifest adds a module manifest to the type checker's built-in environment.
func (tc *TypeChecker) AddBuiltinManifest(name string, manifest *io.Manifest) {
	if tc.builtins == nil || name == "" || manifest == nil {
		return
	}
	tc.builtinManifests[name] = manifest
	if manifest.Export != nil {
		tc.builtins[name] = manifest.Export
		tc.base = tc.base.WithSymbol(name, manifest.Export)
	}
}

// BuildEnv creates an environment with all builtin modules
func (tc *TypeChecker) BuildEnv() *scope.State {
	return tc.base
}

// Clone creates a copy of the TypeChecker for parallel use.
// Each clone has its own db.DB for concurrent type checking.
func (tc *TypeChecker) Clone() *TypeChecker {
	return &TypeChecker{
		config:           tc.config,
		builtins:         tc.builtins,
		builtinManifests: tc.builtinManifests,
		base:             tc.base,
		db:               db.New(),
	}
}

// WithConfig creates a copy with a different configuration.
// Used by the linter to enable checking with custom settings.
func (tc *TypeChecker) WithConfig(cfg TypeCheckConfig) *TypeChecker {
	return &TypeChecker{
		config:           cfg,
		builtins:         tc.builtins,
		builtinManifests: tc.builtinManifests,
		base:             tc.base,
		db:               db.New(),
	}
}

// HasErrors checks if any diagnostic is an error
func HasErrors(diagnostics []diag.Diagnostic) bool {
	for _, d := range diagnostics {
		if d.Severity == diag.SeverityError {
			return true
		}
	}
	return false
}

// FilterErrors returns only error-level diagnostics
func FilterErrors(diagnostics []diag.Diagnostic) []diag.Diagnostic {
	var errors []diag.Diagnostic
	for _, d := range diagnostics {
		if d.Severity == diag.SeverityError {
			errors = append(errors, d)
		}
	}
	return errors
}
