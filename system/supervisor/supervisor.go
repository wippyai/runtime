// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

const (
	actRegister actKind = iota
	actRemove
	actStart
	actStop
	actBegin
	actCommit
	actDiscard
)

type (
	actKind int

	action struct {
		entry     *supervisor.Entry
		serviceID string
		kind      actKind
	}

	// Supervisor manages the lifecycle of registered services, handling their
	// registration, startup, shutdown, and monitoring. It provides transaction
	// support for service state changes and integrates with the event system
	// for coordinated operations.
	Supervisor struct {
		ctx                context.Context
		bus                event.Bus
		subscriber         *eventbus.Subscriber
		logger             *zap.Logger
		controllers        map[string]*Controller
		actions            chan action
		tx                 *regTx
		sequencer          *sequencer
		dependencyResolver supervisor.DependencyResolver
		transitionMu       sync.Mutex
		wg                 sync.WaitGroup
		mu                 sync.RWMutex
	}

	// Option is a functional option for configuring a Supervisor.
	Option func(*Supervisor)
)

// NewSupervisor creates a new Supervisor instance with the provided event bus
// and logger. The supervisor is initially inactive and must be started with
// the Launch method.
func NewSupervisor(bus event.Bus, logger *zap.Logger, opts ...Option) *Supervisor {
	s := &Supervisor{
		bus:         bus,
		logger:      logger,
		controllers: make(map[string]*Controller),
		actions:     make(chan action, 1024),
		tx:          newRegTx(logger),
		sequencer:   newSequencer(logger),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// WithDependencyResolver configures the supervisor to use the provided resolver
// for discovering additional service dependencies beyond those declared in the
// lifecycle configuration.
func WithDependencyResolver(resolver supervisor.DependencyResolver) Option {
	return func(s *Supervisor) {
		s.dependencyResolver = resolver
	}
}

func (s *Supervisor) executeOperations(ctx context.Context, operations []operation) error {
	return s.runTransition(ctx, operations)
}

func (s *Supervisor) runTransition(ctx context.Context, operations []operation) error {
	if len(operations) == 0 {
		return nil
	}

	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()

	return s.sequencer.transition(ctx, operations...)
}

func (s *Supervisor) snapshotControllers() map[string]*Controller {
	s.mu.RLock()
	defer s.mu.RUnlock()

	controllers := make(map[string]*Controller, len(s.controllers))
	for id, ctrl := range s.controllers {
		controllers[id] = ctrl
	}

	return controllers
}

// GetState returns the current state of a service identified by its Alias.
// Returns an error if the service is not found.
func (s *Supervisor) GetState(id string) (State, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	controller, exists := s.controllers[id]
	if !exists {
		return State{}, NewServiceNotFoundError(id)
	}

	return controller.State(), nil
}

// GetAllStates returns a map of service states for all registered services,
// indexed by their service IDs.
func (s *Supervisor) GetAllStates() map[string]State {
	s.mu.RLock()
	defer s.mu.RUnlock()

	states := make(map[string]State)
	for id, controller := range s.controllers {
		states[id] = controller.State()
	}

	return states
}

// Start initializes the supervisor and begins listening for events.
// It sets up event subscriptions and starts the main control loop.
func (s *Supervisor) Start(ctx context.Context) error {
	// Set context before launching goroutine to avoid race with Stop()
	s.ctx = ctx

	// Subscribe to all relevant events using a single subscriber with patterns
	sub, err := eventbus.NewSubscriber(
		ctx,
		s.bus,
		"(registry|supervisor)",
		"*",
		s.handleEvent,
	)

	if err != nil {
		return NewSubscriberError(err)
	}
	s.subscriber = sub

	// Launch main control loop
	s.wg.Add(1)
	go s.run(ctx)

	s.logger.Info("supervisor started")

	return nil
}

// Stop gracefully shuts down the supervisor and all managed services.
// It ensures all services are properly stopped and resources are cleaned up.
func (s *Supervisor) Stop() error {
	s.logger.Info("stopping supervisor")

	if s.subscriber != nil {
		s.subscriber.Close()
		s.subscriber = nil
	}

	controllers := s.snapshotControllers()
	operations := make([]operation, 0)
	for id, ctrl := range controllers {
		operations = append(operations, operation{
			kind:         opStop,
			id:           id,
			controller:   ctrl,
			dependencies: ctrl.config.DependsOn,
		})
	}

	// close all controllers in proper dependency order
	if err := s.runTransition(s.ctx, operations); err != nil {
		s.logger.Error("failed to stop controllers during shutdown", zap.Error(err))
	}

	close(s.actions)
	s.wg.Wait()

	s.logger.Info("supervisor stopped")
	return nil
}

func (s *Supervisor) handleEvent(e event.Event) {
	if e.System == registry.System {
		switch e.Kind {
		case registry.TxBegin:
			s.actions <- action{kind: actBegin}
		case registry.TxCommit:
			s.actions <- action{kind: actCommit}
		case registry.TxDiscard:
			s.actions <- action{kind: actDiscard}
		}
		return
	}

	if e.System != supervisor.System {
		return
	}

	switch e.Kind {
	case supervisor.ServiceRegister:
		entry, ok := e.Data.(*supervisor.Entry)
		if !ok {
			s.logger.Error(
				"failed to decode registration entry",
				zap.String("event_path", e.Path),
			)
			return
		}

		s.actions <- action{
			serviceID: e.Path,
			kind:      actRegister,
			entry:     entry,
		}

	case supervisor.ServiceRemove:
		s.actions <- action{serviceID: e.Path, kind: actRemove}

	case supervisor.ServiceStart:
		s.actions <- action{serviceID: e.Path, kind: actStart}

	case supervisor.ServiceStop:
		s.actions <- action{serviceID: e.Path, kind: actStop}
	}
}

func (s *Supervisor) run(ctx context.Context) {
	defer s.logger.Info("supervisor control loop stopped")
	defer s.wg.Done()

	for action := range s.actions {
		switch action.kind {
		case actBegin:
			s.tx.begin()

		case actDiscard:
			s.tx.discard()

		case actCommit:
			// execute commit protocol
			err := s.execute(ctx, s.tx)
			if err != nil {
				s.logger.Error("failed to execute commit protocol", zap.Error(err))
				s.tx.reset()
				continue
			}

			s.tx.reset()

		case actRegister:
			action.entry.Config.InitDefaults()

			if err := s.tx.registerService(action.serviceID, action.entry); err != nil {
				s.logger.Error("failed to register service in transaction",
					zap.String("serviceID", action.serviceID),
					zap.Error(err),
				)
			}
			s.logger.Info("service registered", zap.String("serviceID", action.serviceID))

		case actRemove:
			if err := s.tx.removeService(action.serviceID); err != nil {
				s.logger.Error("failed to remove service from transaction",
					zap.String("serviceID", action.serviceID),
					zap.Error(err),
				)
			}

			s.logger.Info("service removed", zap.String("serviceID", action.serviceID))

		case actStart:
			if s.tx.open {
				s.logger.Warn("transaction already open")
				continue
			}

			controllers := s.snapshotControllers()
			l := s.logger.With(zap.String("serviceID", action.serviceID))
			if _, exists := controllers[action.serviceID]; exists {
				l.Info("service start requested")
				ops := s.buildStartOperations(controllers, action.serviceID)
				if err := s.executeOperations(ctx, ops); err != nil {
					s.logger.Error("failed to execute start operations", zap.Error(err))
				}
			}

		case actStop:
			if s.tx.open {
				s.logger.Warn("transaction already open")
				continue
			}

			controllers := s.snapshotControllers()
			l := s.logger.With(zap.String("serviceID", action.serviceID))
			if _, exists := controllers[action.serviceID]; exists {
				l.Info("service stop requested")
				ops := s.buildStopOperations(controllers, action.serviceID)
				if err := s.executeOperations(ctx, ops); err != nil {
					s.logger.Error("failed to execute stop operations", zap.Error(err))
				}
			}
		}
	}
}

// createStateHandler returns a state change handler function for a service
func (s *Supervisor) createStateHandler(id string) func(supervisor.Status, any) {
	return func(status supervisor.Status, details any) {
		if err, ok := details.(error); ok {
			switch {
			case errors.Is(err, supervisor.ErrExit):
				s.logger.Info(fmt.Sprintf("service %s is %s", id, status),
					zap.String("serviceID", id),
					zap.String("status", status),
					zap.Error(err),
				)
			case errors.Is(err, supervisor.ErrTerminated) || errors.Is(err, context.Canceled):
				s.logger.Warn(fmt.Sprintf("service %s is %s", id, status),
					zap.String("serviceID", id),
					zap.String("status", status),
					zap.Error(err),
				)
			default:
				s.logger.Error(fmt.Sprintf("service %s is %s", id, status),
					zap.String("serviceID", id),
					zap.String("status", status),
					zap.Error(err),
				)
			}
		} else if details != nil {
			s.logger.Info(fmt.Sprintf("service %s is %s", id, status),
				zap.String("serviceID", id),
				zap.String("status", status),
				zap.Any("details", details),
			)
		}

		s.bus.Send(s.ctx, event.Event{
			System: supervisor.System,
			Path:   id,
			Kind:   supervisor.ServiceUpdate,
			Data: State{
				Status:     status,
				Details:    details,
				Desired:    status,
				RetryCount: 0,
				LastUpdate: time.Now(),
			},
		})
	}
}

// resolveDependencies returns the complete list of dependencies for a service,
// combining lifecycle dependencies with registry-extracted dependencies.
func (s *Supervisor) resolveDependencies(
	controllers map[string]*Controller,
	serviceID string,
) ([]string, error) {
	ctrl, exists := controllers[serviceID]
	if !exists {
		return nil, NewServiceNotFoundError(serviceID)
	}

	// Start with lifecycle dependencies
	deps := make(map[string]struct{})
	for _, dep := range ctrl.config.DependsOn {
		deps[dep] = struct{}{}
	}

	// Add registry-extracted dependencies if resolver is configured
	if s.dependencyResolver != nil {
		id := registry.ParseID(serviceID)
		registryDeps, err := s.dependencyResolver(id)
		if err != nil {
			return nil, NewDependencyResolveError(serviceID, err)
		}

		for _, dep := range registryDeps {
			deps[dep.String()] = struct{}{}
		}
	}

	// Convert to slice
	result := make([]string, 0, len(deps))
	for dep := range deps {
		result = append(result, dep)
	}

	return result, nil
}

// execute processes the transaction by creating new services,
// stopping removed services, and starting auto-start services
func (s *Supervisor) execute(ctx context.Context, tx *regTx) error {
	// Mutate controller registry under lock, then run potentially long transitions
	// lock-free so state readers are never blocked behind start/stop timeouts.
	s.mu.Lock()
	for id, entry := range tx.register {
		if _, exists := s.controllers[id]; !exists {
			s.controllers[id] = NewController(s.ctx, entry.Service, entry.Config, s.createStateHandler(id))
		}
	}
	s.mu.Unlock()

	controllers := s.snapshotControllers()
	var operations []operation

	// Queue stop operations for services being removed
	for id := range tx.remove {
		if ctrl, exists := controllers[id]; exists {
			deps, err := s.resolveDependencies(controllers, id)
			if err != nil {
				return NewDependencyResolveError(id, err)
			}
			operations = append(operations, operation{
				kind:         opStop,
				id:           id,
				controller:   ctrl,
				dependencies: deps,
			})
		}
	}

	// Build start operations for new auto-start services and their dependencies
	visited := make(map[string]bool)
	var buildStartOps func(id string) error
	buildStartOps = func(id string) error {
		if visited[id] {
			return nil
		}
		visited[id] = true

		ctrl, exists := controllers[id]
		if !exists {
			return NewServiceNotFoundError(id)
		}

		// Resolve all dependencies (lifecycle + registry-extracted)
		deps, err := s.resolveDependencies(controllers, id)
		if err != nil {
			return err
		}

		// Visit dependencies first and filter out non-existent ones
		validDeps := make([]string, 0, len(deps))
		for _, depID := range deps {
			// Skip dependencies that don't exist as controllers
			// (registry-extracted deps might include non-service references)
			if _, exists := controllers[depID]; !exists {
				s.logger.Debug("skipping non-existent dependency",
					zap.String("service_id", id),
					zap.String("dependency", depID))
				continue
			}
			validDeps = append(validDeps, depID)
			if err := buildStartOps(depID); err != nil {
				return err
			}
		}

		operations = append(operations, operation{
			kind:         opStart,
			id:           id,
			controller:   ctrl,
			dependencies: validDeps,
		})

		return nil
	}

	// Find autostart services and build their start chains
	for id, entry := range tx.register {
		if entry.Config.AutoStart {
			if err := buildStartOps(id); err != nil {
				return NewStartOperationsError(err)
			}
		}
	}

	// Execute transitions in dependency order
	if err := s.runTransition(ctx, operations); err != nil {
		return NewTransitionError(err)
	}

	// Done stopped services
	s.mu.Lock()
	for id := range tx.remove {
		delete(s.controllers, id)
	}
	s.mu.Unlock()

	return nil
}

func (s *Supervisor) buildStartOperations(
	controllers map[string]*Controller,
	serviceID string,
) []operation {
	visited := make(map[string]bool)
	var operations []operation

	var visit func(id string)
	visit = func(id string) {
		if visited[id] {
			return
		}
		visited[id] = true

		ctrl, exists := controllers[id]
		if !exists {
			return
		}

		// Visit dependencies first
		for _, depID := range ctrl.config.DependsOn {
			visit(depID)
		}

		// Add operation after dependencies
		operations = append(operations, operation{
			kind:         opStart,
			id:           id,
			controller:   ctrl,
			dependencies: ctrl.config.DependsOn,
		})
	}

	visit(serviceID)
	return operations
}

func (s *Supervisor) buildStopOperations(
	controllers map[string]*Controller,
	serviceID string,
) []operation {
	visited := make(map[string]bool)
	var operations []operation

	// First, build a reverse dependency map
	dependedOnBy := make(map[string][]string)
	for id, ctrl := range controllers {
		for _, depID := range ctrl.config.DependsOn {
			dependedOnBy[depID] = append(dependedOnBy[depID], id)
		}
	}

	var visit func(id string)
	visit = func(id string) {
		if visited[id] {
			return
		}
		visited[id] = true

		// Visit services that depend on this one first
		for _, depID := range dependedOnBy[id] {
			visit(depID)
		}

		// Add operation after dependents
		if ctrl, exists := controllers[id]; exists {
			operations = append(operations, operation{
				kind:         opStop,
				id:           id,
				controller:   ctrl,
				dependencies: ctrl.config.DependsOn,
			})
		}
	}

	visit(serviceID)
	return operations
}
