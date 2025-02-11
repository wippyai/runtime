package code

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	glua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/parse"
	"go.uber.org/zap"
	"strings"
	"time"
)

type (
	// CodeManager centralizes code and dependency management
	CodeManager struct {
		log      *zap.Logger
		bus      events.Bus
		memGraph *MemoryGraph
		compiler *Compiler

		// Transaction tracking
		txNodes map[registry.ID]bool
	}

	// Config defines initialization parameters
	Config struct {
		Modules        []api.Module
		ProtoCacheSize int
		MainCacheSize  int
	}
)

// NewCodeManager creates a new code manager instance
func NewCodeManager(log *zap.Logger, bus events.Bus, cfg *Config) (*CodeManager, error) {
	if cfg == nil {
		cfg = &Config{
			ProtoCacheSize: 100,
			MainCacheSize:  50,
		}
	}

	cm := &CodeManager{
		log:      log,
		bus:      bus,
		memGraph: NewMemoryGraph(),
		compiler: NewCompiler(
			func(node *Node) (*glua.FunctionProto, error) {
				chunk, err := parse.Parse(strings.NewReader(node.Source), node.ID.String())
				if err != nil {
					return nil, fmt.Errorf("parse error: %w", err)
				}

				fnProto, err := glua.Compile(chunk, node.ID.String())
				if err != nil {
					return nil, fmt.Errorf("compile error: %w", err)
				}

				return fnProto, nil
			},
			cfg.ProtoCacheSize,
			cfg.MainCacheSize,
		),
		txNodes: make(map[registry.ID]bool),
	}

	// built-in modules
	for _, mod := range cfg.Modules {
		node := &Node{
			ID:     registry.ID{NS: "", Name: mod.Name()},
			Kind:   api.KindModule,
			Module: mod,
		}

		if err := cm.memGraph.AddNode(node); err != nil {
			return nil, fmt.Errorf("failed to add module node: %w", err)
		}
	}

	return cm, nil
}

// Begin implements TransactionListener
func (cm *CodeManager) Begin(_ context.Context) {
	cm.txNodes = make(map[registry.ID]bool)
}

// Commit implements TransactionListener
func (cm *CodeManager) Commit(ctx context.Context) {
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

	// to slice of []registry.ID
	affectedSlice := make([]registry.ID, 0, len(affected))
	for id := range affected {
		affectedSlice = append(affectedSlice, id)
	}

	// Emit reset signal with affected nodes
	cm.bus.Send(ctx, events.Event{
		System: api.System,
		Kind:   api.EventResetNodes,
		Data:   affectedSlice,
	})
}

// Discard implements TransactionListener
func (cm *CodeManager) Discard(_ context.Context) {
	cm.txNodes = make(map[registry.ID]bool)
}

// Compile compiles a main entry point and its dependencies
func (cm *CodeManager) Compile(
	entrypoint registry.ID,
	options *BuildOptions,
) (*CompiledMain, error) {
	return cm.compiler.Compile(entrypoint, options)
}

// AddNode adds a new node with dependencies to the graph
func (cm *CodeManager) AddNode(_ context.Context, node Node, deps []Import) error {
	// Create pointer from value
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

	// Add node to graph
	if err := cm.memGraph.AddNode(nodePtr); err != nil {
		return fmt.Errorf("failed to add node: %w", err)
	}

	// Add dependencies
	for _, dep := range deps {
		if err := cm.memGraph.AddDependency(node.ID, dep.ID, dep.Alias); err != nil {
			return fmt.Errorf("failed to add dependency %s -> %s: %w",
				node.ID, dep.ID, err)
		}
	}

	// Mark node for transaction
	cm.txNodes[node.ID] = true

	return nil
}

// UpdateNode updates an existing node with new content and dependencies
func (cm *CodeManager) UpdateNode(_ context.Context, node Node, deps []Import) error {
	// Get existing node
	existing, err := cm.memGraph.GetNode(node.ID)
	if err != nil {
		return fmt.Errorf("node not found: %w", err)
	}

	// Update fields
	existing.Source = node.Source
	existing.Method = node.Method
	existing.Version = Version{
		Hash:    HashNode(&node),
		Created: time.Now(),
	}

	// Remove old dependencies
	oldDeps, err := cm.memGraph.GetDirectDependencies(node.ID)
	if err != nil {
		return fmt.Errorf("failed to get old dependencies: %w", err)
	}

	for _, dep := range oldDeps {
		if err := cm.memGraph.RemoveDependency(node.ID, dep.ID); err != nil {
			return fmt.Errorf("failed to remove old dependency: %w", err)
		}
	}

	// Add new dependencies
	for _, dep := range deps {
		if err := cm.memGraph.AddDependency(node.ID, dep.ID, dep.Alias); err != nil {
			return fmt.Errorf("failed to add new dependency: %w", err)
		}
	}

	// Mark node for transaction
	cm.txNodes[node.ID] = true

	return nil
}

// DeleteNode removes a node and its dependencies from the graph
func (cm *CodeManager) DeleteNode(_ context.Context, id registry.ID) error {
	// Get node to verify it exists
	if _, err := cm.memGraph.GetNode(id); err != nil {
		return fmt.Errorf("node not found: %w", err)
	}

	// Remove node (MemoryGraph handles dependency cleanup)
	if err := cm.memGraph.RemoveNode(id); err != nil {
		return fmt.Errorf("failed to remove node: %w", err)
	}

	// Mark node for transaction
	cm.txNodes[id] = true

	return nil
}
