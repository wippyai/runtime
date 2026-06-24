// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"fmt"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
)

// CompiledFactory creates processes from compiled code.
type CompiledFactory interface {
	CreateFactory(id registry.ID, opts ...FactoryOption) (process.FactoryFunc, error)
}

// ProcessFactory creates processes from compiled code with configurable module binding.
type ProcessFactory struct {
	code *code.Manager
}

// Ensure ProcessFactory implements CompiledFactory
var _ CompiledFactory = (*ProcessFactory)(nil)

// NewProcessFactory creates a factory that wraps the code manager.
// Core modules (channel, pubsub, print, payload, ostime) are loaded by default.
func NewProcessFactory(code *code.Manager) *ProcessFactory {
	return &ProcessFactory{
		code: code,
	}
}

// FactoryOption configures process creation via ProcessFactory.
type FactoryOption func(*processConfig)

// processConfig holds all configuration for process creation.
type processConfig struct {
	filter         func(name string, classes []string) (bool, error)
	allowedIDs     []registry.ID
	deniedIDs      []registry.ID
	requiredIDs    []registry.ID
	allowedClasses []string
	forbidClasses  []string
	excludeClasses []string
	forbidModules  []string
	excludeModules []string
	extraModules   []*luaapi.ModuleDef
	buildMode      code.AccessMode
}

func newProcessConfig() *processConfig {
	return &processConfig{
		buildMode: code.AllowAll,
	}
}

// WithMode sets the compile-time access mode.
func WithMode(mode code.AccessMode) FactoryOption {
	return func(c *processConfig) {
		c.buildMode = mode
	}
}

// WithAllowed adds IDs to the compile-time allowed list.
func WithAllowed(ids ...registry.ID) FactoryOption {
	return func(c *processConfig) {
		c.allowedIDs = append(c.allowedIDs, ids...)
	}
}

// WithAllowedClasses sets compile-time allowed classes.
// Modules must have at least one of these classes to be allowed.
func WithAllowedClasses(classes ...string) FactoryOption {
	return func(c *processConfig) {
		c.allowedClasses = append(c.allowedClasses, classes...)
	}
}

// ExcludeClasses silently skips binding modules with these classes.
func ExcludeClasses(classes ...string) FactoryOption {
	return func(c *processConfig) {
		c.excludeClasses = append(c.excludeClasses, classes...)
	}
}

// ExcludeModules silently skips binding modules with these names.
func ExcludeModules(names ...string) FactoryOption {
	return func(c *processConfig) {
		c.excludeModules = append(c.excludeModules, names...)
	}
}

// WithModule adds an extra module to load.
func WithModule(mod *luaapi.ModuleDef) FactoryOption {
	return func(c *processConfig) {
		c.extraModules = append(c.extraModules, mod)
	}
}

// WithFilter sets a custom filter function.
// Return (true, nil) to include, (false, nil) to exclude, (false, err) to fail.
func WithFilter(fn func(name string, classes []string) (bool, error)) FactoryOption {
	return func(c *processConfig) {
		c.filter = fn
	}
}

// CreateFactory returns a factory function for creating processes.
func (f *ProcessFactory) CreateFactory(id registry.ID, opts ...FactoryOption) (process.FactoryFunc, error) {
	cfg := newProcessConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	// Build compile-time options
	buildOpts := code.NewBuildOptions().WithMode(cfg.buildMode)
	if len(cfg.allowedIDs) > 0 {
		buildOpts.WithAllowed(cfg.allowedIDs...)
	}
	if len(cfg.deniedIDs) > 0 {
		buildOpts.WithDenied(cfg.deniedIDs...)
	}
	if len(cfg.requiredIDs) > 0 {
		buildOpts.WithRequired(cfg.requiredIDs...)
	}
	if len(cfg.allowedClasses) > 0 {
		buildOpts.WithAllowedClasses(cfg.allowedClasses...)
	}

	// Compile
	compiled, err := f.code.Compile(id, buildOpts)
	if err != nil {
		return nil, err
	}

	// Build binders with runtime filtering
	binders, err := f.buildBinders(compiled, cfg)
	if err != nil {
		return nil, err
	}

	factoryCfg := FactoryConfig{
		Proto:         compiled.Main,
		ModuleBinders: binders,
	}

	factory := NewFactory(factoryCfg)
	return factory, nil
}

// buildBinders creates module binders with filtering applied.
func (f *ProcessFactory) buildBinders(compiled *code.CompiledMain, cfg *processConfig) ([]ModuleBinder, error) {
	binders := []ModuleBinder{LoadCoreModules}

	// Extra modules are runtime-injected and globally available to every chunk
	// via the shared base globals.
	for _, mod := range cfg.extraModules {
		m := mod
		binders = append(binders, func(l *lua.LState) error {
			LoadModuleDef(l, m)
			return nil
		})
	}

	// Preloaded modules are forced-available globals as well.
	for _, pre := range compiled.Preloaded {
		dep := pre
		if dep.Node == nil || dep.Node.Module == nil {
			continue
		}
		mod := dep.Node.Module
		name := dep.Name
		binders = append(binders, func(l *lua.LState) error {
			l.SetGlobal(name, ModuleValue(mod))
			return nil
		})
	}

	// Build exclusion sets for O(1) lookup
	excludeClassSet := toSet(cfg.excludeClasses)
	excludeModuleSet := toSet(cfg.excludeModules)
	forbidClassSet := toSet(cfg.forbidClasses)
	forbidModuleSet := toSet(cfg.forbidModules)

	binders = append(binders, f.isolationBinder(compiled, cfg, excludeClassSet, excludeModuleSet, forbidClassSet, forbidModuleSet))
	return binders, nil
}

// isolationBinder binds dependencies into per-chunk scoped environments.
// Registry modules and libraries are never placed in the shared _G; instead
// each chunk (the entrypoint and every library) runs under its own environment
// table that exposes only the imports that chunk declared, plus a scoped
// require that resolves the same set. The shared _G holds only the safe base
// (core libs, extra/preloaded modules) reachable through each env's metatable.
func (f *ProcessFactory) isolationBinder(
	compiled *code.CompiledMain,
	cfg *processConfig,
	excludeClassSet, excludeModuleSet, forbidClassSet, forbidModuleSet map[string]struct{},
) ModuleBinder {
	return func(l *lua.LState) error {
		base := l.Get(lua.GlobalsIndex).(*lua.LTable)
		valueByNode := make(map[registry.ID]lua.LValue, len(compiled.Dependencies))
		processed := make(map[registry.ID]struct{}, len(compiled.Dependencies))

		// Dependencies are ordered deepest-first, so a library's imports are
		// resolved before the library itself runs.
		for _, dep := range compiled.Dependencies {
			if dep.Node == nil {
				continue
			}
			id := dep.Node.ID
			if _, done := processed[id]; done {
				continue
			}
			processed[id] = struct{}{}

			if dep.Node.Kind == luaapi.ModuleKind && dep.Node.Module != nil {
				mod := dep.Node.Module
				name := mod.Info().Name

				if _, excluded := excludeModuleSet[name]; excluded {
					continue
				}
				if hasAnyClass(mod.Class, excludeClassSet) {
					continue
				}
				if _, forbidden := forbidModuleSet[name]; forbidden {
					return fmt.Errorf("forbidden module: %s", name)
				}
				if hasAnyClass(mod.Class, forbidClassSet) {
					return fmt.Errorf("forbidden class in module %s", name)
				}
				if cfg.filter != nil {
					include, err := cfg.filter(name, mod.Class)
					if err != nil {
						return fmt.Errorf("filter rejected module %s: %w", name, err)
					}
					if !include {
						continue
					}
				}

				valueByNode[id] = ModuleValue(mod)
				continue
			}

			if dep.Proto != nil {
				env := buildChunkEnv(l, base, compiled.Imports[id], valueByNode)
				fn := l.LoadProto(dep.Proto)
				fn.Env = env
				l.Push(fn)
				if err := l.PCall(0, 1, nil); err != nil {
					return fmt.Errorf("failed to load dependency %s: %w", dep.Name, err)
				}
				valueByNode[id] = l.Get(-1)
				l.Pop(1)
			}
		}

		// The entrypoint runs under its own scoped environment. Assigning it as
		// the state environment makes LoadProto stamp the main chunk with it.
		l.Env = buildChunkEnv(l, base, compiled.Imports[compiled.MainID], valueByNode)
		return nil
	}
}

// buildChunkEnv creates a scoped global environment for one code chunk. The
// environment exposes only the chunk's declared imports (both as globals and
// through a scoped require) and falls back to the shared safe base for standard
// globals via its metatable. Undeclared modules resolve to nil and require
// fails closed.
func buildChunkEnv(l *lua.LState, base *lua.LTable, imports []code.Import, valueByNode map[registry.ID]lua.LValue) *lua.LTable {
	env := l.NewTable()
	mt := l.NewTable()
	mt.RawSetString("__index", base)
	l.SetMetatable(env, mt)

	resolved := make(map[string]lua.LValue, len(imports))
	for _, imp := range imports {
		v, ok := valueByNode[imp.ID]
		if !ok {
			continue
		}
		alias := imp.Alias
		if alias == "" {
			alias = imp.ID.Name
		}
		env.RawSetString(alias, v)
		resolved[alias] = v
	}

	env.RawSetString("require", l.NewFunction(func(s *lua.LState) int {
		name := s.CheckString(1)
		if v, ok := resolved[name]; ok {
			s.Push(v)
			return 1
		}
		// Fall back to the always-available base (core libs, extra/preloaded
		// modules). Registry modules are not in the base, so an undeclared
		// capability module (e.g. funcs) still fails closed here.
		if v := base.RawGetString(name); v != lua.LNil {
			s.Push(v)
			return 1
		}
		s.RaiseError("module '%s' not found", name)
		return 0
	}))
	return env
}

// Helper functions

func toSet(items []string) map[string]struct{} {
	set := make(map[string]struct{}, len(items))
	for _, item := range items {
		set[item] = struct{}{}
	}
	return set
}

func hasAnyClass(classes []string, set map[string]struct{}) bool {
	for _, c := range classes {
		if _, ok := set[c]; ok {
			return true
		}
	}
	return false
}

// Low-level Factory (from proto)

// FactoryConfig configures a Lua process factory.
type FactoryConfig struct {
	Proto         *lua.FunctionProto
	StateOptions  *lua.Options
	Script        string
	ScriptName    string
	ModuleBinders []ModuleBinder
}

// Factory creates Lua processes with shared configuration.
// Holds binders and options - processes only store script/proto.
type Factory struct {
	proto         *lua.FunctionProto
	stateOpts     *lua.Options
	script        string
	scriptName    string
	moduleBinders []ModuleBinder
}

// NewFactory creates a ProcessFactory for Lua processes.
// The factory returns processes that are already initialized.
func NewFactory(cfg FactoryConfig) process.FactoryFunc {
	f := &Factory{
		proto:         cfg.Proto,
		script:        cfg.Script,
		scriptName:    cfg.ScriptName,
		moduleBinders: cfg.ModuleBinders,
		stateOpts:     cfg.StateOptions,
	}
	return f.Create
}

// Create produces a new initialized Process.
func (f *Factory) Create() (process.Process, error) {
	state, err := f.CreateState()
	if err != nil {
		return nil, err
	}

	proc := &Process{
		threads:  make([]*Task, 0, 4),
		queue:    NewTaskQueue(),
		yieldBuf: make([]*Task, 0, 4),
		factory:  f,
		state:    state,
	}

	if f.proto != nil {
		proc.proto = f.proto
	} else if f.script != "" {
		proc.script = f.script
		proc.scriptName = f.scriptName
	}

	return proc, nil
}

// NewFactoryFromProto creates a factory from a precompiled proto with default module bindings.
func NewFactoryFromProto(proto *lua.FunctionProto, binders ...ModuleBinder) process.FactoryFunc {
	return NewFactory(FactoryConfig{
		Proto:         proto,
		ModuleBinders: binders,
	})
}

// CreateState creates and initializes a new Lua state with core libs and module binders.
// Returns error if any module binder fails.
func (f *Factory) CreateState() (*lua.LState, error) {
	opts := lua.Options{
		RegistrySize:        128,
		RegistryMaxSize:     256 * 256,
		RegistryGrowStep:    16,
		SkipOpenLibs:        true,
		CallStackSize:       128,
		MinimizeStackMemory: true,
		IncludeGoStackTrace: true,
	}
	if f.stateOpts != nil {
		opts = *f.stateOpts
	}

	state := lua.NewState(opts)

	// Base functions (writes to globals directly)
	lua.OpenBase(state)

	// Standard libraries (cached, immutable, shared)
	BindCachedLibs(state)

	// Restricted package loader (wippy-specific)
	state.Push(lua.LGoFunc(OpenRestrictedPackage))
	state.Push(lua.LString(lua.LoadLibName))
	if err := state.PCall(1, 0, nil); err != nil {
		state.Close()
		return nil, fmt.Errorf("failed to load package loader: %w", err)
	}

	// Apply module binders
	for _, binder := range f.moduleBinders {
		if err := binder(state); err != nil {
			state.Close()
			return nil, fmt.Errorf("module binder failed: %w", err)
		}
	}

	return state, nil
}
