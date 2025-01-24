// Package supervisor provides service lifecycle management and supervision
// functionality for the Pony runtime environment. It handles service registration,
// state transitions, and failure recovery.
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ponyruntime/pony/api/events"
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
		serviceID string
		entry     *supervisor.Entry
	}

	// Supervisor manages the lifecycle of registered services, handling their
	// registration, startup, shutdown, and monitoring. It provides transaction
	// support for service state changes and integrates with the event system
	// for coordinated operations.
	Supervisor struct {
		ctx         context.Context
		bus         events.Bus
		subscriber  *eventbus.Subscriber
		logger      *zap.Logger
		mu          sync.RWMutex
		controllers map[string]*Controller
		actions     chan action
		wg          sync.WaitGroup
		tx          *registryTX
	}
)

// NewSupervisor creates a new Supervisor instance with the provided event bus
// and logger. The supervisor is initially inactive and must be started with
// the Start method.
func NewSupervisor(bus events.Bus, logger *zap.Logger) *Supervisor {
	return &Supervisor{
		bus:         bus,
		logger:      logger,
		controllers: make(map[string]*Controller),
		actions:     make(chan action, 100),
		tx:          newTransactionHelper(logger),
	}
}

// GetState returns the current state of a service identified by its ID.
// Returns an error if the service is not found.
func (s *Supervisor) GetState(id string) (State, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	controller, exists := s.controllers[id]
	if !exists {
		return State{}, fmt.Errorf("service %s not found", id)
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

// Stop gracefully shuts down the supervisor and all managed services.
// It ensures all services are properly stopped and resources are cleaned up.
func (s *Supervisor) Stop() error {
	s.logger.Info("stopping supervisor")

	if s.subscriber != nil {
		s.subscriber.Close()
		s.subscriber = nil
	}

	close(s.actions)
	s.wg.Wait()

	// stop all controllers
	s.mu.Lock()
	defer s.mu.Unlock()

	var wg sync.WaitGroup
	errCh := make(chan error, len(s.controllers))

	for id, controller := range s.controllers {
		wg.Add(1)
		go func(id string, c *Controller) {
			defer wg.Done()
			log.Printf("stopping controller %s", id)
			if err := c.Stop(); err != nil {
				s.logger.Error("failed to stop controller",
					zap.String("serviceID", id),
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

func (s *Supervisor) handleEvent(e events.Event) {
	if e.System == registry.System {
		switch e.Kind {
		case registry.Begin:
			s.actions <- action{kind: actionBegin}
		case registry.Commit:
			s.actions <- action{kind: actionCommit}
		case registry.Discard:
			s.actions <- action{kind: actionDiscard}
		}
		return
	}

	if e.System != supervisor.System {
		return
	}

	switch {
	case e.Kind == supervisor.Register:
		entry, ok := e.Data.(*supervisor.Entry)
		if !ok {
			s.logger.Error(
				"failed to decode registration entry",
				zap.String("event_path", string(e.Path)),
			)
			return
		}

		s.actions <- action{
			serviceID: string(e.Path),
			kind:      actionRegister,
			entry:     entry,
		}
	case e.Kind == supervisor.Remove:
		s.actions <- action{serviceID: string(e.Path), kind: actionRemove}

	case e.Kind == supervisor.Start:
		s.actions <- action{serviceID: string(e.Path), kind: actionStart}

	case e.Kind == supervisor.Stop:
		s.actions <- action{serviceID: string(e.Path), kind: actionStop}
	}
}

func (s *Supervisor) run(ctx context.Context) {
	defer s.wg.Done()

	s.ctx = ctx

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
				s.startPending()
			}

		case actionRegister:
			if err := s.tx.registerService(action.serviceID, action.entry); err != nil {
				s.logger.Error("failed to register service",
					zap.String("serviceID", string(action.serviceID)),
					zap.Error(err),
				)
			}

		case actionRemove:
			if err := s.tx.removeService(action.serviceID); err != nil {
				s.logger.Error("failed to remove service",
					zap.String("serviceID", action.serviceID),
					zap.Error(err),
				)
			} else {
				s.logger.Info("service removed",
					zap.String("serviceID", action.serviceID),
				)
			}

		case actionStart:
			err := s.startService(action.serviceID)
			if err != nil {
				s.logger.Error("failed to start service",
					zap.String("serviceID", action.serviceID),
					zap.Error(err),
				)
			} else {
				s.logger.Info("service started",
					zap.String("serviceID", action.serviceID),
				)
			}

		case actionStop:
			err := s.stopService(action.serviceID)
			if err != nil {
				s.logger.Error("failed to stop service",
					zap.String("serviceID", action.serviceID),
					zap.Error(err),
				)
			}
		}
	}
}

func (s *Supervisor) registerService(id string, entry *supervisor.Entry) error {
	s.mu.Lock()
	if _, exists := s.controllers[id]; exists {
		//return current.updateConfig(entry.Config) todo: implement later
		return fmt.Errorf("service %s already registered", id)
	}
	s.mu.Unlock()

	stateHandler := func(status supervisor.Status, details any) {
		if err, ok := details.(error); ok {
			if errors.Is(err, supervisor.ExitErr) {
				s.logger.Info(fmt.Sprintf("service %s is %s", id, status),
					zap.String("serviceID", id),
					zap.String("status", string(status)),
					zap.Error(err),
				)
			} else if errors.Is(err, supervisor.TerminatedErr) || errors.Is(err, context.Canceled) {
				s.logger.Warn(fmt.Sprintf("service %s is %s", id, status),
					zap.String("serviceID", id),
					zap.String("status", string(status)),
					zap.Error(err),
				)
			} else {
				s.logger.Error(fmt.Sprintf("service %s is %s", id, status),
					zap.String("serviceID", id),
					zap.String("status", string(status)),
					zap.Error(err),
				)
			}
		} else {
			s.logger.Info(fmt.Sprintf("service %s is %s", id, status),
				zap.String("serviceID", id),
				zap.String("status", string(status)),
				zap.Any("details", details),
			)
		}

		s.bus.Send(s.ctx, events.Event{
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

	entry.Config.InitDefaults()

	s.mu.Lock()
	s.controllers[id] = NewController(s.ctx, entry.Service, entry.Config, stateHandler)
	s.mu.Unlock()

	s.logger.Info("service registered",
		zap.String("serviceID", id),
		zap.Bool("auto_start", entry.Config.AutoStart),
	)

	return nil
}

func (s *Supervisor) removeService(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	controller, exists := s.controllers[id]
	if !exists {
		return fmt.Errorf("service %s not found", id)
	}

	delete(s.controllers, id)

	if err := controller.Stop(); err != nil {
		s.logger.Warn("failed to stop service during removal, detaching",
			zap.String("serviceID", id),
			zap.Error(err),
		)
		return err
	}

	s.logger.Info("service removed", zap.String("serviceID", id))

	return nil
}

func (s *Supervisor) startPending() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for id, controller := range s.controllers {
		state := controller.State()
		if state.Desired == supervisor.Running || (!controller.config.AutoStart && state.Desired != supervisor.Running) {
			continue
		}

		s.logger.Info("starting service",
			zap.String("serviceID", id),
		)

		if err := controller.Start(); err != nil {
			s.logger.Error("failed to start service",
				zap.String("serviceID", id),
				zap.Error(err),
			)
		}
	}
}

func (s *Supervisor) startService(id string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	controller, exists := s.controllers[id]
	if !exists {
		return fmt.Errorf("service %s not found", id)
	}

	return controller.Start()
}

func (s *Supervisor) stopService(id string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	controller, exists := s.controllers[id]
	if !exists {
		return fmt.Errorf("service %s not found", id)
	}

	return controller.Stop()
}
