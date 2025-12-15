package engine

import (
	"fmt"

	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	lua "github.com/yuin/gopher-lua"
)

// CompiledFactory creates processes from compiled code.
type CompiledFactory interface {
	CreateFactory(id registry.ID, opts ...FactoryOption) (process.FactoryFunc, error)
}

// ProcessFactory creates processes from compiled code with configurable module binding.
type ProcessFactory struct {
	code       *code.Manager
	baseBinder ModuleBinder        // Low-level modules (errors, channel, etc.)
	modules    []*luaapi.ModuleDef // Additional default modules
}

// Ensure ProcessFactory implements CompiledFactory
var _ CompiledFactory = (*ProcessFactory)(nil)

// NewProcessFactory creates a factory that wraps the code manager.
// baseBinder loads low-level modules (use LoadCoreModules or nil).
// modules are additional defaults loaded after baseBinder.
func NewProcessFactory(code *code.Manager, baseBinder ModuleBinder, modules []*luaapi.ModuleDef) *ProcessFactory {
	return &ProcessFactory{
		code:       code,
		baseBinder: baseBinder,
		modules:    modules,
	}
}

// FactoryOption configures process creation via ProcessFactory.
type FactoryOption func(*processConfig)

// processConfig holds all configuration for process creation.
type processConfig struct {
	// Compile-time constraints (passed to code.BuildOptions)
	buildMode      code.AccessMode
	allowedIDs     []registry.ID
	deniedIDs      []registry.ID
	requiredIDs    []registry.ID
	deniedClasses  []string // Compile-time class deny
	allowedClasses []string // Compile-time class allow

	// Runtime class filtering
	forbidClasses  []string // Fail if dependency has this class
	excludeClasses []string // Silently skip binding

	// Runtime module filtering
	forbidModules  []string // Fail if dependency matches name
	excludeModules []string // Silently skip binding

	// Extra modules beyond factory defaults
	extraModules []*luaapi.ModuleDef

	// Exclude default modules by name
	excludeDefaults []string

	// Custom filter (return false to exclude, error to fail)
	filter func(name string, classes []string) (bool, error)
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

// ForbidClasses fails process creation if any dependency has these classes.
func ForbidClasses(classes ...string) FactoryOption {
	return func(c *processConfig) {
		c.forbidClasses = append(c.forbidClasses, classes...)
	}
}

// ExcludeClasses silently skips binding modules with these classes.
func ExcludeClasses(classes ...string) FactoryOption {
	return func(c *processConfig) {
		c.excludeClasses = append(c.excludeClasses, classes...)
	}
}

// ForbidModules fails process creation if any dependency matches these names.
func ForbidModules(names ...string) FactoryOption {
	return func(c *processConfig) {
		c.forbidModules = append(c.forbidModules, names...)
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

// WithoutDefaultModule excludes a default module by name.
func WithoutDefaultModule(names ...string) FactoryOption {
	return func(c *processConfig) {
		c.excludeDefaults = append(c.excludeDefaults, names...)
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
	if len(cfg.deniedClasses) > 0 {
		buildOpts.WithDeniedClasses(cfg.deniedClasses...)
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
	binders := make([]ModuleBinder, 0)

	// 1. Base binder first (low-level: errors, channel, ostime, etc.)
	if f.baseBinder != nil {
		binders = append(binders, f.baseBinder)
	}

	// Build exclusion sets for O(1) lookup
	excludeClassSet := toSet(cfg.excludeClasses)
	excludeModuleSet := toSet(cfg.excludeModules)
	forbidClassSet := toSet(cfg.forbidClasses)
	forbidModuleSet := toSet(cfg.forbidModules)
	excludeDefaultSet := toSet(cfg.excludeDefaults)

	// 2. Additional default modules (filtered)
	defaultModules := make([]*luaapi.ModuleDef, 0, len(f.modules))
	for _, mod := range f.modules {
		name := mod.Name
		classes := mod.Class

		// Check exclusions
		if _, excluded := excludeDefaultSet[name]; excluded {
			continue
		}
		if _, excluded := excludeModuleSet[name]; excluded {
			continue
		}
		if hasAnyClass(classes, excludeClassSet) {
			continue
		}

		// Check forbids
		if _, forbidden := forbidModuleSet[name]; forbidden {
			return nil, fmt.Errorf("forbidden module in defaults: %s", name)
		}
		if hasAnyClass(classes, forbidClassSet) {
			return nil, fmt.Errorf("forbidden class in default module %s", name)
		}

		// Custom filter
		if cfg.filter != nil {
			include, err := cfg.filter(name, classes)
			if err != nil {
				return nil, fmt.Errorf("filter rejected default module %s: %w", name, err)
			}
			if !include {
				continue
			}
		}

		defaultModules = append(defaultModules, mod)
	}

	// Binder for default modules
	if len(defaultModules) > 0 {
		mods := defaultModules
		binders = append(binders, func(l *lua.LState) {
			for _, mod := range mods {
				mod.Load(l)
			}
		})
	}

	// Bind compiled dependencies
	depBinders, err := f.bindDependencies(compiled.Dependencies, cfg, excludeClassSet, excludeModuleSet, forbidClassSet, forbidModuleSet)
	if err != nil {
		return nil, err
	}
	binders = append(binders, depBinders...)

	// Bind preloaded
	preloadBinders, err := f.bindDependencies(compiled.Preloaded, cfg, excludeClassSet, excludeModuleSet, forbidClassSet, forbidModuleSet)
	if err != nil {
		return nil, err
	}
	binders = append(binders, preloadBinders...)

	// Extra modules
	for _, mod := range cfg.extraModules {
		m := mod
		binders = append(binders, func(l *lua.LState) {
			m.Load(l)
		})
	}

	return binders, nil
}

// bindDependencies creates binders for compiled dependencies with filtering.
func (f *ProcessFactory) bindDependencies(
	deps []code.CompiledProto,
	cfg *processConfig,
	excludeClassSet, excludeModuleSet, forbidClassSet, forbidModuleSet map[string]struct{},
) ([]ModuleBinder, error) {
	binders := make([]ModuleBinder, 0, len(deps))

	for _, dep := range deps {
		name := dep.Name
		var classes []string
		if dep.Node != nil && dep.Node.Module != nil {
			classes = dep.Node.Module.Info().Class
		}

		// Check exclusions
		if _, excluded := excludeModuleSet[name]; excluded {
			continue
		}
		if hasAnyClass(classes, excludeClassSet) {
			continue
		}

		// Check forbids
		if _, forbidden := forbidModuleSet[name]; forbidden {
			return nil, fmt.Errorf("forbidden module: %s", name)
		}
		if hasAnyClass(classes, forbidClassSet) {
			return nil, fmt.Errorf("forbidden class in module %s", name)
		}

		// Custom filter
		if cfg.filter != nil {
			include, err := cfg.filter(name, classes)
			if err != nil {
				return nil, fmt.Errorf("filter rejected module %s: %w", name, err)
			}
			if !include {
				continue
			}
		}

		// Create binder based on type
		if dep.Node != nil && dep.Node.Module != nil {
			mod := dep.Node.Module
			modName := name
			binders = append(binders, func(l *lua.LState) {
				l.PreloadModule(modName, func(l *lua.LState) int {
					return mod.Loader(l)
				})
			})
		}

		if dep.Proto != nil {
			proto := dep.Proto
			protoName := name
			binders = append(binders, func(l *lua.LState) {
				l.PreloadModule(protoName, func(l *lua.LState) int {
					fn := l.LoadProto(proto)
					l.Push(fn)
					l.Call(0, 1)
					return 1
				})
			})
		}
	}

	return binders, nil
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
