package supervisor

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/ponyruntime/pony/internal/graph"
	"go.uber.org/zap"
)

type OperationType int

const (
	OperationStart OperationType = iota
	OperationStop
)

// Operation represents a single service lifecycle operation
type Operation struct {
	Type         OperationType
	ID           string
	Controller   Controllable
	Dependencies []string
}

// Sequencer handles ordered processing of service operations based on dependencies
type Sequencer struct {
	logger *zap.Logger
	mu     sync.RWMutex
}

// NewSequencer creates a new sequence processor
func NewSequencer(logger *zap.Logger) *Sequencer {
	return &Sequencer{
		logger: logger,
	}
}

// Transition executes a set of service operations in the correct dependency order
func (sp *Sequencer) Transition(ctx context.Context, operations ...Operation) error {
	if len(operations) == 0 {
		return nil
	}

	log.Printf("Processing %+v operations", operations)

	// Separate start and stop operations
	var startOps, stopOps []Operation
	for _, op := range operations {
		switch op.Type {
		case OperationStart:
			startOps = append(startOps, op)
		case OperationStop:
			stopOps = append(stopOps, op)
		}
	}

	// Process stops first (in reverse dependency order)
	if len(stopOps) > 0 {
		if err := sp.processStopOperations(ctx, stopOps); err != nil {
			return fmt.Errorf("stop sequence failed: %w", err)
		}
	}

	// Then process starts (in dependency order)
	if len(startOps) > 0 {
		if err := sp.processStartOperations(ctx, startOps); err != nil {
			return fmt.Errorf("start sequence failed: %w", err)
		}
	}

	return nil
}

func (sp *Sequencer) processStartOperations(ctx context.Context, operations []Operation) error {
	// Build dependency graph for starts
	g := graph.NewGraph()

	// Add all services as nodes
	for _, op := range operations {
		g.AddNode(graph.Node(op.ID))
	}

	// Add dependency edges
	for _, op := range operations {
		for _, dep := range op.Dependencies {
			// Add edge from dependency to dependent
			g.AddEdge(graph.Edge{
				From:   graph.Node(dep),
				To:     graph.Node(op.ID),
				Weight: 1,
			})
		}
	}

	// Get dependency levels
	levels, err := g.DependencyLevels()
	if err != nil {
		return fmt.Errorf("failed to determine start dependency levels: %w", err)
	}

	// Create operation lookup map
	opMap := make(map[string]Operation)
	for _, op := range operations {
		opMap[op.ID] = op
	}

	// Process each level in sequence
	for i := 0; i < levels.LevelCount(); i++ {
		levelNodes, err := levels.GetLevel(i)
		if err != nil {
			return fmt.Errorf("failed to get level %d: %w", i, err)
		}

		var wg sync.WaitGroup
		errChan := make(chan error, len(levelNodes))

		// Start services in current level in parallel
		for _, node := range levelNodes {
			serviceID := string(node)
			if op, exists := opMap[serviceID]; exists {
				wg.Add(1)
				go func(op Operation) {
					defer wg.Done()

					sp.logger.Info("starting service",
						zap.String("service_id", op.ID),
						zap.Int("level", i))

					if err := op.Controller.Start(); err != nil {
						errChan <- fmt.Errorf("failed to start service %s: %w", op.ID, err)
					}
				}(op)
			}
		}

		// Wait for current level to complete
		wg.Wait()
		close(errChan)

		// Check for any errors
		for err := range errChan {
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (sp *Sequencer) processStopOperations(ctx context.Context, operations []Operation) error {
	g := graph.NewGraph()
	opMap := make(map[string]Operation)

	// Add all nodes first
	for _, op := range operations {
		g.AddNode(graph.Node(op.ID))
		opMap[op.ID] = op
	}

	// For stop operations, if A depends on B, we need to stop A before B
	// So we add edges from dependent to dependency
	for _, op := range operations {
		for _, depID := range op.Dependencies {
			if _, exists := opMap[depID]; exists {
				// Add edge FROM dependent TO dependency
				// This ensures dependent is processed before its dependencies
				g.AddEdge(graph.Edge{
					From:   graph.Node(depID), // Dependency
					To:     graph.Node(op.ID), // Dependent
					Weight: 1,
				})
			}
		}
	}

	levels, err := g.DependencyLevels()
	if err != nil {
		return fmt.Errorf("failed to determine stop dependency levels: %w", err)
	}

	// Process each level in sequence
	for i := 0; i < levels.LevelCount(); i++ {
		levelNodes, err := levels.GetLevel(i)
		if err != nil {
			return fmt.Errorf("failed to get level %d: %w", i, err)
		}

		var wg sync.WaitGroup
		errChan := make(chan error, len(levelNodes))

		// Stop services in current level in parallel
		for _, node := range levelNodes {
			serviceID := string(node)
			if op, exists := opMap[serviceID]; exists {
				wg.Add(1)
				go func(op Operation) {
					defer wg.Done()

					sp.logger.Info("stopping service",
						zap.String("service_id", op.ID),
						zap.Int("level", i))

					if err := op.Controller.Stop(); err != nil {
						errChan <- fmt.Errorf("failed to stop service %s: %w", op.ID, err)
					}
				}(op)
			}
		}

		wg.Wait()
		close(errChan)

		for err := range errChan {
			if err != nil {
				return err
			}
		}
	}

	return nil
}
