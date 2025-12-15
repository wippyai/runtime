package supervisor

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/internal/graph"
	"go.uber.org/zap"
)

type opKind int

const (
	opStart opKind = iota
	opStop
)

type operation struct {
	kind         opKind
	id           string
	controller   controllable
	dependencies []string
}

type sequencer struct {
	logger *zap.Logger
}

func newSequencer(logger *zap.Logger) *sequencer {
	return &sequencer{
		logger: logger,
	}
}

func (sp *sequencer) transition(ctx context.Context, operations ...operation) error {
	if len(operations) == 0 {
		return nil
	}

	// Separate start and stop operations
	var startOps, stopOps []operation
	for _, op := range operations {
		switch op.kind {
		case opStart:
			startOps = append(startOps, op)
		case opStop:
			stopOps = append(stopOps, op)
		}
	}

	// Process stops first (in reverse dependency order)
	if len(stopOps) > 0 {
		if err := sp.processStopOperations(ctx, stopOps); err != nil {
			return NewStopSequenceError(err)
		}
	}

	// Then process starts (in dependency order)
	if len(startOps) > 0 {
		if err := sp.processStartOperations(ctx, startOps); err != nil {
			return NewStartSequenceError(err)
		}
	}

	return nil
}

func (sp *sequencer) processStartOperations(_ context.Context, operations []operation) error {
	// Build dependency graph for starts
	g := graph.New[string, any]()

	// Add all services as nodes
	for _, op := range operations {
		g.AddNode(op.id)
	}

	// Add dependency edges
	for _, op := range operations {
		for _, dep := range op.dependencies {
			// Add edge from dependency to dependent
			g.AddEdge(dep, op.id, 1, nil)
		}
	}

	// Spawn dependency levels
	levels, err := g.DependencyLevels()
	if err != nil {
		return NewDependencyLevelsError("start", err)
	}

	// Build operation lookup map
	opMap := make(map[string]operation)
	for _, op := range operations {
		opMap[op.id] = op
	}

	// Process each level in sequence
	allLevels := levels.AllLevels()
	for i, levelNodes := range allLevels {
		var wg sync.WaitGroup
		errChan := make(chan error, len(levelNodes))

		// Launch services in current level in parallel
		for _, serviceID := range levelNodes {
			if op, exists := opMap[serviceID]; exists {
				wg.Add(1)
				go func(op operation) {
					defer wg.Done()

					sp.logger.Info("starting service",
						zap.String("service_id", op.id),
						zap.Int("level", i))

					if err := op.controller.Start(); err != nil {
						errChan <- NewServiceStartError(op.id, err)
					}
				}(op)
			}
		}

		// Wait for current level to complete
		wg.Wait()
		close(errChan)

		// For start operations, stop on first error - don't start services with broken dependencies
		for err := range errChan {
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (sp *sequencer) processStopOperations(_ context.Context, operations []operation) error {
	g := graph.New[string, any]()
	opMap := make(map[string]operation)

	// Add all nodes first
	for _, op := range operations {
		g.AddNode(op.id)
		opMap[op.id] = op
	}

	// For stop operations, if A depends on B, we need to stop A before B
	// So we add edges from dependent to dependency
	for _, op := range operations {
		for _, depID := range op.dependencies {
			if _, exists := opMap[depID]; exists {
				// Add edge FROM dependent TO dependency
				// This ensures dependent is processed before its dependencies
				g.AddEdge(op.id, depID, 1, nil)
			}
		}
	}

	levels, err := g.DependencyLevels()
	if err != nil {
		return NewDependencyLevelsError("stop", err)
	}

	allErrors := make([]error, 0)

	// Process each level in sequence
	allLevels := levels.AllLevels()
	for i, levelNodes := range allLevels {
		var wg sync.WaitGroup
		errChan := make(chan error, len(levelNodes))

		// close services in current level in parallel
		for _, serviceID := range levelNodes {
			if op, exists := opMap[serviceID]; exists {
				wg.Add(1)
				go func(op operation) {
					defer wg.Done()

					sp.logger.Info("stopping service",
						zap.String("service_id", op.id),
						zap.Int("level", i))

					if err := op.controller.Stop(); err != nil {
						errChan <- NewServiceStopError(op.id, err)
					}
				}(op)
			}
		}

		wg.Wait()
		close(errChan)

		for err := range errChan {
			if err != nil {
				allErrors = append(allErrors, err)
			}
		}
	}

	if len(allErrors) == 0 {
		return nil
	}

	if len(allErrors) == 1 {
		return allErrors[0]
	}

	return NewMultiStopError(len(allErrors), allErrors[0])
}
