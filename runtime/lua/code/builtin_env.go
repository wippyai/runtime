// SPDX-License-Identifier: MPL-2.0

package code

import (
	"github.com/wippyai/go-lua/compiler/check/scope"
	"github.com/wippyai/go-lua/compiler/stdlib"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
	api "github.com/wippyai/runtime/api/runtime/lua"
)

// BuiltinEnvironment is what the type checker sees of the builtin modules:
// their manifests, the type namespace and the value namespace. Every checker
// the runtime builds (lint, compile-time checks, LSP) uses it, so a type
// annotation resolves the same way everywhere.
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
}

// NewBuiltinEnvironment builds the checker environment for builtin modules.
func NewBuiltinEnvironment(mods []*api.ModuleDef) *BuiltinEnvironment {
	env := &BuiltinEnvironment{
		Manifests:   make(map[string]*io.Manifest),
		Modules:     make(map[string]typ.Type),
		GlobalTypes: make(map[string]typ.Type),
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
