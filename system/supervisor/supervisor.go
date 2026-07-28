// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"errors"
	"fmt"
	"sort"
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

	startRoot struct {
		id       string
		required bool
	}

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
		sequencer          *sequencer
		dependencyResolver supervisor.DependencyResolver
		controllers        map[string]*Controller
		actions            chan action
		done               chan struct{}
		tx                 *regTx
		subscriber         *eventbus.Subscriber
		logger             *zap.Logger
		stopErr            error
		wg                 sync.WaitGroup
		lifecycleMu        sync.RWMutex
		mu                 sync.RWMutex
		stopOnce           sync.Once
		transitionMu       sync.Mutex
		stopped            bool
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
		done:        make(chan struct{}),
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
	// Controllers outlive cancellation of the run loop so shutdown can stop
	// services with its own bounded context.
	s.ctx = context.WithoutCancel(ctx)

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
	return s.StopContext(context.Background())
}

// StopContext shuts down the supervisor using ctx to bound stop sequencing.
func (s *Supervisor) StopContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.stopOnce.Do(func() {
		s.logger.Info("stopping supervisor")

		s.lifecycleMu.Lock()
		s.stopped = true
		close(s.done)
		s.lifecycleMu.Unlock()

		if s.subscriber != nil {
			s.subscriber.Close()
			s.subscriber = nil
		}

		controllers := s.snapshotControllers()
		s.cancelActiveStarts(controllers)
		if err := s.stopFailedStartRetries(ctx, controllers); err != nil {
			s.stopErr = err
		}

		operations := make([]operation, 0)
		for id, ctrl := range controllers {
			deps, err := s.resolveDependencies(controllers, id)
			if err != nil {
				s.logger.Warn("failed to resolve service dependencies during shutdown; using lifecycle dependencies",
					zap.String("serviceID", id),
					zap.Error(err))
				deps = ctrl.config.RequiredServices()
			}
			operations = append(operations, operation{
				kind:         opStop,
				id:           id,
				controller:   ctrl,
				dependencies: deps,
			})
		}

		// close all controllers in proper dependency order
		if err := s.runTransition(ctx, operations); err != nil {
			s.logger.Error("failed to stop controllers during shutdown", zap.Error(err))
			var boundErr *controllerStopBoundError
			switch {
			case errors.As(err, &boundErr):
				s.stopErr = boundErr.cause
			case ctx.Err() != nil:
				s.stopErr = ctx.Err()
			}
		}

		s.wg.Wait()

		s.logger.Info("supervisor stopped")
	})
	return s.stopErr
}

func (s *Supervisor) cancelActiveStarts(controllers map[string]*Controller) {
	for _, ctrl := range controllers {
		ctrl.cancelStart()
	}
}

func (s *Supervisor) stopFailedStartRetries(ctx context.Context, controllers map[string]*Controller) error {
	var wg sync.WaitGroup
	boundErrors := make(chan error, len(controllers))
	for id, ctrl := range controllers {
		state := ctrl.State()
		if state.Desired != supervisor.StatusRunning || state.Status != supervisor.StatusFailed {
			continue
		}

		wg.Add(1)
		go func(id string, ctrl *Controller) {
			defer wg.Done()
			if err := ctrl.StopContext(ctx); err != nil {
				s.logger.Warn("failed to stop retrying service before shutdown",
					zap.String("serviceID", id),
					zap.Error(err))
				var boundErr *controllerStopBoundError
				if errors.As(err, &boundErr) {
					boundErrors <- boundErr.cause
				}
			}
		}(id, ctrl)
	}
	wg.Wait()
	close(boundErrors)
	if err := ctx.Err(); err != nil {
		return err
	}
	for err := range boundErrors {
		return err
	}
	return nil
}

func (s *Supervisor) handleEvent(e event.Event) {
	if e.System == registry.System {
		switch e.Kind {
		case registry.TxBegin:
			s.enqueueAction(action{kind: actBegin})
		case registry.TxCommit:
			s.enqueueAction(action{kind: actCommit})
		case registry.TxDiscard:
			s.enqueueAction(action{kind: actDiscard})
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

		s.enqueueAction(action{
			serviceID: e.Path,
			kind:      actRegister,
			entry:     entry,
		})

	case supervisor.ServiceRemove:
		s.enqueueAction(action{serviceID: e.Path, kind: actRemove})

	case supervisor.ServiceStart:
		s.enqueueAction(action{serviceID: e.Path, kind: actStart})

	case supervisor.ServiceStop:
		s.enqueueAction(action{serviceID: e.Path, kind: actStop})
	}
}

func (s *Supervisor) enqueueAction(next action) {
	s.lifecycleMu.RLock()
	if s.stopped {
		s.lifecycleMu.RUnlock()
		return
	}
	done := s.done
	s.lifecycleMu.RUnlock()

	select {
	case s.actions <- next:
	case <-done:
	}
}

func (s *Supervisor) run(ctx context.Context) {
	defer s.logger.Info("supervisor control loop stopped")
	defer s.wg.Done()

	for {
		var next action
		select {
		case <-s.done:
			return
		case next = <-s.actions:
		}
		select {
		case <-s.done:
			return
		default:
		}

		switch next.kind {
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
			next.entry.Config.InitDefaults()

			if err := s.tx.registerService(next.serviceID, next.entry); err != nil {
				s.logger.Error("failed to register service in transaction",
					zap.String("serviceID", next.serviceID),
					zap.Error(err),
				)
			}
			s.logger.Info("service registered", zap.String("serviceID", next.serviceID))

		case actRemove:
			if err := s.tx.removeService(next.serviceID); err != nil {
				s.logger.Error("failed to remove service from transaction",
					zap.String("serviceID", next.serviceID),
					zap.Error(err),
				)
			}

			s.logger.Info("service removed", zap.String("serviceID", next.serviceID))

		case actStart:
			if s.tx.open {
				s.logger.Warn("transaction already open")
				continue
			}

			controllers := s.snapshotControllers()
			l := s.logger.With(zap.String("serviceID", next.serviceID))
			if _, exists := controllers[next.serviceID]; exists {
				l.Info("service start requested")
				ops, err := s.buildStartOperations(controllers, next.serviceID)
				if err != nil {
					s.logger.Error("failed to build start operations", zap.Error(err))
					continue
				}
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
			l := s.logger.With(zap.String("serviceID", next.serviceID))
			if _, exists := controllers[next.serviceID]; exists {
				l.Info("service stop requested")
				ops, err := s.buildStopOperations(controllers, next.serviceID)
				if err != nil {
					s.logger.Error("failed to build stop operations", zap.Error(err))
					continue
				}
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
	lifecycleDeps, registryDeps, err := s.resolveDependencySets(controllers, serviceID)
	if err != nil {
		return nil, err
	}

	lifecycleServices, _, err := s.resolveServiceDependencyRefs(controllers, serviceID, lifecycleDeps, false)
	if err != nil {
		return nil, err
	}
	registryServices, _, err := s.resolveServiceDependencyRefs(controllers, serviceID, registryDeps, false)
	if err != nil {
		return nil, err
	}

	result := make([]string, 0, len(lifecycleServices)+len(registryServices))
	for _, dep := range lifecycleServices {
		result = appendUniqueString(result, dep)
	}
	for _, dep := range registryServices {
		result = appendUniqueString(result, dep)
	}

	return result, nil
}

func (s *Supervisor) resolveDependencySets(
	controllers map[string]*Controller,
	serviceID string,
) ([]string, []string, error) {
	ctrl, exists := controllers[serviceID]
	if !exists {
		return nil, nil, NewServiceNotFoundError(serviceID)
	}

	lifecycleDeps := ctrl.config.RequiredServices()
	registryDeps := make([]string, 0)

	if s.dependencyResolver != nil {
		id := registry.ParseID(serviceID)
		deps, err := s.dependencyResolver(id)
		if err != nil {
			return nil, nil, NewDependencyResolveError(serviceID, err)
		}

		for _, dep := range deps {
			registryDeps = append(registryDeps, dependencyIDString(dep))
		}
	}

	return lifecycleDeps, registryDeps, nil
}

func (s *Supervisor) resolveServiceDependencyRefs(
	controllers map[string]*Controller,
	sourceServiceID string,
	refs []string,
	blockUnresolved bool,
) ([]string, []string, error) {
	sourceID := registry.ParseID(sourceServiceID)
	services := make([]string, 0, len(refs))
	blockers := make([]string, 0)
	visiting := make(map[string]struct{})
	resolved := make(map[string]bool)

	var visit func(ref string) (bool, error)
	visit = func(ref string) (bool, error) {
		depID := normalizeDependencyRef(sourceID, ref)
		if depID == "" {
			return false, nil
		}

		if _, exists := controllers[depID]; exists {
			services = appendUniqueString(services, depID)
			return true, nil
		}

		if found, ok := resolved[depID]; ok {
			return found, nil
		}
		if _, ok := visiting[depID]; ok {
			return false, nil
		}

		visiting[depID] = struct{}{}
		foundService := false
		if s.dependencyResolver != nil {
			deps, err := s.dependencyResolver(registry.ParseID(depID))
			if err != nil {
				return false, err
			}
			for _, dep := range deps {
				found, err := visit(dependencyIDString(dep))
				if err != nil {
					return false, err
				}
				foundService = foundService || found
			}
		}
		delete(visiting, depID)
		resolved[depID] = foundService
		return foundService, nil
	}

	for _, ref := range refs {
		depID := normalizeDependencyRef(sourceID, ref)
		found, err := visit(depID)
		if err != nil {
			return nil, nil, NewDependencyResolveError(sourceServiceID, err)
		}
		if blockUnresolved && !found {
			blockers = appendUniqueString(blockers, depID)
		}
	}

	return services, blockers, nil
}

// execute processes the transaction by creating new services,
// stopping removed services, and starting auto-start services.
//
// All iterations of tx.register, tx.remove, and s.controllers traverse a
// pre-sorted slice of IDs. The supervisor feeds the sequencer in this order,
// so a single hash-seed seam used to leak into the boot ordering and surface
// as intermittent "filesystem not found"/"driver not found" rejections on
// services whose listener depends on resources that should have started first.
func (s *Supervisor) execute(ctx context.Context, tx *regTx) (err error) {
	registerIDs := sortedRegisterIDs(tx.register)
	removeIDs := sortedRemoveIDs(tx.remove)

	// Mutate controller registry under lock, then run potentially long transitions
	// lock-free so state readers are never blocked behind start/stop timeouts.
	created := make(map[string]*Controller, len(registerIDs))
	s.mu.Lock()
	for _, id := range registerIDs {
		entry := tx.register[id]
		if _, exists := s.controllers[id]; !exists {
			ctrl := NewController(s.ctx, entry.Service, entry.Config, s.createStateHandler(id))
			s.controllers[id] = ctrl
			created[id] = ctrl
		}
	}
	s.mu.Unlock()

	defer func() {
		cancellation := ctx.Err()
		if cancellation == nil || !errors.Is(err, cancellation) {
			return
		}
		for _, ctrl := range created {
			ctrl.cancelStart()
			_ = ctrl.Stop()
			ctrl.cancel()
		}
		s.mu.Lock()
		for id, ctrl := range created {
			if s.controllers[id] == ctrl {
				delete(s.controllers, id)
			}
		}
		s.mu.Unlock()
	}()

	controllers := s.snapshotControllers()
	var operations []operation

	// Queue stop operations for services being removed
	for _, id := range removeIDs {
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

	roots := make([]startRoot, 0, len(registerIDs))
	for _, id := range registerIDs {
		entry := tx.register[id]
		if entry.Config.AutoStart {
			roots = append(roots, startRoot{
				id:       id,
				required: entry.Config.StartupRequired(),
			})
		}
	}
	startOps, err := s.buildStartOperationsForRoots(controllers, roots)
	if err != nil {
		return NewStartOperationsError(err)
	}
	operations = append(operations, startOps...)

	// Execute transitions in dependency order
	if err := s.runTransition(ctx, operations); err != nil {
		return NewTransitionError(err)
	}

	// Done stopped services
	s.mu.Lock()
	for _, id := range removeIDs {
		delete(s.controllers, id)
	}
	s.mu.Unlock()

	return nil
}

func (s *Supervisor) buildStartOperations(
	controllers map[string]*Controller,
	serviceID string,
) ([]operation, error) {
	return s.buildStartOperationsForRoots(controllers, []startRoot{{
		id:       serviceID,
		required: true,
	}})
}

func (s *Supervisor) buildStartOperationsForRoots(
	controllers map[string]*Controller,
	roots []startRoot,
) ([]operation, error) {
	nodes := make(map[string]*operation)
	order := make([]string, 0, len(roots))
	processedRequired := make(map[string]bool)

	ensureNode := func(id string, ctrl *Controller, required bool) *operation {
		if op, exists := nodes[id]; exists {
			if required {
				op.optional = false
			}
			return op
		}

		op := &operation{
			kind:       opStart,
			id:         id,
			controller: ctrl,
			optional:   !required,
		}
		nodes[id] = op
		order = append(order, id)
		return op
	}

	var visitWithPolicy func(id string, required bool) error
	visitWithPolicy = func(id string, required bool) error {
		if seenRequired, seen := processedRequired[id]; seen && (seenRequired || !required) {
			return nil
		}

		ctrl, exists := controllers[id]
		if !exists {
			return NewServiceNotFoundError(id)
		}

		if ctrl.State().Status == supervisor.StatusRunning {
			processedRequired[id] = processedRequired[id] || required
			return nil
		}

		op := ensureNode(id, ctrl, required)
		processedRequired[id] = processedRequired[id] || required

		lifecycleDeps, registryDeps, err := s.resolveDependencySets(controllers, id)
		if err != nil {
			return err
		}

		lifecycleServices, lifecycleBlockers, err := s.resolveServiceDependencyRefs(controllers, id, lifecycleDeps, true)
		if err != nil {
			return err
		}
		registryServices, _, err := s.resolveServiceDependencyRefs(controllers, id, registryDeps, false)
		if err != nil {
			return err
		}
		op.blockers = append(op.blockers, lifecycleBlockers...)

		serviceDeps := make([]string, 0, len(lifecycleServices)+len(registryServices))
		for _, depID := range lifecycleServices {
			serviceDeps = appendUniqueString(serviceDeps, depID)
		}
		for _, depID := range registryServices {
			serviceDeps = appendUniqueString(serviceDeps, depID)
		}

		for _, depID := range serviceDeps {
			depCtrl, exists := controllers[depID]
			if !exists {
				op.blockers = appendUniqueString(op.blockers, depID)
				continue
			}
			if depCtrl.State().Status == supervisor.StatusRunning {
				continue
			}
			op.dependencies = appendUniqueString(op.dependencies, depID)
			if err := visitWithPolicy(depID, required); err != nil {
				return err
			}
		}

		return nil
	}

	for _, root := range roots {
		if err := visitWithPolicy(root.id, root.required); err != nil {
			return nil, err
		}
	}

	operations := make([]operation, 0, len(order))
	for _, id := range order {
		operations = append(operations, *nodes[id])
	}
	return operations, nil
}

func (s *Supervisor) buildStopOperations(
	controllers map[string]*Controller,
	serviceID string,
) ([]operation, error) {
	visited := make(map[string]bool)
	var operations []operation

	controllerIDs := sortedControllerIDs(controllers)
	dependedOnBy := make(map[string][]string)
	resolvedDeps := make(map[string][]string, len(controllers))
	for _, id := range controllerIDs {
		deps, err := s.resolveDependencies(controllers, id)
		if err != nil {
			return nil, err
		}

		validDeps := make([]string, 0, len(deps))
		for _, depID := range deps {
			if _, exists := controllers[depID]; !exists {
				continue
			}
			validDeps = append(validDeps, depID)
			dependedOnBy[depID] = append(dependedOnBy[depID], id)
		}
		resolvedDeps[id] = validDeps
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
				dependencies: resolvedDeps[id],
			})
		}
	}

	visit(serviceID)
	return operations, nil
}

func appendUniqueString(values []string, next string) []string {
	for _, value := range values {
		if value == next {
			return values
		}
	}
	return append(values, next)
}

// sortedRegisterIDs returns the keys of m sorted lexicographically. Used so
// the supervisor processes registrations in a stable order independent of the
// Go map iteration hash seed.
func sortedRegisterIDs(m map[string]*supervisor.Entry) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// sortedRemoveIDs returns the keys of m sorted lexicographically.
func sortedRemoveIDs(m map[string]struct{}) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// sortedControllerIDs returns the keys of m sorted lexicographically.
func sortedControllerIDs(m map[string]*Controller) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func normalizeDependencyRef(sourceID registry.ID, ref string) string {
	if ref == "" {
		return ""
	}
	depID := registry.ParseID(ref)
	if depID.NS == "" && sourceID.NS != "" {
		depID = depID.WithDefaultNS(sourceID.NS)
	}
	return dependencyIDString(depID)
}

func dependencyIDString(id registry.ID) string {
	if id.NS == "" {
		return id.Name
	}
	return id.String()
}
