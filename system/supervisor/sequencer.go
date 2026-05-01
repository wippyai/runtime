// SPDX-License-Identifier: MPL-2.0

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
	controller   controllable
	id           string
	dependencies []string
	blockers     []string
	kind         opKind
	optional     bool
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

func (sp *sequencer) processStartOperations(ctx context.Context, operations []operation) error {
	opMap := make(map[string]operation)
	for _, op := range operations {
		opMap[op.id] = op
	}

	// The supervisor planner classifies dependency policy before operations
	// reach the sequencer. At this layer dependencies are intentionally just
	// batch-local service edges; missing lifecycle requirements and ignored
	// registry references are represented as operation blockers.
	dependencies := make(map[string]map[string]struct{}, len(operations))
	dependents := make(map[string][]string, len(operations))
	for _, op := range operations {
		if dependencies[op.id] == nil {
			dependencies[op.id] = make(map[string]struct{})
		}
		for _, dep := range op.dependencies {
			if _, exists := opMap[dep]; !exists {
				continue
			}
			dependencies[op.id][dep] = struct{}{}
			dependents[dep] = append(dependents[dep], op.id)
		}
	}

	// Cycle detection is still strict for the services being transitioned
	// together. A real cycle inside this batch is not recoverable by retrying:
	// no member can become ready first, so the commit must fail loudly.
	if err := detectStartCycle(dependencies); err != nil {
		return err
	}

	started := make(map[string]struct{}, len(operations))
	blocked := make(map[string]struct{})
	inFlight := make(map[string]struct{})
	scheduled := make(map[string]struct{}, len(operations))
	resultCh := make(chan startResult, len(operations))
	stateChangedCh := make(chan struct{}, 1)
	doneWatching := make(chan struct{})
	defer close(doneWatching)
	var allErrors []error

	for _, op := range operations {
		if len(op.blockers) == 0 {
			continue
		}
		blocked[op.id] = struct{}{}
		blockDependentSubtree(op.id, dependents, blocked)
		if !op.optional {
			allErrors = append(allErrors, NewServiceBlockedError(op.id, op.blockers))
		}
	}

	scheduleReady := func() {
		for _, op := range operations {
			if _, done := started[op.id]; done {
				continue
			}
			if _, isBlocked := blocked[op.id]; isBlocked {
				continue
			}
			if _, alreadyScheduled := scheduled[op.id]; alreadyScheduled {
				continue
			}
			if !dependenciesSatisfied(dependencies[op.id], started) {
				continue
			}

			// Start only when all in-batch service dependencies have completed
			// their Start() call successfully. Optional failed branches are
			// marked blocked and never scheduled.
			scheduled[op.id] = struct{}{}
			inFlight[op.id] = struct{}{}
			if notifier, ok := op.controller.(startStateChangeNotifier); ok {
				go forwardStartStateChanges(ctx, doneWatching, notifier.startStateChanged(), stateChangedCh)
			}
			go func(op operation) {
				sp.logger.Info("starting service",
					zap.String("service_id", op.id))

				if err := op.controller.Start(); err != nil {
					resultCh <- startResult{
						serviceID: op.id,
						err:       NewServiceStartError(op.id, err),
					}
					return
				}
				resultCh <- startResult{serviceID: op.id}
			}(op)
		}
	}

	handleResult := func(result startResult) {
		delete(inFlight, result.serviceID)
		if result.err != nil {
			if !opMap[result.serviceID].optional {
				allErrors = append(allErrors, result.err)
			}
			// A failed service only poisons the subtree that depends on it.
			// Independent branches should continue to start, which is what lets
			// required app services boot while optional integrations retry.
			blockDependentSubtree(result.serviceID, dependents, blocked)
			return
		}
		started[result.serviceID] = struct{}{}
	}

	for {
		scheduleReady()

		if len(inFlight) == 0 {
			break
		}

		// Optional controllers may keep retrying after the initial Start()
		// attempt fails. If no unscheduled operation depends on those services,
		// the transition can finish and leave them supervised in the background.
		// Required services never use this escape hatch: they either become
		// running, exhaust retry policy and fail, or keep boot pending.
		if !hasPendingDependents(inFlight, dependencies, started, blocked, scheduled) {
			if allInFlightCanCompleteInBackground(inFlight, opMap) {
				break
			}
		}

		select {
		case result := <-resultCh:
			handleResult(result)
		case <-stateChangedCh:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if len(allErrors) == 0 {
		return nil
	}

	if len(allErrors) == 1 {
		return allErrors[0]
	}

	return NewMultiStartError(len(allErrors), allErrors[0])
}

type startResult struct {
	err       error
	serviceID string
}

type backgroundStartControllable interface {
	startMayCompleteInBackground() bool
}

type startStateChangeNotifier interface {
	startStateChanged() <-chan struct{}
}

func forwardStartStateChanges(
	ctx context.Context,
	done <-chan struct{},
	source <-chan struct{},
	target chan<- struct{},
) {
	for {
		select {
		case <-source:
			select {
			case target <- struct{}{}:
			default:
			}
		case <-done:
			return
		case <-ctx.Done():
			return
		}
	}
}

func allInFlightCanCompleteInBackground(inFlight map[string]struct{}, opMap map[string]operation) bool {
	for id := range inFlight {
		op, exists := opMap[id]
		if !exists {
			return false
		}
		if !op.optional {
			return false
		}
		bg, ok := op.controller.(backgroundStartControllable)
		if !ok || !bg.startMayCompleteInBackground() {
			return false
		}
	}
	return true
}

func dependenciesSatisfied(deps map[string]struct{}, started map[string]struct{}) bool {
	for dep := range deps {
		if _, ok := started[dep]; !ok {
			return false
		}
	}
	return true
}

func hasPendingDependents(
	inFlight map[string]struct{},
	dependencies map[string]map[string]struct{},
	started map[string]struct{},
	blocked map[string]struct{},
	scheduled map[string]struct{},
) bool {
	for id, deps := range dependencies {
		if _, done := started[id]; done {
			continue
		}
		if _, isBlocked := blocked[id]; isBlocked {
			continue
		}
		if _, alreadyScheduled := scheduled[id]; alreadyScheduled {
			continue
		}
		for dep := range deps {
			if _, running := inFlight[dep]; running {
				return true
			}
		}
	}
	return false
}

func blockDependentSubtree(root string, dependents map[string][]string, blocked map[string]struct{}) {
	for _, dependent := range dependents[root] {
		if _, seen := blocked[dependent]; seen {
			continue
		}
		blocked[dependent] = struct{}{}
		blockDependentSubtree(dependent, dependents, blocked)
	}
}

func detectStartCycle(dependencies map[string]map[string]struct{}) error {
	g := graph.New[string, any]()
	for id := range dependencies {
		g.AddNode(id)
	}
	for id, deps := range dependencies {
		for dep := range deps {
			g.AddEdge(dep, id, 1, nil)
		}
	}
	if _, err := g.DependencyLevels(); err != nil {
		return NewDependencyLevelsError("start", err)
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
