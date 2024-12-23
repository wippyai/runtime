package supervisor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/pkg/eventbus"
	"go.uber.org/zap"
)

const (
	actionRegister actionType = iota
	actionRemove
	actionStart
	actionStop
	actionBegin
	actionCommit
	actionDiscard
)

type (
	actionType int

	action struct {
		kind      actionType
		serviceID registry.ID
		entry     *supervisor.Entry
	}

	Services map[string]State

	Supervisor struct {
		bus         events.Bus
		subscriber  *eventbus.Subscriber
		logger      *zap.Logger
		mu          sync.RWMutex
		controllers map[registry.ID]*Controller
		actions     chan action
		wg          sync.WaitGroup
		tx          *registryTX
	}
)

// NewSupervisor creates a new Supervisor instance
func NewSupervisor(bus events.Bus, logger *zap.Logger) *Supervisor {
	return &Supervisor{
		bus:         bus,
		logger:      logger,
		controllers: make(map[registry.ID]*Controller),
		actions:     make(chan action, 100),
		tx:          newTransactionHelper(logger),
	}
}

// Services returns a map of all service states indexed by service ID
func (s *Supervisor) Services() Services {
	s.mu.RLock()
	defer s.mu.RUnlock()

	states := make(Services)
	for id, controller := range s.controllers {
		states[string(id)] = controller.State()
	}

	return states
}

// Start initializes the supervisor and starts listening for events
func (s *Supervisor) Start(ctx context.Context) error {
	// Subscribe to all relevant events using a single subscriber with patterns
	sub, err := eventbus.NewSubscriber(
		ctx,
		s.bus,
		"(registry|supervisor)",
		"*",
		s.handleEvent,
	)

	if err != nil {
		return fmt.Errorf("failed to create event subscriber: %w", err)
	}
	s.subscriber = sub

	// Start main control loop
	s.wg.Add(1)
	go s.run(ctx)

	s.logger.Info("supervisor started")

	return nil
}

func (s *Supervisor) handleEvent(e events.Event) {
	switch {
	case e.System == supervisor.System && e.Kind == supervisor.Register:
		// Register new service for supervision
		entry, ok := e.Data.(*supervisor.Entry)
		if !ok {
			s.logger.Error(
				"failed to decode registration entry",
				zap.String("event_path", string(e.Path)),
			)
			return
		}

		s.actions <- action{
			serviceID: registry.ID(e.Path),
			kind:      actionRegister,
			entry:     entry,
		}
	case e.System == supervisor.System && e.Kind == supervisor.Remove:
		// Remove service from supervision
		s.actions <- action{serviceID: registry.ID(e.Path), kind: actionRemove}
	case e.System == registry.System:
		// Manage system configuration state
		switch e.Kind {
		case registry.Begin:
			s.actions <- action{kind: actionBegin}
		case registry.Commit:
			s.actions <- action{kind: actionCommit}
		case registry.Discard:
			s.actions <- action{kind: actionDiscard}
		}
	}
}

func (s *Supervisor) run(ctx context.Context) {
	defer s.wg.Done()

	for action := range s.actions {
		switch action.kind {
		case actionBegin:
			s.tx.begin()

		case actionDiscard:
			s.tx.discard()

		case actionCommit:
			if err := s.tx.commit(s.removeService, s.registerService); err != nil {
				s.logger.Error("failed to commit transaction", zap.Error(err))
			} else {
				s.startPendingServices()
			}

		case actionRegister:
			if err := s.tx.registerService(action.serviceID, action.entry); err != nil {
				s.logger.Error("failed to register service in transaction",
					zap.String("serviceID", string(action.serviceID)),
					zap.Error(err),
				)
			} else {
				s.logger.Info("service registered",
					zap.String("serviceID", string(action.serviceID)),
				)
			}

		case actionRemove:
			if err := s.tx.removeService(action.serviceID); err != nil {
				s.logger.Error("failed to remove service in transaction",
					zap.String("serviceID", string(action.serviceID)),
					zap.Error(err),
				)
			} else {
				s.logger.Info("service removed",
					zap.String("serviceID", string(action.serviceID)),
				)
			}

		case actionStart:
			err := s.startService(action.serviceID)
			if err != nil {
				s.logger.Error("failed to start service in transaction",
					zap.String("serviceID", string(action.serviceID)),
					zap.Error(err),
				)
			} else {
				s.logger.Info("service started",
					zap.String("serviceID", string(action.serviceID)),
				)
			}

		case actionStop:
			err := s.stopService(action.serviceID)
			if err != nil {
				s.logger.Error("failed to stop service",
					zap.String("serviceID", string(action.serviceID)),
					zap.Error(err),
				)
			}
		}
	}
}

func (s *Supervisor) registerService(id registry.ID, entry *supervisor.Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if current, exists := s.controllers[id]; exists {
		return current.updateConfig(entry.Config)
	}

	stateHandler := func(status supervisor.Status, details payload.Payload) {
		s.logger.Info("service state update",
			zap.String("serviceID", string(id)),
			zap.String("status", string(status)),
		)

		s.bus.Send(context.Background(), events.Event{
			System: supervisor.System,
			Path:   events.Path(id),
			Kind:   supervisor.Update,
			Data: State{
				Status:     status,
				Details:    details,
				Desired:    status,
				RetryCount: 0,
				LastUpdate: time.Now(),
			},
		})
	}

	controller := NewController(
		context.Background(),
		entry.Service,
		entry.Config,
		stateHandler,
	)

	s.controllers[id] = controller
	s.logger.Info("service registered",
		zap.String("serviceID", string(id)),
		zap.Bool("auto_start", entry.Config.AutoStart),
	)

	return nil
}

func (s *Supervisor) removeService(id registry.ID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	controller, exists := s.controllers[id]
	if !exists {
		return fmt.Errorf("service %s not found", id)
	}

	if err := controller.Stop(); err != nil {
		s.logger.Error("failed to stop service during removal",
			zap.String("serviceID", string(id)),
			zap.Error(err),
		)
		return err
	}

	delete(s.controllers, id)
	s.logger.Info("service removed", zap.String("serviceID", string(id)))

	return nil
}

func (s *Supervisor) startPendingServices() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for id, controller := range s.controllers {
		if state := controller.State(); state.Desired == supervisor.Running {
			continue
		}

		s.logger.Info("starting service from tx",
			zap.String("serviceID", string(id)), // Log service ID correctly
		)

		if err := controller.Start(); err != nil {
			s.logger.Error("failed to start tx service",
				zap.String("serviceID", string(id)), // Log service ID correctly
				zap.Error(err),
			)
		}
	}
}

// GetServiceState returns the current state of a service
func (s *Supervisor) GetServiceState(id registry.ID) (State, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	controller, exists := s.controllers[id]
	if !exists {
		return State{}, fmt.Errorf("service %s not found", id)
	}

	return controller.State(), nil
}

func (s *Supervisor) startService(id registry.ID) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	controller, exists := s.controllers[id]
	if !exists {
		return fmt.Errorf("service %s not found", id)
	}

	return controller.Start()
}

func (s *Supervisor) stopService(id registry.ID) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	controller, exists := s.controllers[id]
	if !exists {
		return fmt.Errorf("service %s not found", id)
	}

	return controller.Stop()
}

// Stop gracefully shuts down the supervisor and all managed services
func (s *Supervisor) Stop(ctx context.Context) error {
	s.logger.Info("stopping supervisor")

	if s.subscriber != nil {
		s.subscriber.Close()
	}

	close(s.actions)
	s.wg.Wait()

	// Stop all controllers
	s.mu.Lock()
	defer s.mu.Unlock()

	var wg sync.WaitGroup
	errCh := make(chan error, len(s.controllers))

	for id, controller := range s.controllers {
		wg.Add(1)
		go func(id registry.ID, c *Controller) { // Use registry.ID
			defer wg.Done()
			if err := c.Stop(); err != nil {
				s.logger.Error("failed to stop controller",
					zap.String("serviceID", string(id)), // Log service ID correctly
					zap.Error(err),
				)
				errCh <- fmt.Errorf("failed to stop controller %s: %w", id, err)
			}
		}(id, controller)
	}

	wg.Wait()
	close(errCh)

	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to stop some controllers: %v", errs)
	}

	s.logger.Info("supervisor stopped")
	return nil
}
