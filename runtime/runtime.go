package runtime

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/pkg/eventbus"
	"go.uber.org/zap"
)

const (
	kindFunction registry.Kind = "function"
)

// Service manages runtime functions based on registry configuration
type Service struct {
	ctx        context.Context
	log        *zap.Logger
	bus        events.Bus
	dtt        payload.Transcoder
	subscriber *eventbus.Subscriber
	mu         sync.RWMutex
}

// Init creates a new runtime service instance
func Init(bus events.Bus, dtt payload.Transcoder, logger *zap.Logger) *Service {
	return &Service{
		log: logger,
		bus: bus,
		dtt: dtt,
	}
}

// Start begins listening for registry events
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

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

	s.subscriber = nil

	return nil
}

func (s *Service) processEvent(evt events.Event) {
	entry, ok := evt.Data.(registry.Entry)
	if !ok {
		s.log.Error("invalid registry event data", zap.Any("event", evt))
		return
	}

	if entry.Kind != kindFunction {
		return // ignore non-function events
	}

	//var cfg FunctionConfig
	//if err := s.dtt.Unmarshal(entry.Data, &cfg); err != nil {
	//	s.log.Error("failed to unmarshal function config",
	//		zap.String("function_id", string(entry.ID)),
	//		zap.Error(err),
	//	)
	//	return
	//}

	// Always accept the function registration
	//	s.registerFunction(entry.ID, cfg)

	s.log.Info("registered function",
		zap.String("function_id", string(entry.ID)),
	)

	s.sendAcceptance(entry)
}

//
//func (s *Service) registerFunction(id registry.ID, cfg FunctionConfig) {
//	s.mu.Lock()
//	defer s.mu.Unlock()
//
//	s.functions[id] = &Function{
//		ID:     id,
//		Config: cfg,
//	}
//
//	s.log.Info("registered function",
//		zap.String("function_id", string(id)),
//		zap.String("name", cfg.Name),
//		zap.String("runtime", cfg.Runtime),
//	)
//}

func (s *Service) sendAcceptance(entry registry.Entry) {
	s.bus.Send(s.ctx, events.Event{
		System: registry.System,
		Kind:   registry.Accept,
		Data:   entry,
	})
}
