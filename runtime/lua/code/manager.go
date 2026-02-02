package code

import (
	"context"
	"fmt"
	"os"
	"runtime/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	glua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/compiler/ast"
	"github.com/wippyai/go-lua/compiler/parse"
	"github.com/wippyai/go-lua/types/diag"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code/cache"

	"go.uber.org/zap"
)

var typecheckProfileActive atomic.Bool

func withTypecheckCPUProfile(log *zap.Logger, id string, fn func() (*io.Manifest, []diag.Diagnostic)) (*io.Manifest, []diag.Diagnostic) {
	stop := startTypecheckCPUProfile(log, id)
	manifest, diagnostics := fn()
	if stop != nil {
		stop()
	}
	return manifest, diagnostics
}

func startTypecheckCPUProfile(log *zap.Logger, id string) func() {
	profilePath := os.Getenv("WIPPY_TYPECHECK_CPU_PROFILE")
	if profilePath == "" {
		return nil
	}
	entryFilter := os.Getenv("WIPPY_TYPECHECK_PROFILE_ENTRY")
	if entryFilter != "" && entryFilter != id {
		return nil
	}
	if !typecheckProfileActive.CompareAndSwap(false, true) {
		return nil
	}
	path := profilePath
	if strings.Contains(path, "%s") {
		path = fmt.Sprintf(path, sanitizeProfileID(id))
	}
	f, err := os.Create(path)
	if err != nil {
		typecheckProfileActive.Store(false)
		log.Warn("typecheck cpu profile create failed", zap.String("path", path), zap.Error(err))
		return nil
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		_ = f.Close()
		typecheckProfileActive.Store(false)
		log.Warn("typecheck cpu profile start failed", zap.String("path", path), zap.Error(err))
		return nil
	}
	log.Info("typecheck cpu profile started", zap.String("id", id), zap.String("path", path))
	return func() {
		pprof.StopCPUProfile()
		_ = f.Close()
		typecheckProfileActive.Store(false)
		log.Info("typecheck cpu profile stopped", zap.String("id", id), zap.String("path", path))
	}
}

func sanitizeProfileID(id string) string {
	replacer := strings.NewReplacer("/", "_", ":", "_", ".", "_")
	return replacer.Replace(id)
}

type (
	// Manager centralizes code and dependency management
	Manager struct {
		bus         event.Bus
		log         *zap.Logger
		memGraph    *MemoryGraph
		compiler    *Compiler
		typeChecker *TypeChecker
		txNodes     map[registry.ID]bool
		txMu        sync.Mutex
		cacheCfg    cache.Config
		cacheStore  cache.Store
		typeCfgHash string
		builtinHash string
	}

	// Config defines initialization parameters
	Config struct {
		Modules        []*api.ModuleDef
		ProtoCacheSize int
		MainCacheSize  int
		TypeCheck      TypeCheckConfig
		Cache          cache.Config
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
	cacheCfg := cfg.Cache.Normalize()

	cm := &Manager{
		log:         log,
		bus:         bus,
		memGraph:    NewMemoryGraph(),
		typeChecker: typeChecker,
		txNodes:     make(map[registry.ID]bool),
		cacheCfg:    cacheCfg,
		typeCfgHash: TypecheckConfigHash(cfg.TypeCheck),
	}
	if cacheCfg.Enabled {
		cm.cacheStore = cache.NewDiskStore(cacheCfg.Dir)
	}

	// Create compiler with a callback that can access cm.memGraph for dependency manifests
	cm.compiler = NewCompiler(
		func(node *Node) (*glua.FunctionProto, error) {
			var chunk []ast.Stmt
			var parsed bool
			parseOnce := func() error {
				if parsed {
					return nil
				}
				parsed = true
				parsedChunk, err := parse.Parse(strings.NewReader(node.Source), node.ID.String())
				if err != nil {
					return NewParseError(err, node.Source)
				}
				chunk = parsedChunk
				return nil
			}

			// Type check if enabled
			var diagnostics []diag.Diagnostic
			if typeChecker.IsEnabled() && node.Source != "" {
				var tcDeps []cache.DepMeta
				var tcFP string
				if fingerprint, deps, err := cm.typecheckFingerprint(node.ID); err == nil {
					tcFP = fingerprint
					tcDeps = deps
					if manifest, cachedDiagnostics, ok := cm.loadTypecheckCache(node.ID, fingerprint); ok {
						node.Manifest = manifest
						diagnostics = cachedDiagnostics
					}
				}
				if diagnostics == nil {
					if err := parseOnce(); err != nil {
						return nil, err
					}
					// Get dependency manifests from the graph
					imports := make(map[string]*io.Manifest)
					deps, _ := cm.memGraph.GetDependenciesWithAliases(node.ID)
					for _, dep := range deps {
						if dep.Node.Manifest != nil {
							imports[dep.Name] = dep.Node.Manifest
						}
					}

					// TODO(wippy): Remove timing logs once typecheck hot spots are identified.
					typecheckStart := time.Now()
					manifest, diags := withTypecheckCPUProfile(log, node.ID.String(), func() (*io.Manifest, []diag.Diagnostic) {
						return typeChecker.CheckParsed(chunk, node.ID.String(), imports)
					})
					diagnostics = diags
					lineCount := 0
					if node.Source != "" {
						lineCount = strings.Count(node.Source, "\n") + 1
					}
					cm.log.Debug("lua typecheck completed",
						zap.String("id", node.ID.String()),
						zap.Int("imports", len(imports)),
						zap.Int("lines", lineCount),
						zap.Int("bytes", len(node.Source)),
						zap.Duration("duration", time.Since(typecheckStart)))

					// Store manifest on the node for downstream dependencies
					if manifest != nil {
						node.Manifest = manifest
					}
					if tcFP != "" {
						cm.saveTypecheckCache(node, tcFP, tcDeps, manifest, diagnostics)
					}
				}

				if HasErrors(diagnostics) && typeChecker.IsStrict() {
					return nil, NewTypeCheckDiagnosticError(node.ID, diagnostics)
				}
			}

			var compileFP string
			var compileDeps []cache.DepMeta
			if fingerprint, deps, err := cm.compileFingerprint(node.ID); err == nil {
				compileFP = fingerprint
				compileDeps = deps
				if proto, ok := cm.loadCompileCache(node.ID, fingerprint); ok {
					return proto, nil
				}
			}

			if err := parseOnce(); err != nil {
				return nil, err
			}

			fnProto, err := glua.Compile(chunk, node.ID.String())
			if err != nil {
				return nil, NewCompileError(node.ID, err)
			}

			if compileFP != "" {
				cm.saveCompileCache(node, compileFP, compileDeps, fnProto)
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
		if mod.Types != nil {
			node.Manifest = mod.Types()
		}

		cm.log.Debug("adding built-in module", zap.String("name", info.Name))

		if err := cm.memGraph.AddNode(node); err != nil {
			return nil, NewAddModuleNodeError(err)
		}
	}

	cm.refreshBuiltinHash()

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

		// Type checking happens during compile to avoid duplicate work.
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

	dependents, depErr := cm.memGraph.GetAllDependents(node.ID)
	var oldCompileFPs map[registry.ID]string
	var oldTypecheckFPs map[registry.ID]string
	if cm.cacheAllowsWrite() {
		invalidateIDs := make([]registry.ID, 0, len(dependents)+1)
		invalidateIDs = append(invalidateIDs, node.ID)
		for _, dep := range dependents {
			invalidateIDs = append(invalidateIDs, dep.ID)
		}
		oldCompileFPs = cm.compileFingerprints(invalidateIDs)
		oldTypecheckFPs = cm.typecheckFingerprints(invalidateIDs)
	}

	// Eager compilation check: validate source code before updating
	if node.Source != "" && existing.Kind != api.ModuleKind {
		_, err := parse.Parse(strings.NewReader(node.Source), node.ID.String())
		if err != nil {
			return NewParseError(err, node.Source)
		}

		// Type checking happens during compile to avoid duplicate work.
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
	if depErr != nil {
		cm.log.Warn("failed to get dependents for cache invalidation",
			zap.Stringer("node", &node.ID),
			zap.Error(depErr))
	}

	invalidateIDs := make([]registry.ID, 0, len(dependents)+1)
	invalidateIDs = append(invalidateIDs, node.ID)

	for _, dep := range dependents {
		invalidateIDs = append(invalidateIDs, dep.ID)
	}

	// Invalidate cache
	cm.compiler.Invalidate(invalidateIDs)

	if oldCompileFPs != nil || oldTypecheckFPs != nil {
		cm.deleteCacheFingerprints(oldCompileFPs, oldTypecheckFPs)
	}

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

	var oldCompileFPs map[registry.ID]string
	var oldTypecheckFPs map[registry.ID]string
	if cm.cacheAllowsWrite() {
		oldCompileFPs = cm.compileFingerprints([]registry.ID{id})
		oldTypecheckFPs = cm.typecheckFingerprints([]registry.ID{id})
	}

	if err := cm.memGraph.RemoveNode(id); err != nil {
		return NewRemoveNodeError(err)
	}

	// Mark node for transaction
	cm.txMu.Lock()
	cm.txNodes[id] = true
	cm.txMu.Unlock()

	if oldCompileFPs != nil || oldTypecheckFPs != nil {
		cm.deleteCacheFingerprints(oldCompileFPs, oldTypecheckFPs)
	}

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

// GetModuleManifests returns manifests from the code manager graph.
func (cm *Manager) GetModuleManifests() map[registry.ID]*io.Manifest {
	manifests := make(map[registry.ID]*io.Manifest)
	for _, node := range cm.memGraph.nodes {
		if node.Manifest == nil {
			continue
		}
		manifests[node.ID] = node.Manifest
	}
	return manifests
}

// GetAllNodes returns all nodes in the code manager graph.
func (cm *Manager) GetAllNodes() []*Node {
	cm.memGraph.mu.RLock()
	defer cm.memGraph.mu.RUnlock()

	nodes := make([]*Node, 0, len(cm.memGraph.nodes))
	for _, node := range cm.memGraph.nodes {
		nodes = append(nodes, node)
	}
	return nodes
}

// GetNodeDependencyManifests returns manifests of a node's direct dependencies.
func (cm *Manager) GetNodeDependencyManifests(id registry.ID) map[string]*io.Manifest {
	deps, err := cm.memGraph.GetDependenciesWithAliases(id)
	if err != nil {
		return nil
	}

	manifests := make(map[string]*io.Manifest)
	for _, dep := range deps {
		if dep.Node != nil && dep.Node.Manifest != nil {
			manifests[dep.Name] = dep.Node.Manifest
		}
	}
	return manifests
}

// AddBuiltinType registers a module's types in the type checker's built-in environment
func (cm *Manager) AddBuiltinType(mod *api.ModuleDef) {
	if cm.typeChecker != nil {
		cm.typeChecker.AddBuiltin(mod)
	}
	cm.refreshBuiltinHash()
}

// GetTypeChecker returns the code manager's type checker for linting.
// The returned type checker has all registered modules available.
func (cm *Manager) GetTypeChecker() *TypeChecker {
	return cm.typeChecker
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
