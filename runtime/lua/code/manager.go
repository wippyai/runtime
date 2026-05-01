// SPDX-License-Identifier: MPL-2.0

package code

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime/pprof"
	"sort"
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

// DefaultInvalidationWaitTimeout matches the registry/event request-reply
// budget by default. Boot wiring can override it from config.
const DefaultInvalidationWaitTimeout = event.DefaultAwaitTimeout

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

func (cm *Manager) nextVersion(hash string) Version {
	return Version{
		Hash:     hash,
		Created:  time.Now(),
		Revision: cm.revision.Add(1),
	}
}

type (
	// Manager centralizes code and dependency management
	Manager struct {
		bus                     event.Bus
		cacheStore              cache.Store
		log                     *zap.Logger
		memGraph                *MemoryGraph
		compiler                *Compiler
		typeChecker             *TypeChecker
		txAffected              map[registry.ID]registry.Kind
		typeCfgHash             string
		builtinHash             string
		cacheCfg                cache.Config
		revision                atomic.Uint64
		invalidationSeq         atomic.Uint64
		invalidationWaitTimeout time.Duration
		mutMu                   sync.Mutex
		txMu                    sync.Mutex
	}

	// Config defines initialization parameters
	Config struct {
		Cache                   cache.Config
		Modules                 []*api.ModuleDef
		ProtoCacheSize          int
		MainCacheSize           int
		TypeCheck               TypeCheckConfig
		InvalidationWaitTimeout time.Duration
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
	if cfg.InvalidationWaitTimeout <= 0 {
		cfg.InvalidationWaitTimeout = DefaultInvalidationWaitTimeout
	}

	typeChecker := NewTypeChecker(cfg.TypeCheck, cfg.Modules)
	cacheCfg := cfg.Cache.Normalize()

	cm := &Manager{
		log:                     log,
		bus:                     bus,
		memGraph:                NewMemoryGraph(),
		typeChecker:             typeChecker,
		txAffected:              make(map[registry.ID]registry.Kind),
		cacheCfg:                cacheCfg,
		typeCfgHash:             TypecheckConfigHash(cfg.TypeCheck),
		invalidationWaitTimeout: cfg.InvalidationWaitTimeout,
	}
	if cacheCfg.Enabled {
		cm.cacheStore = cache.NewDiskStore(cacheCfg.Dir)
	}

	// Create compiler with a callback that can access cm.memGraph for dependency manifests
	cm.compiler = NewCompiler(
		func(memGraph *MemoryGraph, node *Node) (*glua.FunctionProto, error) {
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
				if fingerprint, deps, err := cm.typecheckFingerprintFromGraph(memGraph, node.ID); err == nil {
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
					deps, _ := memGraph.GetDependenciesWithAliases(node.ID)
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
						cm.memGraph.SetManifestIfRevision(node.ID, node.Version.Revision, manifest)
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
			if fingerprint, deps, err := cm.compileFingerprintFromGraph(memGraph, node.ID); err == nil {
				compileFP = fingerprint
				compileDeps = deps
				if proto, ok := cm.loadCompileCache(node.ID, fingerprint); ok {
					if node.Manifest != nil && len(proto.TypeInfo) == 0 {
						if data, err := node.Manifest.Encode(); err == nil {
							proto.SetTypeInfo(data)
						}
					}
					return proto, nil
				}
			}

			if err := parseOnce(); err != nil {
				return nil, err
			}

			compileOpts, err := CompileOptionsForManifest(node.Manifest)
			if err != nil {
				return nil, fmt.Errorf("encode manifest for %s: %w", node.ID.String(), err)
			}
			fnProto, err := glua.CompileWithOptions(chunk, node.ID.String(), compileOpts)
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
			ID:      registry.NewID("", info.Name),
			Kind:    api.ModuleKind,
			Module:  mod,
			Version: cm.nextVersion(HashNode(&Node{Method: info.Name})),
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
func (cm *Manager) Begin(_ context.Context) error {
	cm.txMu.Lock()
	defer cm.txMu.Unlock()
	cm.txAffected = make(map[registry.ID]registry.Kind)
	return nil
}

// Commit implements TransactionListener
func (cm *Manager) Commit(ctx context.Context) error {
	cm.txMu.Lock()
	affected := cm.txAffected
	cm.txAffected = make(map[registry.ID]registry.Kind)
	cm.txMu.Unlock()

	// to slice of []registry.ID
	affectedSlice := make([]registry.ID, 0, len(affected))
	affectedNodes := make([]api.InvalidateNode, 0, len(affected))
	for id, kind := range affected {
		affectedSlice = append(affectedSlice, id)
		affectedNodes = append(affectedNodes, api.InvalidateNode{ID: id, Kind: kind})
	}
	sort.Slice(affectedNodes, func(i, j int) bool {
		return affectedNodes[i].ID.String() < affectedNodes[j].ID.String()
	})
	sort.Slice(affectedSlice, func(i, j int) bool {
		return affectedSlice[i].String() < affectedSlice[j].String()
	})

	return cm.invalidateNodes(ctx, affectedNodes, affectedSlice)
}

func (cm *Manager) invalidateNodes(ctx context.Context, nodes []api.InvalidateNode, ids []registry.ID) error {
	if len(ids) == 0 {
		return nil
	}

	awaitSvc := event.GetAwaitService(ctx)
	if awaitSvc == nil {
		// Preserve the old fire-and-forget shape for isolated tests and minimal embedders.
		cm.bus.Send(ctx, event.Event{
			System: api.System,
			Kind:   api.InvalidateNodes,
			Data:   ids,
		})
		return nil
	}

	ackPrefix := fmt.Sprintf("lua.invalidate/%d", cm.invalidationSeq.Add(1))
	waiters := make([]event.AwaitWaiter, 0, len(nodes))
	for _, node := range nodes {
		if !shouldAwaitInvalidation(node.Kind) {
			continue
		}
		path := ackPrefix + "/" + node.ID.String()
		waiter, err := awaitSvc.Prepare(ctx, api.System, api.InvalidateNodesResult, path, cm.invalidationWaitTimeout)
		if err != nil {
			cm.log.Error("failed to prepare lua invalidation waiter",
				zap.Stringer("node", &node.ID),
				zap.String("kind", node.Kind),
				zap.Error(err))
			for _, prepared := range waiters {
				prepared.Close()
			}
			return err
		}
		waiters = append(waiters, waiter)
	}

	// Emit reset signal with affected nodes, then wait for runtime handlers to
	// finish replacing pools/factories for callable nodes.
	cm.bus.Send(ctx, event.Event{
		System: api.System,
		Kind:   api.InvalidateNodes,
		Data: api.InvalidateNodesRequest{
			Nodes:     nodes,
			AckPrefix: ackPrefix,
		},
	})
	var errs []error
	for _, waiter := range waiters {
		result := waiter.Wait()
		if result.Error != nil {
			cm.log.Error("lua invalidation acknowledgement failed", zap.Error(result.Error))
			errs = append(errs, result.Error)
		}
	}
	return errors.Join(errs...)
}

func shouldAwaitInvalidation(kind registry.Kind) bool {
	switch kind {
	case api.Function, api.FunctionBytecode, api.Process, api.ProcessBytecode, api.Workflow, api.WorkflowBytecode:
		return true
	default:
		return false
	}
}

// Discard implements TransactionListener
func (cm *Manager) Discard(_ context.Context) error {
	cm.txMu.Lock()
	defer cm.txMu.Unlock()
	cm.txAffected = make(map[registry.ID]registry.Kind)
	return nil
}

func (cm *Manager) markTransactionAffected(nodes ...*Node) {
	cm.txMu.Lock()
	defer cm.txMu.Unlock()

	for _, node := range nodes {
		if node != nil {
			cm.txAffected[node.ID] = node.Kind
		}
	}
}

func (cm *Manager) affectedNodes(node *Node, dependents []*Node) []*Node {
	nodes := make([]*Node, 0, len(dependents)+1)
	nodes = append(nodes, node)
	return append(nodes, dependents...)
}

func nodeIDs(nodes []*Node) []registry.ID {
	ids := make([]registry.ID, 0, len(nodes))
	for _, node := range nodes {
		if node != nil {
			ids = append(ids, node.ID)
		}
	}
	return ids
}

// Compile compiles a main entry point and its dependencies
func (cm *Manager) Compile(
	entrypoint registry.ID,
	options *BuildOptions,
) (*CompiledMain, error) {
	return cm.compiler.Compile(cm.memGraph.Snapshot(), entrypoint, options)
}

// AddNode adds a new node with dependencies to the graph
func (cm *Manager) AddNode(_ context.Context, node Node, deps []Import) error {
	// Spawn pointer from value
	nodePtr := &Node{
		ID:      node.ID,
		Kind:    node.Kind,
		Source:  node.Source,
		Method:  node.Method,
		Module:  node.Module,
		Version: cm.nextVersion(HashNode(&node)),
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

	cm.mutMu.Lock()
	defer cm.mutMu.Unlock()

	if err := cm.memGraph.AddNode(nodePtr); err != nil {
		return NewAddNodeErrorWithCause(err)
	}

	for _, dep := range deps {
		if err := cm.memGraph.AddDependency(node.ID, dep.ID, dep.Alias); err != nil {
			_ = cm.memGraph.RemoveNode(node.ID)
			return NewAddDependencyError(node.ID, dep.ID, err)
		}
	}

	// A delete followed by a create with the same registry ID must never reuse
	// a previously compiled main/proto from the in-memory compiler caches.
	cm.compiler.Invalidate([]registry.ID{node.ID})
	cm.markTransactionAffected(nodePtr)

	return nil
}

// UpdateNode updates an existing node with new content and dependencies
func (cm *Manager) UpdateNode(_ context.Context, node Node, deps []Import) error {
	cm.mutMu.Lock()
	defer cm.mutMu.Unlock()

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

	nodePtr := &Node{
		ID:      node.ID,
		Kind:    existing.Kind,
		Source:  node.Source,
		Method:  node.Method,
		Module:  existing.Module,
		Version: cm.nextVersion(HashNode(&node)),
	}
	if err := cm.memGraph.UpdateNode(nodePtr, deps); err != nil {
		return NewAddNewDependencyError(err)
	}

	affectedNodes := cm.affectedNodes(nodePtr, dependents)
	cm.markTransactionAffected(affectedNodes...)

	// Calculate all dependents for cache invalidation
	if depErr != nil {
		cm.log.Warn("failed to get dependents for cache invalidation",
			zap.Stringer("node", &node.ID),
			zap.Error(depErr))
	}

	// Invalidate cache
	cm.compiler.Invalidate(nodeIDs(affectedNodes))

	if oldCompileFPs != nil || oldTypecheckFPs != nil {
		cm.deleteCacheFingerprints(oldCompileFPs, oldTypecheckFPs)
	}

	return nil
}

// GetNode retrieves a node from the graph by ID
func (cm *Manager) GetNode(id registry.ID) (*Node, error) {
	node, err := cm.memGraph.GetNode(id)
	if err != nil {
		return nil, err
	}
	return cloneNode(node), nil
}

// GetDirectDependencies returns direct dependencies of a node
func (cm *Manager) GetDirectDependencies(id registry.ID) ([]*Node, error) {
	deps, err := cm.memGraph.GetDirectDependencies(id)
	if err != nil {
		return nil, err
	}
	return cloneNodes(deps), nil
}

// DeleteNode removes a node and its dependencies from the graph
func (cm *Manager) DeleteNode(_ context.Context, id registry.ID) error {
	cm.mutMu.Lock()
	defer cm.mutMu.Unlock()

	existing, err := cm.memGraph.GetNode(id)
	if err != nil {
		return NewNodeNotFoundError(id)
	}

	dependents, depErr := cm.memGraph.GetAllDependents(id)

	var oldCompileFPs map[registry.ID]string
	var oldTypecheckFPs map[registry.ID]string
	if cm.cacheAllowsWrite() {
		oldCompileFPs = cm.compileFingerprints([]registry.ID{id})
		oldTypecheckFPs = cm.typecheckFingerprints([]registry.ID{id})
	}

	if err := cm.memGraph.RemoveNode(id); err != nil {
		return NewRemoveNodeError(err)
	}

	if depErr != nil {
		cm.log.Warn("failed to get dependents for transaction invalidation",
			zap.Stringer("node", &id),
			zap.Error(depErr))
	}

	affectedNodes := cm.affectedNodes(existing, dependents)
	cm.markTransactionAffected(affectedNodes...)

	cm.compiler.Invalidate(nodeIDs(affectedNodes))

	if oldCompileFPs != nil || oldTypecheckFPs != nil {
		cm.deleteCacheFingerprints(oldCompileFPs, oldTypecheckFPs)
	}

	return nil
}

// GetModules returns all registered modules with their info
func (cm *Manager) GetModules() []api.ModuleInfo {
	cm.memGraph.mu.RLock()
	defer cm.memGraph.mu.RUnlock()

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
	cm.memGraph.mu.RLock()
	defer cm.memGraph.mu.RUnlock()

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
	cm.memGraph.mu.RLock()
	defer cm.memGraph.mu.RUnlock()

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
		nodes = append(nodes, cloneNode(node))
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
	cm.mutMu.Lock()
	defer cm.mutMu.Unlock()

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
		ID:      node.ID,
		Kind:    node.Kind,
		Source:  node.Source,
		Method:  node.Method,
		Module:  node.Module,
		Version: cm.nextVersion(HashNodeWithProto(&node, proto)),
	}

	cm.mutMu.Lock()
	defer cm.mutMu.Unlock()

	if err := cm.memGraph.AddNode(nodePtr); err != nil {
		return NewAddNodeErrorWithCause(err)
	}

	for _, dep := range deps {
		if err := cm.memGraph.AddDependency(node.ID, dep.ID, dep.Alias); err != nil {
			_ = cm.memGraph.RemoveNode(node.ID)
			return NewAddDependencyError(node.ID, dep.ID, err)
		}
	}

	// Clear any old compiled main/proto for this ID before registering a fresh
	// bytecode node. SetProto below only replaces the proto cache; main cache
	// must also be dropped.
	cm.compiler.Invalidate([]registry.ID{node.ID})

	// Inject proto into compiler cache
	if proto != nil {
		tag, err := runtimeFingerprintMemo(cm.memGraph, node.ID, make(map[registry.ID]string))
		if err != nil {
			_ = cm.memGraph.RemoveNode(node.ID)
			return err
		}
		cm.compiler.SetProto(node.ID, tag, proto)
	}

	cm.markTransactionAffected(nodePtr)

	return nil
}

// UpdateNodeWithProto updates an existing node with a precompiled prototype.
func (cm *Manager) UpdateNodeWithProto(_ context.Context, node Node, deps []Import, proto *glua.FunctionProto) error {
	cm.mutMu.Lock()
	defer cm.mutMu.Unlock()

	existing, err := cm.memGraph.GetNode(node.ID)
	if err != nil {
		return NewNodeNotFoundError(node.ID)
	}

	nodePtr := &Node{
		ID:      node.ID,
		Kind:    existing.Kind,
		Source:  node.Source,
		Method:  node.Method,
		Module:  existing.Module,
		Version: cm.nextVersion(HashNodeWithProto(&node, proto)),
	}
	if err := cm.memGraph.UpdateNode(nodePtr, deps); err != nil {
		return NewAddNewDependencyError(err)
	}

	dependents, err := cm.memGraph.GetAllDependents(node.ID)
	if err != nil {
		cm.log.Warn("failed to get dependents for cache invalidation",
			zap.Stringer("node", &node.ID),
			zap.Error(err))
	}

	affectedNodes := cm.affectedNodes(nodePtr, dependents)
	cm.markTransactionAffected(affectedNodes...)
	cm.compiler.Invalidate(nodeIDs(affectedNodes))

	// Inject updated proto into compiler cache
	if proto != nil {
		tag, err := runtimeFingerprintMemo(cm.memGraph, node.ID, make(map[registry.ID]string))
		if err != nil {
			return err
		}
		cm.compiler.SetProto(node.ID, tag, proto)
	}

	return nil
}

func cloneNode(node *Node) *Node {
	if node == nil {
		return nil
	}
	copied := *node
	return &copied
}

func cloneNodes(nodes []*Node) []*Node {
	out := make([]*Node, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, cloneNode(node))
	}
	return out
}
