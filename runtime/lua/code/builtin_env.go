// SPDX-License-Identifier: MPL-2.0

package code

import (
	"github.com/wippyai/go-lua/compiler/check"
	"github.com/wippyai/go-lua/compiler/check/scope"
	"github.com/wippyai/go-lua/compiler/stdlib"
	"github.com/wippyai/go-lua/types/db"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/query/core"
	"github.com/wippyai/go-lua/types/typ"
	api "github.com/wippyai/runtime/api/runtime/lua"
)

// BuiltinEnvironment is what the type checker sees of the builtin modules:
// their manifests, the type namespace and the value namespace, together with
// the type-checking semantics configured for the runtime. Every checker the
// runtime builds (lint, compile-time checks, LSP) comes from it, so a type
// annotation resolves and a value is checked the same way everywhere.
type BuiltinEnvironment struct {
	// Manifests maps module names to their type manifests.
	Manifests map[string]*io.Manifest
	// Modules maps module names to their exported types (the module globals).
	Modules map[string]typ.Type
	// TypeScope binds the builtin type names and the types builtin modules
	// define, unqualified where modules agree on a name.
	TypeScope *scope.State
	// GlobalTypes is the value namespace: stdlib globals, module globals and
	// globals the manifests declare.
	GlobalTypes map[string]typ.Type
	// Options is the type-checking semantics (lua.type_system settings).
	Options check.Options
}

// NewBuiltinEnvironment builds the checker environment for builtin modules
// under the given type-checking semantics.
func NewBuiltinEnvironment(mods []*api.ModuleDef, options check.Options) *BuiltinEnvironment {
	env := &BuiltinEnvironment{
		Manifests:   make(map[string]*io.Manifest),
		Modules:     make(map[string]typ.Type),
		GlobalTypes: make(map[string]typ.Type),
		Options:     options,
	}
	manifests := make([]*io.Manifest, 0, len(mods))
	for _, mod := range mods {
		if mod == nil || mod.Types == nil || mod.Name == "" {
			continue
		}
		manifest := mod.Types()
		if manifest == nil {
			continue
		}
		env.Manifests[mod.Name] = manifest
		manifests = append(manifests, manifest)
		if manifest.Export != nil {
			env.Modules[mod.Name] = manifest.Export
		}
		for name, t := range manifest.AllGlobals() {
			env.Modules[name] = t
		}
	}

	env.TypeScope = scope.NewWithBuiltins().WithModuleTypes(manifests)
	for name, t := range stdlib.Library() {
		env.GlobalTypes[name] = t
	}
	for name, t := range env.Modules {
		env.GlobalTypes[name] = t
	}
	return env
}

// NewDatabase returns a query database with the builtin manifests connected.
func (env *BuiltinEnvironment) NewDatabase() *db.DB {
	database := db.New()
	for path, manifest := range env.Manifests {
		database.Connect(path, manifest)
	}
	return database
}

// NewChecker builds a checker over database that sees the environment's type
// and value namespaces, applies its type-checking semantics and runs hooks.
func (env *BuiltinEnvironment) NewChecker(database *db.DB, hooks ...check.Option) *check.Checker {
	opts := append([]check.Option{check.WithOptions(env.Options)}, hooks...)
	return check.NewChecker(database, check.Deps{
		Types:       core.NewEngineWithStdlib(stdlib.EngineConfig()),
		Stdlib:      env.TypeScope,
		GlobalTypes: env.GlobalTypes,
		Resolver: &core.FuncResolver{
			FieldFunc: core.Field,
			IndexFunc: core.Index,
		},
	}, opts...)
}
