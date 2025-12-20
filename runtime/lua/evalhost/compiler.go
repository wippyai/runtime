// Package evalhost provides the eval host for dynamic Lua code execution.
package evalhost

import (
	"strings"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/parse"
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

// Program represents a compiled Lua program.
type Program struct {
	source  string
	method  string
	modules []string
	proto   *lua.FunctionProto
}

func (p *Program) Method() string            { return p.method }
func (p *Program) Modules() []string         { return p.modules }
func (p *Program) Proto() *lua.FunctionProto { return p.proto }

// Compiler compiles Lua source with module constraints.
type Compiler struct {
	availableModules map[string]*luaapi.ModuleDef
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

// NewCompiler creates a compiler with available modules.
func NewCompiler(modules []*luaapi.ModuleDef, opts ...CompilerOption) *Compiler {
	available := make(map[string]*luaapi.ModuleDef)
	for _, m := range modules {
		available[m.Name] = m
	}
	c := &Compiler{
		availableModules: available,
		forbiddenClasses: ForbiddenClasses,
		allowedClasses:   DefaultAllowedClasses,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Compile compiles Lua source into a Program.
func (c *Compiler) Compile(cmd CompileCmd) (*Program, error) {
	// Determine modules to use
	modules := cmd.Modules
	if len(modules) == 0 {
		// Use all available modules that pass class filtering
		modules = c.getDefaultModules()
	}

	// Validate all requested modules
	for _, name := range modules {
		m, ok := c.availableModules[name]
		if !ok {
			return nil, NewModuleNotAvailableError(name)
		}

		// Check class-based restrictions
		if err := c.validateModuleClasses(name, m.Class); err != nil {
			return nil, err
		}
	}

	// Parse and compile
	chunk, err := parse.Parse(strings.NewReader(cmd.Source), "eval")
	if err != nil {
		return nil, NewParseError(err)
	}

	proto, err := lua.Compile(chunk, "eval")
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

// validateModuleClasses checks if a module's classes pass filtering.
func (c *Compiler) validateModuleClasses(name string, classes []string) error {
	// Check for forbidden classes
	for _, class := range classes {
		if containsString(c.forbiddenClasses, class) {
			return NewForbiddenClassError(name, class)
		}
	}

	// If allowed classes specified, module must have at least one
	if len(c.allowedClasses) > 0 {
		hasAllowed := false
		for _, class := range classes {
			if containsString(c.allowedClasses, class) {
				hasAllowed = true
				break
			}
		}
		if !hasAllowed {
			return nil // Allow modules without explicit classes
		}
	}

	return nil
}

// getDefaultModules returns all available modules that pass class filtering.
func (c *Compiler) getDefaultModules() []string {
	var modules []string
	for name, m := range c.availableModules {
		if c.validateModuleClasses(name, m.Class) == nil {
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
func (c *Compiler) GetModuleBinder(modules []string) func(*lua.LState) {
	return func(l *lua.LState) {
		for _, name := range modules {
			m, ok := c.availableModules[name]
			if !ok {
				continue
			}
			l.SetGlobal(m.Name, engine.ModuleValue(m))
		}
	}
}

// GetAvailableModule returns a module by name.
func (c *Compiler) GetAvailableModule(name string) (*luaapi.ModuleDef, bool) {
	m, ok := c.availableModules[name]
	return m, ok
}

// ModuleInfo returns info about a module.
func (c *Compiler) ModuleInfo(name string) (luaapi.ModuleInfo, bool) {
	m, ok := c.availableModules[name]
	if !ok {
		return luaapi.ModuleInfo{}, false
	}
	return m.Info(), true
}

// IsModuleAllowed checks if a module would be allowed based on class filtering.
func (c *Compiler) IsModuleAllowed(name string) bool {
	m, ok := c.availableModules[name]
	if !ok {
		return false
	}
	return c.validateModuleClasses(name, m.Class) == nil
}

// GetForbiddenClasses returns the configured forbidden classes.
func (c *Compiler) GetForbiddenClasses() []string {
	return c.forbiddenClasses
}

// GetAllowedClasses returns the configured allowed classes.
func (c *Compiler) GetAllowedClasses() []string {
	return c.allowedClasses
}
