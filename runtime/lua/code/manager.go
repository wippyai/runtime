package code

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/lua"
	glua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/compiler/parse"
	"github.com/yuin/gopher-lua/types"

	"go.uber.org/zap"
)

type (
	// Manager centralizes code and dependency management
	Manager struct {
		log         *zap.Logger
		bus         event.Bus
		memGraph    *MemoryGraph
		compiler    *Compiler
		typeChecker *TypeChecker

		// Transaction tracking
		txMu    sync.Mutex
		txNodes map[registry.ID]bool
	}

	// Config defines initialization parameters
	Config struct {
		Modules        []*api.ModuleDef
		ProtoCacheSize int
		MainCacheSize  int
		TypeCheck      TypeCheckConfig
	}
)

// NewCodeManager creates a new code manager instance
func NewCodeManager(log *zap.Logger, bus event.Bus, cfg Config) (*Manager, error) {
	if cfg.ProtoCacheSize <= 0 {
		cfg.ProtoCacheSize = 5000
	}

	if cfg.MainCacheSize <= 0 {
		cfg.MainCacheSize = 1000
	}

	typeChecker := NewTypeChecker(cfg.TypeCheck, cfg.Modules)

	cm := &Manager{
		log:         log,
		bus:         bus,
		memGraph:    NewMemoryGraph(),
		typeChecker: typeChecker,
		txNodes:     make(map[registry.ID]bool),
	}

	// Create compiler with a callback that can access cm.memGraph for dependency manifests
	cm.compiler = NewCompiler(
		func(node *Node) (*glua.FunctionProto, error) {
			chunk, err := parse.Parse(strings.NewReader(node.Source), node.ID.String())
			if err != nil {
				return nil, NewParseError(err, node.Source)
			}

			// Type check if enabled
			if typeChecker.IsEnabled() {
				// Get dependency manifests from the graph
				imports := make(map[string]*types.TypeManifest)
				deps, _ := cm.memGraph.GetDependenciesWithAliases(node.ID)
				for _, dep := range deps {
					if dep.Node.Manifest != nil {
						imports[dep.Name] = dep.Node.Manifest
					}
				}

				// Use typeChecker.Check which properly handles imports
				manifest, diagnostics, _ := typeChecker.Check(node.Source, node.ID.String(), imports)

				// Store manifest on the node for downstream dependencies
				if manifest != nil {
					node.Manifest = manifest
				}

				if HasErrors(diagnostics) && typeChecker.IsStrict() {
					return nil, NewTypeCheckDiagnosticError(node.ID, diagnostics)
				}
			}

			fnProto, err := glua.Compile(chunk, node.ID.String())
			if err != nil {
				return nil, NewCompileError(node.ID, err)
			}

			return fnProto, nil
		},
		cfg.ProtoCacheSize,
		cfg.MainCacheSize,
	)

	// built-in modules
	for _, mod := range cfg.Modules {
		info := mod.Info()
		node := &Node{
			ID:     registry.NewID("", info.Name),
			Kind:   api.ModuleKind,
			Module: mod,
		}

		cm.log.Debug("adding built-in module", zap.String("name", info.Name))

		if err := cm.memGraph.AddNode(node); err != nil {
			return nil, NewAddModuleNodeError(err)
		}
	}

	return cm, nil
}

// Begin implements TransactionListener
func (cm *Manager) Begin(_ context.Context) {
	cm.txMu.Lock()
	defer cm.txMu.Unlock()
	cm.txNodes = make(map[registry.ID]bool)
}

// Commit implements TransactionListener
func (cm *Manager) Commit(ctx context.Context) {
	cm.txMu.Lock()
	// Get all affected nodes
	affected := make(map[registry.ID]bool)
	for id := range cm.txNodes {
		// Get node and its dependencies
		_, err := cm.memGraph.GetNode(id)
		if err != nil {
			cm.log.Error("failed to get node", zap.Error(err))
			continue
		}

		// Mark node as affected
		affected[id] = true

		// Get all dependents
		deps, err := cm.memGraph.GetAllDependents(id)
		if err != nil {
			cm.log.Error("failed to get dependents", zap.Error(err))
			continue
		}

		// Mark all dependents as affected
		for _, dep := range deps {
			affected[dep.ID] = true
		}
	}

	// Clear transaction nodes
	cm.txNodes = make(map[registry.ID]bool)
	cm.txMu.Unlock()

	// to slice of []registry.Process
	affectedSlice := make([]registry.ID, 0, len(affected))
	for id := range affected {
		affectedSlice = append(affectedSlice, id)
	}

	// Emit reset signal with affected nodes
	cm.bus.Send(ctx, event.Event{
		System: api.System,
		Kind:   api.InvalidateNodes,
		Data:   affectedSlice,
	})
}

// Discard implements TransactionListener
func (cm *Manager) Discard(_ context.Context) {
	cm.txMu.Lock()
	defer cm.txMu.Unlock()
	cm.txNodes = make(map[registry.ID]bool)
}

// Compile compiles a main entry point and its dependencies
func (cm *Manager) Compile(
	entrypoint registry.ID,
	options *BuildOptions,
) (*CompiledMain, error) {
	return cm.compiler.Compile(cm.memGraph, entrypoint, options)
}

// AddNode adds a new node with dependencies to the graph
func (cm *Manager) AddNode(_ context.Context, node Node, deps []Import) error {
	// Spawn pointer from value
	nodePtr := &Node{
		ID:     node.ID,
		Kind:   node.Kind,
		Source: node.Source,
		Method: node.Method,
		Module: node.Module,
		Version: Version{
			Hash:    HashNode(&node),
			Created: time.Now(),
		},
	}

	// Eager compilation check: validate source code before adding to graph
	// This catches parse errors and type errors at registration time
	if node.Source != "" && node.Kind != api.ModuleKind {
		_, err := parse.Parse(strings.NewReader(node.Source), node.ID.String())
		if err != nil {
			return NewParseError(err, node.Source)
		}

		if cm.typeChecker.IsEnabled() {
			// Get dependency manifests from deps (node not yet in graph)
			imports := make(map[string]*types.TypeManifest)
			for _, dep := range deps {
				depNode, err := cm.memGraph.GetNode(dep.ID)
				if err == nil && depNode.Manifest != nil {
					alias := dep.Alias
					if alias == "" {
						alias = dep.ID.Name
					}
					imports[alias] = depNode.Manifest
				}
			}

			manifest, diagnostics, _ := cm.typeChecker.Check(node.Source, node.ID.String(), imports)
			nodePtr.Manifest = manifest

			if HasErrors(diagnostics) && cm.typeChecker.IsStrict() {
				return NewTypeCheckDiagnosticError(node.ID, diagnostics)
			}
		}
	}

	if err := cm.memGraph.AddNode(nodePtr); err != nil {
		return NewAddNodeErrorWithCause(err)
	}

	for _, dep := range deps {
		if err := cm.memGraph.AddDependency(node.ID, dep.ID, dep.Alias); err != nil {
			_ = cm.memGraph.RemoveNode(node.ID)
			return NewAddDependencyError(node.ID, dep.ID, err)
		}
	}

	// Mark node for transaction
	cm.txMu.Lock()
	cm.txNodes[node.ID] = true
	cm.txMu.Unlock()

	return nil
}

// UpdateNode updates an existing node with new content and dependencies
func (cm *Manager) UpdateNode(_ context.Context, node Node, deps []Import) error {
	existing, err := cm.memGraph.GetNode(node.ID)
	if err != nil {
		return NewNodeNotFoundError(node.ID)
	}

	// Eager compilation check: validate source code before updating
	if node.Source != "" && existing.Kind != api.ModuleKind {
		_, err := parse.Parse(strings.NewReader(node.Source), node.ID.String())
		if err != nil {
			return NewParseError(err, node.Source)
		}

		if cm.typeChecker.IsEnabled() {
			// Get dependency manifests from new deps
			imports := make(map[string]*types.TypeManifest)
			for _, dep := range deps {
				depNode, err := cm.memGraph.GetNode(dep.ID)
				if err == nil && depNode.Manifest != nil {
					alias := dep.Alias
					if alias == "" {
						alias = dep.ID.Name
					}
					imports[alias] = depNode.Manifest
				}
			}

			manifest, diagnostics, _ := cm.typeChecker.Check(node.Source, node.ID.String(), imports)
			existing.Manifest = manifest

			if HasErrors(diagnostics) && cm.typeChecker.IsStrict() {
				return NewTypeCheckDiagnosticError(node.ID, diagnostics)
			}
		}
	}

	// Update fields
	existing.Source = node.Source
	existing.Method = node.Method
	existing.Version = Version{
		Hash:    HashNode(&node),
		Created: time.Now(),
	}

	oldDeps, err := cm.memGraph.GetDirectDependencies(node.ID)
	if err != nil {
		return NewGetOldDependenciesError(err)
	}

	for _, dep := range oldDeps {
		if err := cm.memGraph.RemoveDependency(node.ID, dep.ID); err != nil {
			return NewRemoveOldDependencyError(err)
		}
	}

	for _, dep := range deps {
		if err := cm.memGraph.AddDependency(node.ID, dep.ID, dep.Alias); err != nil {
			return NewAddNewDependencyError(err)
		}
	}

	// Mark node for transaction
	cm.txMu.Lock()
	cm.txNodes[node.ID] = true
	cm.txMu.Unlock()

	// Calculate all dependents for cache invalidation
	dependents, err := cm.memGraph.GetAllDependents(node.ID)
	if err != nil {
		cm.log.Warn("failed to get dependents for cache invalidation",
			zap.Stringer("node", &node.ID),
			zap.Error(err))
	}

	invalidateIDs := make([]registry.ID, 0, len(dependents)+1)
	invalidateIDs = append(invalidateIDs, node.ID)

	for _, dep := range dependents {
		invalidateIDs = append(invalidateIDs, dep.ID)
	}

	// Invalidate cache
	cm.compiler.Invalidate(invalidateIDs)

	return nil
}

// GetNode retrieves a node from the graph by ID
func (cm *Manager) GetNode(id registry.ID) (*Node, error) {
	return cm.memGraph.GetNode(id)
}

// GetDirectDependencies returns direct dependencies of a node
func (cm *Manager) GetDirectDependencies(id registry.ID) ([]*Node, error) {
	return cm.memGraph.GetDirectDependencies(id)
}

// DeleteNode removes a node and its dependencies from the graph
func (cm *Manager) DeleteNode(_ context.Context, id registry.ID) error {
	if _, err := cm.memGraph.GetNode(id); err != nil {
		return NewNodeNotFoundError(id)
	}

	if err := cm.memGraph.RemoveNode(id); err != nil {
		return NewRemoveNodeError(err)
	}

	// Mark node for transaction
	cm.txMu.Lock()
	cm.txNodes[id] = true
	cm.txMu.Unlock()

	return nil
}

// GetModules returns all registered modules with their info
func (cm *Manager) GetModules() []api.ModuleInfo {
	var modules []api.ModuleInfo
	for _, node := range cm.memGraph.nodes {
		if node.Module != nil {
			modules = append(modules, node.Module.Info())
		}
	}
	return modules
}

// GetModuleDefs returns all registered module definitions
func (cm *Manager) GetModuleDefs() []*api.ModuleDef {
	var modules []*api.ModuleDef
	for _, node := range cm.memGraph.nodes {
		if node.Module != nil {
			modules = append(modules, node.Module)
		}
	}
	return modules
}

// AddBuiltinType registers a module's types in the type checker's built-in environment
func (cm *Manager) AddBuiltinType(mod *api.ModuleDef) {
	if cm.typeChecker != nil {
		cm.typeChecker.AddBuiltin(mod)
	}
}

// AddNodeWithProto adds a node with a precompiled prototype (for bytecode entries).
// The proto is injected directly into the compiler cache, bypassing source compilation.
func (cm *Manager) AddNodeWithProto(_ context.Context, node Node, deps []Import, proto *glua.FunctionProto) error {
	nodePtr := &Node{
		ID:     node.ID,
		Kind:   node.Kind,
		Source: node.Source,
		Method: node.Method,
		Module: node.Module,
		Version: Version{
			Hash:    HashNode(&node),
			Created: time.Now(),
		},
	}

	if err := cm.memGraph.AddNode(nodePtr); err != nil {
		return NewAddNodeErrorWithCause(err)
	}

	for _, dep := range deps {
		if err := cm.memGraph.AddDependency(node.ID, dep.ID, dep.Alias); err != nil {
			_ = cm.memGraph.RemoveNode(node.ID)
			return NewAddDependencyError(node.ID, dep.ID, err)
		}
	}

	// Inject proto into compiler cache
	if proto != nil {
		cm.compiler.SetProto(node.ID, proto)
	}

	cm.txMu.Lock()
	cm.txNodes[node.ID] = true
	cm.txMu.Unlock()

	return nil
}

// UpdateNodeWithProto updates an existing node with a precompiled prototype.
func (cm *Manager) UpdateNodeWithProto(_ context.Context, node Node, deps []Import, proto *glua.FunctionProto) error {
	existing, err := cm.memGraph.GetNode(node.ID)
	if err != nil {
		return NewNodeNotFoundError(node.ID)
	}

	existing.Source = node.Source
	existing.Method = node.Method
	existing.Version = Version{
		Hash:    HashNode(&node),
		Created: time.Now(),
	}

	oldDeps, err := cm.memGraph.GetDirectDependencies(node.ID)
	if err != nil {
		return NewGetOldDependenciesError(err)
	}

	for _, dep := range oldDeps {
		if err := cm.memGraph.RemoveDependency(node.ID, dep.ID); err != nil {
			return NewRemoveOldDependencyError(err)
		}
	}

	for _, dep := range deps {
		if err := cm.memGraph.AddDependency(node.ID, dep.ID, dep.Alias); err != nil {
			return NewAddNewDependencyError(err)
		}
	}

	cm.txMu.Lock()
	cm.txNodes[node.ID] = true
	cm.txMu.Unlock()

	dependents, err := cm.memGraph.GetAllDependents(node.ID)
	if err != nil {
		cm.log.Warn("failed to get dependents for cache invalidation",
			zap.Stringer("node", &node.ID),
			zap.Error(err))
	}

	invalidateIDs := make([]registry.ID, 0, len(dependents)+1)
	invalidateIDs = append(invalidateIDs, node.ID)
	for _, dep := range dependents {
		invalidateIDs = append(invalidateIDs, dep.ID)
	}
	cm.compiler.Invalidate(invalidateIDs)

	// Inject updated proto into compiler cache
	if proto != nil {
		cm.compiler.SetProto(node.ID, proto)
	}

	return nil
}
