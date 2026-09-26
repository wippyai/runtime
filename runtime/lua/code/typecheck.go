// SPDX-License-Identifier: MPL-2.0

package code

import (
	"sync"

	"github.com/wippyai/go-lua/compiler/ast"
	"github.com/wippyai/go-lua/compiler/check"
	"github.com/wippyai/go-lua/compiler/check/hooks"
	"github.com/wippyai/go-lua/compiler/check/scope"
	"github.com/wippyai/go-lua/compiler/parse"
	"github.com/wippyai/go-lua/types/db"
	"github.com/wippyai/go-lua/types/diag"
	"github.com/wippyai/go-lua/types/io"
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

	// Check is the type-checking semantics (lua.type_system.strict_any and
	// the options that follow it).
	Check check.Options
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
	env             *BuiltinEnvironment
	db              *db.DB
	checker         *check.Checker
	invalidateHook  func(string)
	baseHookOptions []check.Option
	hookOptions     []check.Option
	config          TypeCheckConfig
	checkMu         sync.Mutex
}

// defaultHookOptions are the diagnostic passes every runtime checker runs.
func defaultHookOptions() []check.Option {
	return []check.Option{
		hooks.WithAssign(),
		hooks.WithReturn(),
		hooks.WithCall(),
		hooks.WithField(),
	}
}

// NewTypeChecker creates a configured type checker.
// Built-in modules are added as globals so they're always available.
// The Enabled flag controls whether checking runs at compile time, not initialization.
func NewTypeChecker(cfg TypeCheckConfig, builtinMods []*api.ModuleDef) *TypeChecker {
	env := NewBuiltinEnvironment(builtinMods, cfg.Check)
	tc := &TypeChecker{
		env:             env,
		config:          cfg,
		db:              env.NewDatabase(),
		baseHookOptions: defaultHookOptions(),
	}
	tc.checker = tc.newChecker(tc.db)
	return tc
}

// newChecker builds a checker over database with the checker's environment
// and hooks.
func (tc *TypeChecker) newChecker(database *db.DB) *check.Checker {
	hooks := append(append([]check.Option{}, tc.baseHookOptions...), tc.hookOptions...)
	return tc.env.NewChecker(database, hooks...)
}

// CheckParsed performs type checking on a parsed AST with provided imports
func (tc *TypeChecker) CheckParsed(chunk []ast.Stmt, entryID string, imports map[string]*io.Manifest) (*io.Manifest, []diag.Diagnostic) {
	tc.checkMu.Lock()
	defer tc.checkMu.Unlock()

	previous := make(map[string]*io.Manifest, len(imports))
	// Connect imports to database
	for alias, manifest := range imports {
		previous[alias] = tc.db.Manifest(alias)
		if manifest != nil {
			tc.db.Connect(alias, manifest)
		}
	}
	defer func() {
		for alias, manifest := range previous {
			if manifest == nil {
				tc.db.Disconnect(alias)
				continue
			}
			tc.db.Connect(alias, manifest)
		}
	}()

	// Check the chunk
	sess := tc.checker.CheckChunk(chunk, entryID)

	// Build manifest from checked code via canonical checker export path.
	manifest := sess.ExportManifest(entryID)

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
	if tc.env == nil {
		return nil
	}
	return tc.env.Manifests[name]
}

// AddBuiltin adds a module to the type checker's built-in environment
func (tc *TypeChecker) AddBuiltin(mod *api.ModuleDef) {
	if tc.env == nil || mod == nil || mod.Types == nil {
		return
	}
	if manifest := mod.Types(); manifest != nil {
		tc.AddBuiltinManifest(mod.Name, manifest)
	}
}

// AddBuiltinManifest adds a module manifest to the type checker's built-in environment.
func (tc *TypeChecker) AddBuiltinManifest(name string, manifest *io.Manifest) {
	if tc.env == nil || name == "" || manifest == nil {
		return
	}
	tc.env.Manifests[name] = manifest
	tc.db.Connect(name, manifest)
	if manifest.Export != nil {
		tc.env.Modules[name] = manifest.Export
		tc.env.GlobalTypes[name] = manifest.Export
	}
	for gname, t := range manifest.AllGlobals() {
		tc.env.Modules[gname] = t
		tc.env.GlobalTypes[gname] = t
	}
}

// BuildEnv creates an environment with all builtin modules
func (tc *TypeChecker) BuildEnv() *scope.State {
	return tc.env.TypeScope
}

// GlobalTypes returns the map of global symbol names to their types.
// This includes stdlib functions and builtin module exports.
func (tc *TypeChecker) GlobalTypes() map[string]typ.Type {
	return tc.env.GlobalTypes
}

// Clone creates a copy of the TypeChecker for parallel use.
// Each clone has its own db.DB for concurrent type checking.
func (tc *TypeChecker) Clone() *TypeChecker {
	return tc.WithConfig(tc.config)
}

// WithConfig creates a copy with a different configuration and its own
// db.DB. Used by the linter to enable checking with custom settings.
func (tc *TypeChecker) WithConfig(cfg TypeCheckConfig) *TypeChecker {
	env := *tc.env
	env.Options = cfg.Check
	clone := &TypeChecker{
		env:             &env,
		config:          cfg,
		db:              env.NewDatabase(),
		baseHookOptions: tc.baseHookOptions,
		hookOptions:     append([]check.Option{}, tc.hookOptions...),
		invalidateHook:  tc.invalidateHook,
	}
	clone.checker = clone.newChecker(clone.db)
	return clone
}

// ClearCache removes memoized results from the type checker.
// Use for batch operations where memoization between files isn't needed.
func (tc *TypeChecker) ClearCache() {
	if tc == nil || tc.checker == nil {
		return
	}
	tc.checkMu.Lock()
	defer tc.checkMu.Unlock()
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
	tc.checker = tc.newChecker(tc.db)
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
