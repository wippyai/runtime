package runtime

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/runtime"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/pkg/eventbus"
	"go.uber.org/zap"
)

// Service manages run functions and libraries based on registry configuration
type Service struct {
	ctx        context.Context
	log        *zap.Logger
	bus        events.Bus
	dtt        payload.Transcoder
	subscriber *eventbus.Subscriber
	run        runtime.Runtime
	mu         sync.RWMutex

	// Track registered functions and libraries
	functions map[registry.ID]*runtime.FunctionConfig
	libraries map[registry.ID]*runtime.LibraryConfig
}

// Init creates a new run service instance
func Init(
	bus events.Bus,
	run runtime.Runtime,
	dtt payload.Transcoder,
	logger *zap.Logger,
) *Service {
	return &Service{
		log:       logger,
		bus:       bus,
		dtt:       dtt,
		run:       run,
		functions: make(map[registry.ID]*runtime.FunctionConfig),
		libraries: make(map[registry.ID]*runtime.LibraryConfig),
	}
}

// Start begins listening for registry events
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ctx != nil {
		return fmt.Errorf("runtime configurer service already started")
	}

	s.ctx = ctx
	sub, err := eventbus.NewSubscriber(
		ctx,
		s.bus,
		registry.System,
		registry.Changes,
		s.processEvent,
	)

	if err != nil {
		return fmt.Errorf("failed to create subscriber: %w", err)
	}
	s.subscriber = sub
	return nil
}

// Stop gracefully shuts down the service and stops listening for events
func (s *Service) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.subscriber != nil {
		s.subscriber.Close()
	}

	s.functions = make(map[registry.ID]*runtime.FunctionConfig)
	s.libraries = make(map[registry.ID]*runtime.LibraryConfig)
	s.subscriber = nil

	return nil
}

func (s *Service) processEvent(evt events.Event) {
	entry, ok := evt.Data.(registry.Entry)
	if !ok {
		s.log.Error("invalid registry event data", zap.Any("event", evt))
		return
	}

	s.log.Debug("processing registry event",
		zap.String("id", string(entry.ID)),
		zap.String("kind", string(evt.Kind)),
		zap.String("type", string(entry.Kind)))

	// For create/update operations, ensure we have valid data
	if evt.Kind != registry.Delete && entry.Data == nil {
		s.reject(entry.ID, fmt.Errorf("configuration data is required for create/update operations"))
		return
	}

	switch entry.Kind {
	case runtime.KindFunction:
		cfg := new(runtime.FunctionConfig)
		if entry.Data != nil {
			if err := s.unmarshalAndValidate(entry.Data, cfg); err != nil {
				s.reject(entry.ID, err)
				return
			}
		}
		s.handleFunction(entry.ID, evt.Kind, cfg)

	case runtime.KindLibrary:
		cfg := new(runtime.LibraryConfig)
		if entry.Data != nil {
			if err := s.unmarshalAndValidate(entry.Data, cfg); err != nil {
				s.reject(entry.ID, err)
				return
			}
		}
		s.handleLibrary(entry.ID, evt.Kind, cfg)
	}
}

func (s *Service) unmarshalAndValidate(data payload.Payload, cfg interface{}) error {
	if err := s.dtt.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if validator, ok := cfg.(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return fmt.Errorf("invalid configuration: %w", err)
		}
	}

	return nil
}

func (s *Service) handleFunction(id registry.ID, kind events.Kind, cfg *runtime.FunctionConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch kind {
	case registry.Create:
		if _, exists := s.functions[id]; exists {
			s.reject(id, fmt.Errorf("function %s already exists", id))
			return
		}

		if err := s.run.AddFunction(id, *cfg); err != nil {
			s.reject(id, fmt.Errorf("failed to add function: %w", err))
			return
		}

		s.functions[id] = cfg

	case registry.Update:
		if _, exists := s.functions[id]; !exists {
			s.reject(id, fmt.Errorf("function %s not found", id))
			return
		}

		if err := s.run.UpdateFunction(id, *cfg); err != nil {
			s.reject(id, fmt.Errorf("failed to update function: %w", err))
			return
		}

		s.functions[id] = cfg

	case registry.Delete:
		if _, exists := s.functions[id]; !exists {
			s.reject(id, fmt.Errorf("function %s not found", id))
			return
		}

		if err := s.run.Delete(id); err != nil {
			s.reject(id, fmt.Errorf("failed to delete function: %w", err))
			return
		}

		delete(s.functions, id)
	}

	s.accept(id)
}

func (s *Service) handleLibrary(id registry.ID, kind events.Kind, cfg *runtime.LibraryConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch kind {
	case registry.Create:
		if _, exists := s.libraries[id]; exists {
			s.reject(id, fmt.Errorf("library %s already exists", id))
			return
		}

		if err := s.run.AddLibrary(id, *cfg); err != nil {
			s.reject(id, fmt.Errorf("failed to add library: %w", err))
			return
		}

		s.libraries[id] = cfg

	case registry.Update:
		if _, exists := s.libraries[id]; !exists {
			s.reject(id, fmt.Errorf("library %s not found", id))
			return
		}

		if err := s.run.UpdateLibrary(id, *cfg); err != nil {
			s.reject(id, fmt.Errorf("failed to update library: %w", err))
			return
		}

		s.libraries[id] = cfg

	case registry.Delete:
		if _, exists := s.libraries[id]; !exists {
			s.reject(id, fmt.Errorf("library %s not found", id))
			return
		}

		if err := s.run.Delete(id); err != nil {
			s.reject(id, fmt.Errorf("failed to delete library: %w", err))
			return
		}

		delete(s.libraries, id)
	}

	s.accept(id)
}

func (s *Service) accept(id registry.ID) {
	s.bus.Send(s.ctx, events.Event{
		System: registry.System,
		Kind:   registry.Accept,
		Path:   events.Path(id),
	})
}

func (s *Service) reject(id registry.ID, err error) {
	s.bus.Send(s.ctx, events.Event{
		System: registry.System,
		Kind:   registry.Reject,
		Path:   events.Path(id),
		Data:   err,
	})
}
