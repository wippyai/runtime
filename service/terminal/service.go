package terminal

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/service/logs"
	api "github.com/ponyruntime/pony/api/service/terminal"
)

var (
	instance          *service
	instanceLock      sync.Mutex
	ErrAlreadyRunning = fmt.Errorf("terminal service already running")
)

type service struct {
	terminal      api.Terminal
	options       api.Options
	id            registry.ID
	lifecycle     *appLifecycle
	mu            sync.Mutex
	baseLogConfig *logs.Config
}

func newService(app api.Terminal, opts api.Options, id registry.ID, timeouts api.TimeoutConfig, bus events.Bus) *service {
	return &service{
		terminal:  app,
		options:   opts,
		id:        id,
		lifecycle: newAppLifecycle(bus, timeouts),
	}
}

func (s *service) redirectLogging(ctx context.Context) error {
	// First, get current config
	respChan := make(chan events.Event, 1)
	subID, err := s.lifecycle.bus.SubscribeP(ctx, logs.System, logs.ConfigStateEvent, respChan)
	if err != nil {
		return fmt.Errorf("failed to subscribe to log config: %w", err)
	}
	defer s.lifecycle.bus.Unsubscribe(ctx, subID)

	// Request current config
	s.lifecycle.bus.Send(ctx, events.Event{
		System: logs.System,
		Kind:   logs.GetConfigEvent,
		Data: &logs.ConfigRequest{
			ResponsePath: "terminal.logs",
		},
	})

	// Wait for response with timeout
	select {
	case resp := <-respChan:
		if cfg, ok := resp.Data.(logs.ConfigResponse); ok {
			s.baseLogConfig = &cfg.Config
		}
	case <-time.After(s.lifecycle.timeouts.StartTimeout):
		return fmt.Errorf("timeout waiting for log config")
	}

	// Now set our terminal logging config
	s.lifecycle.bus.Send(ctx, events.Event{
		System: logs.System,
		Kind:   logs.SetConfigEvent,
		Data: logs.Config{
			PropagateDownstream: false,
			StreamToEvents:      true,
			MinLevel:            s.baseLogConfig.MinLevel, // Preserve original level
		},
	})

	return nil
}

func (s *service) restoreLogging(ctx context.Context) {
	if s.baseLogConfig != nil {
		s.lifecycle.bus.Send(ctx, events.Event{
			System: logs.System,
			Kind:   logs.SetConfigEvent,
			Data:   *s.baseLogConfig,
		})
	}
}

func (s *service) Start(ctx context.Context) (<-chan any, error) {
	instanceLock.Lock()
	if instance != nil {
		instanceLock.Unlock()
		return nil, ErrAlreadyRunning
	}
	instance = s
	instanceLock.Unlock()

	// Setup logging redirection
	if err := s.redirectLogging(ctx); err != nil {
		s.clearInstance()
		return nil, fmt.Errorf("failed to setup logging: %w", err)
	}

	if err := s.lifecycle.Start(ctx, s.terminal, s.options, s.id); err != nil {
		s.restoreLogging(ctx)
		s.clearInstance()
		return nil, fmt.Errorf("failed to start terminal: %w", err)
	}

	return s.lifecycle.Status(), nil
}

func (s *service) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lifecycle.Release(ctx)
	s.restoreLogging(ctx)
	s.clearInstance()
	return nil
}

func (s *service) UpdateApp(ctx context.Context, term api.Terminal, opts api.Options, id registry.ID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.lifecycle.Update(ctx, term, opts, id); err != nil {
		// Since service will die on update failure, restore logging first
		s.restoreLogging(ctx)

		// Then cleanup and return error
		select {
		case status := <-s.lifecycle.Status():
			s.clearInstance()
			return fmt.Errorf("failed to update terminal: %v", status)
		default:
			s.clearInstance()
			return fmt.Errorf("failed to update terminal: %w", err)
		}
	}

	s.terminal = term
	s.options = opts
	s.id = id

	return nil
}

func (s *service) Terminate() {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), s.lifecycle.timeouts.StopTimeout)
	defer cancel()

	s.lifecycle.setState(appStateTerminating)

	if instance, ok := s.lifecycle.Current(); ok {
		instance.cancel()

		if err := instance.terminal.Close(ctx); err != nil {
			select {
			case status := <-s.lifecycle.Status():
				_ = fmt.Errorf("failed to terminate terminal: %v", status)
			default:
				_ = fmt.Errorf("failed to terminate terminal: %w", err)
			}
		}
	}

	s.lifecycle.Release(ctx)
	s.restoreLogging(ctx)
	s.clearInstance()
}

func (s *service) clearInstance() {
	instanceLock.Lock()
	if instance == s {
		instance = nil
	}
	instanceLock.Unlock()
}
