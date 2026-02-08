package code

import (
	"github.com/wippyai/go-lua/compiler/ast"
	"github.com/wippyai/go-lua/compiler/check"
	"github.com/wippyai/go-lua/compiler/check/hooks"
	"github.com/wippyai/go-lua/compiler/check/scope"
	"github.com/wippyai/go-lua/compiler/parse"
	"github.com/wippyai/go-lua/compiler/stdlib"
	"github.com/wippyai/go-lua/types/db"
	"github.com/wippyai/go-lua/types/diag"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/query/core"
	"github.com/wippyai/go-lua/types/typ"
	api "github.com/wippyai/runtime/api/runtime/lua"
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
	builtins         map[string]typ.Type
	builtinManifests map[string]*io.Manifest
	base             *scope.State
	globalTypes      map[string]typ.Type
	db               *db.DB
	checker          *check.Checker
	invalidateHook   func(string)
	baseHookOptions  []check.Option
	hookOptions      []check.Option
	config           TypeCheckConfig
}

// NewTypeChecker creates a configured type checker.
// Built-in modules are added as globals so they're always available.
// The Enabled flag controls whether checking runs at compile time, not initialization.
func NewTypeChecker(cfg TypeCheckConfig, builtinMods []*api.ModuleDef) *TypeChecker {
	builtins := make(map[string]typ.Type)
	manifests := make(map[string]*io.Manifest)

	for _, mod := range builtinMods {
		if mod.Types == nil {
			continue
		}
		manifest := mod.Types()
		if manifest == nil {
			continue
		}
		manifests[mod.Name] = manifest
		if manifest.Export != nil {
			builtins[mod.Name] = manifest.Export
		}
		for name, t := range manifest.AllGlobals() {
			builtins[name] = t
		}
	}

	// Build base scope for type namespace (type aliases)
	base := scope.NewWithBuiltins()

	// Build global types map for value namespace (stdlib + builtins)
	globalTypes := make(map[string]typ.Type)
	for name, t := range stdlib.Library() {
		globalTypes[name] = t
	}
	for name, t := range builtins {
		globalTypes[name] = t
	}

	database := db.New()

	// Connect builtin manifests to database
	for path, manifest := range manifests {
		database.Connect(path, manifest)
	}

	types := core.NewEngineWithStdlib(stdlib.EngineConfig())

	// Create checker with hooks
	baseHookOptions := []check.Option{
		hooks.WithAssign(),
		hooks.WithReturn(),
		hooks.WithCall(),
		hooks.WithField(),
	}
	checker := check.NewChecker(database, check.Deps{
		Types:       types,
		Stdlib:      base,
		GlobalTypes: globalTypes,
		Resolver: &core.FuncResolver{
			FieldFunc: core.Field,
			IndexFunc: core.Index,
		},
	}, baseHookOptions...)

	return &TypeChecker{
		config:           cfg,
		builtins:         builtins,
		builtinManifests: manifests,
		base:             base,
		globalTypes:      globalTypes,
		db:               database,
		checker:          checker,
		baseHookOptions:  baseHookOptions,
	}
}

// CheckParsed performs type checking on a parsed AST with provided imports
func (tc *TypeChecker) CheckParsed(chunk []ast.Stmt, entryID string, imports map[string]*io.Manifest) (*io.Manifest, []diag.Diagnostic) {
	// Connect imports to database
	for alias, manifest := range imports {
		if manifest != nil {
			tc.db.Connect(alias, manifest)
		}
	}

	// Check the chunk
	sess := tc.checker.CheckChunk(chunk, entryID)

	// Build manifest from checked code
	manifest := io.NewManifest(entryID)
	if exportType := sess.ExportType(); exportType != nil {
		manifest.SetExport(exportType)
	}
	if exportTypes := sess.ExportTypes(); exportTypes != nil {
		for name, t := range exportTypes {
			manifest.DefineType(name, t)
		}
	}

	diagnostics := sess.Diagnostics
	sess.Release()

	return manifest, diagnostics
}

// Check performs type checking on Lua source code with provided imports
func (tc *TypeChecker) Check(source, entryID string, imports map[string]*io.Manifest) (*io.Manifest, []diag.Diagnostic, error) {
	chunk, err := parse.ParseString(source, entryID)
	if err != nil {
		return nil, nil, err
	}

	manifest, diagnostics := tc.CheckParsed(chunk, entryID, imports)
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

// BuiltinManifest returns the manifest for a builtin module by name.
func (tc *TypeChecker) BuiltinManifest(name string) *io.Manifest {
	if tc.builtinManifests == nil {
		return nil
	}
	return tc.builtinManifests[name]
}

// AddBuiltin adds a module to the type checker's built-in environment
func (tc *TypeChecker) AddBuiltin(mod *api.ModuleDef) {
	if tc.builtins == nil || mod == nil || mod.Types == nil {
		return
	}
	manifest := mod.Types()
	if manifest != nil {
		tc.builtinManifests[mod.Name] = manifest
		tc.db.Connect(mod.Name, manifest)
		if manifest.Export != nil {
			tc.builtins[mod.Name] = manifest.Export
			tc.globalTypes[mod.Name] = manifest.Export
		}
		for name, t := range manifest.AllGlobals() {
			tc.builtins[name] = t
			tc.globalTypes[name] = t
		}
	}
}

// AddBuiltinManifest adds a module manifest to the type checker's built-in environment.
func (tc *TypeChecker) AddBuiltinManifest(name string, manifest *io.Manifest) {
	if tc.builtins == nil || name == "" || manifest == nil {
		return
	}
	tc.builtinManifests[name] = manifest
	tc.db.Connect(name, manifest)
	if manifest.Export != nil {
		tc.builtins[name] = manifest.Export
		tc.globalTypes[name] = manifest.Export
	}
	for gname, t := range manifest.AllGlobals() {
		tc.builtins[gname] = t
		tc.globalTypes[gname] = t
	}
}

// BuildEnv creates an environment with all builtin modules
func (tc *TypeChecker) BuildEnv() *scope.State {
	return tc.base
}

// GlobalTypes returns the map of global symbol names to their types.
// This includes stdlib functions and builtin module exports.
func (tc *TypeChecker) GlobalTypes() map[string]typ.Type {
	return tc.globalTypes
}

// Clone creates a copy of the TypeChecker for parallel use.
// Each clone has its own db.DB for concurrent type checking.
func (tc *TypeChecker) Clone() *TypeChecker {
	database := db.New()
	types := core.NewEngineWithStdlib(stdlib.EngineConfig())

	// Connect builtin manifests to new database
	for path, manifest := range tc.builtinManifests {
		database.Connect(path, manifest)
	}

	// Create checker with hooks
	baseHookOptions := tc.baseHookOptions
	if len(baseHookOptions) == 0 {
		baseHookOptions = []check.Option{
			hooks.WithAssign(),
			hooks.WithReturn(),
			hooks.WithCall(),
			hooks.WithField(),
		}
	}
	allHookOptions := append(append([]check.Option{}, baseHookOptions...), tc.hookOptions...)
	checker := check.NewChecker(database, check.Deps{
		Types:       types,
		Stdlib:      tc.base,
		GlobalTypes: tc.globalTypes,
		Resolver: &core.FuncResolver{
			FieldFunc: core.Field,
			IndexFunc: core.Index,
		},
	}, allHookOptions...)

	return &TypeChecker{
		config:           tc.config,
		builtins:         tc.builtins,
		builtinManifests: tc.builtinManifests,
		base:             tc.base,
		globalTypes:      tc.globalTypes,
		db:               database,
		checker:          checker,
		baseHookOptions:  baseHookOptions,
		hookOptions:      append([]check.Option{}, tc.hookOptions...),
		invalidateHook:   tc.invalidateHook,
	}
}

// WithConfig creates a copy with a different configuration.
// Used by the linter to enable checking with custom settings.
func (tc *TypeChecker) WithConfig(cfg TypeCheckConfig) *TypeChecker {
	database := db.New()
	types := core.NewEngineWithStdlib(stdlib.EngineConfig())

	// Connect builtin manifests to new database
	for path, manifest := range tc.builtinManifests {
		database.Connect(path, manifest)
	}

	// Create checker with hooks
	baseHookOptions := tc.baseHookOptions
	if len(baseHookOptions) == 0 {
		baseHookOptions = []check.Option{
			hooks.WithAssign(),
			hooks.WithReturn(),
			hooks.WithCall(),
			hooks.WithField(),
		}
	}
	allHookOptions := append(append([]check.Option{}, baseHookOptions...), tc.hookOptions...)
	checker := check.NewChecker(database, check.Deps{
		Types:       types,
		Stdlib:      tc.base,
		GlobalTypes: tc.globalTypes,
		Resolver: &core.FuncResolver{
			FieldFunc: core.Field,
			IndexFunc: core.Index,
		},
	}, allHookOptions...)

	return &TypeChecker{
		config:           cfg,
		builtins:         tc.builtins,
		builtinManifests: tc.builtinManifests,
		base:             tc.base,
		globalTypes:      tc.globalTypes,
		db:               database,
		checker:          checker,
		baseHookOptions:  baseHookOptions,
		hookOptions:      append([]check.Option{}, tc.hookOptions...),
		invalidateHook:   tc.invalidateHook,
	}
}

// ClearCache removes memoized results from the type checker.
// Use for batch operations where memoization between files isn't needed.
func (tc *TypeChecker) ClearCache() {
	if tc == nil || tc.checker == nil {
		return
	}
	tc.checker.ClearCache()
}

// AddHookOptions appends check hooks and rebuilds the checker.
func (tc *TypeChecker) AddHookOptions(opts ...check.Option) {
	if tc == nil || len(opts) == 0 {
		return
	}
	tc.hookOptions = append(tc.hookOptions, opts...)
	tc.rebuildChecker()
}

// ClearHookOptions removes extra hooks and rebuilds the checker.
func (tc *TypeChecker) ClearHookOptions() {
	if tc == nil {
		return
	}
	tc.hookOptions = nil
	tc.rebuildChecker()
}

// SetInvalidateHook registers a callback for external cache invalidation.
// Currently stored for LSP use; the callback is not invoked by this checker.
func (tc *TypeChecker) SetInvalidateHook(fn func(string)) {
	if tc == nil {
		return
	}
	tc.invalidateHook = fn
}

func (tc *TypeChecker) rebuildChecker() {
	if tc == nil || tc.db == nil {
		return
	}
	types := core.NewEngineWithStdlib(stdlib.EngineConfig())
	baseHookOptions := tc.baseHookOptions
	if len(baseHookOptions) == 0 {
		baseHookOptions = []check.Option{
			hooks.WithAssign(),
			hooks.WithReturn(),
			hooks.WithCall(),
			hooks.WithField(),
		}
		tc.baseHookOptions = baseHookOptions
	}
	allHookOptions := append(append([]check.Option{}, baseHookOptions...), tc.hookOptions...)
	tc.checker = check.NewChecker(tc.db, check.Deps{
		Types:       types,
		Stdlib:      tc.base,
		GlobalTypes: tc.globalTypes,
		Resolver: &core.FuncResolver{
			FieldFunc: core.Field,
			IndexFunc: core.Index,
		},
	}, allHookOptions...)
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
