// SPDX-License-Identifier: MPL-2.0

// Package evalhost provides the eval host for dynamic Lua code execution.
package evalhost

import (
	"strings"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/compiler/ast"
	"github.com/wippyai/go-lua/compiler/parse"
	typeio "github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	luacode "github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/engine"
)

// ForbiddenClasses are module classes that cannot be used in eval'd code.
// These classes grant system-level access that eval'd code should not have.
var ForbiddenClasses = []string{
	luaapi.ClassProcess, // Process spawning, registry access
	luaapi.ClassStorage, // Filesystem, database access
	luaapi.ClassNetwork, // Network operations
}

// DefaultAllowedClasses are module classes safe for eval'd code.
// Modules with any of these classes are allowed by default.
var DefaultAllowedClasses = []string{
	luaapi.ClassDeterministic,    // Pure functions
	luaapi.ClassEncoding,         // Data encoding/decoding
	luaapi.ClassTime,             // Time operations (yields to scheduler)
	luaapi.ClassNondeterministic, // Allow if not in forbidden
}

// ModuleProvider returns available module definitions dynamically.
type ModuleProvider func() []*luaapi.ModuleDef

// Program represents a compiled Lua program.
type Program struct {
	proto   *lua.FunctionProto
	source  string
	method  string
	modules []string
}

func (p *Program) Method() string            { return p.method }
func (p *Program) Modules() []string         { return p.modules }
func (p *Program) Proto() *lua.FunctionProto { return p.proto }

// Compiler compiles Lua source with module constraints.
type Compiler struct {
	moduleProvider   ModuleProvider
	forbiddenClasses []string
	allowedClasses   []string
}

// CompilerOption configures a Compiler.
type CompilerOption func(*Compiler)

// WithForbiddenClasses sets the forbidden module classes.
func WithForbiddenClasses(classes ...string) CompilerOption {
	return func(c *Compiler) {
		c.forbiddenClasses = classes
	}
}

// NewCompiler creates a compiler with a module provider.
func NewCompiler(provider ModuleProvider, opts ...CompilerOption) *Compiler {
	c := &Compiler{
		moduleProvider:   provider,
		forbiddenClasses: ForbiddenClasses,
		allowedClasses:   DefaultAllowedClasses,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// getModules returns a map of available modules from the provider.
func (c *Compiler) getModules() map[string]*luaapi.ModuleDef {
	modules := c.moduleProvider()
	available := make(map[string]*luaapi.ModuleDef, len(modules))
	for _, m := range modules {
		available[m.Name] = m
	}
	return available
}

// Compile compiles Lua source into a Program.
func (c *Compiler) Compile(cmd CompileCmd) (*Program, error) {
	available := c.getModules()

	// Collect available names for error messages
	var availableNames []string
	for name := range available {
		availableNames = append(availableNames, name)
	}

	// Determine modules to use
	modules := cmd.Modules
	if len(modules) == 0 {
		modules = c.getDefaultModules(available, cmd.AllowClasses)
	}

	// Validate all requested modules
	for _, name := range modules {
		m, ok := available[name]
		if !ok {
			return nil, NewModuleNotAvailableErrorWithContext(name, availableNames)
		}

		if err := c.validateModuleClasses(name, m.Class, cmd.AllowClasses); err != nil {
			return nil, err
		}
	}

	chunk, err := parse.Parse(strings.NewReader(cmd.Source), "eval")
	if err != nil {
		return nil, NewParseError(err)
	}

	compileOpts, err := compileOptionsForSourceAndModules(chunk, modules, available)
	if err != nil {
		return nil, NewCompileScriptError(err)
	}
	proto, err := lua.CompileWithOptions(chunk, "eval", compileOpts)
	if err != nil {
		return nil, NewCompileScriptError(err)
	}

	return &Program{
		source:  cmd.Source,
		method:  cmd.Method,
		modules: modules,
		proto:   proto,
	}, nil
}

func compileOptionsForSourceAndModules(chunk []ast.Stmt, modules []string, available map[string]*luaapi.ModuleDef) (lua.CompileOptions, error) {
	manifest := typeio.NewManifest("eval")
	conflicts := make(map[string]struct{})
	for _, name := range modules {
		mod := available[name]
		if mod == nil || mod.Types == nil {
			continue
		}
		typesManifest := mod.Types()
		if typesManifest == nil || len(typesManifest.Types) == 0 {
			continue
		}
		for typeName, t := range typesManifest.Types {
			if typeName == "" || t == nil {
				continue
			}
			if _, blocked := conflicts[typeName]; blocked {
				continue
			}
			if existing, ok := manifest.Types[typeName]; ok {
				if typ.TypeEquals(existing, t) {
					continue
				}
				delete(manifest.Types, typeName)
				conflicts[typeName] = struct{}{}
				continue
			}
			manifest.Types[typeName] = t
		}
	}

	typeCfg := luacode.DefaultTypeCheckConfig()
	typeCfg.Enabled = true
	typeCfg.Strict = false
	checker := luacode.NewTypeChecker(typeCfg, modulesForNames(modules, available))
	sourceManifest, _ := checker.CheckParsed(chunk, "eval", nil)
	if sourceManifest != nil {
		for typeName, t := range sourceManifest.Types {
			if typeName == "" || t == nil {
				continue
			}
			if _, blocked := conflicts[typeName]; blocked {
				continue
			}
			if existing, ok := manifest.Types[typeName]; ok {
				if typ.TypeEquals(existing, t) {
					continue
				}
				delete(manifest.Types, typeName)
				conflicts[typeName] = struct{}{}
				continue
			}
			manifest.Types[typeName] = t
		}
	}

	if len(manifest.Types) == 0 {
		return lua.CompileOptions{}, nil
	}
	data, err := manifest.Encode()
	if err != nil {
		return lua.CompileOptions{}, err
	}
	typeNames := make(map[string]struct{}, len(manifest.Types))
	for name := range manifest.Types {
		typeNames[name] = struct{}{}
	}
	return lua.CompileOptions{TypeInfo: data, TypeNames: typeNames}, nil
}

func modulesForNames(names []string, available map[string]*luaapi.ModuleDef) []*luaapi.ModuleDef {
	if len(names) == 0 {
		return nil
	}
	out := make([]*luaapi.ModuleDef, 0, len(names))
	for _, name := range names {
		if mod := available[name]; mod != nil {
			out = append(out, mod)
		}
	}
	return out
}

// validateModuleClasses checks if a module's classes pass filtering.
// extraAllowed contains additional classes that are permitted for this specific call.
func (c *Compiler) validateModuleClasses(name string, classes []string, extraAllowed []string) error {
	// Check for forbidden classes (unless explicitly allowed)
	for _, class := range classes {
		if containsString(c.forbiddenClasses, class) && !containsString(extraAllowed, class) {
			return NewForbiddenClassError(name, class)
		}
	}

	// If allowed classes specified, module must have at least one
	if len(c.allowedClasses) > 0 || len(extraAllowed) > 0 {
		hasAllowed := false
		for _, class := range classes {
			if containsString(c.allowedClasses, class) || containsString(extraAllowed, class) {
				hasAllowed = true
				break
			}
		}
		if !hasAllowed && len(classes) > 0 {
			return nil // Allow modules without explicit classes
		}
	}

	return nil
}

// getDefaultModules returns all available modules that pass class filtering.
func (c *Compiler) getDefaultModules(available map[string]*luaapi.ModuleDef, extraAllowed []string) []string {
	var modules []string
	for name, m := range available {
		if c.validateModuleClasses(name, m.Class, extraAllowed) == nil {
			modules = append(modules, name)
		}
	}
	return modules
}

// containsString checks if a slice contains a string.
func containsString(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// GetModuleBinder returns a ModuleBinder that loads only the specified modules.
func (c *Compiler) GetModuleBinder(modules []string) engine.ModuleBinder {
	available := c.getModules()
	return func(l *lua.LState) error {
		for _, name := range modules {
			m, ok := available[name]
			if !ok {
				continue
			}
			l.SetGlobal(m.Name, engine.ModuleValue(m))
		}
		return nil
	}
}
