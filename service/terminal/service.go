package terminal

import (
	"context"
	"errors"
	"fmt"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/service/terminal"
	"github.com/ponyruntime/pony/api/supervisor"
	"go.uber.org/zap"
	"log"
	"sync"
)

type serviceAction struct {
	actionType string
	terminal   api.Terminal
	options    api.Options
	id         registry.ID
	result     chan error
}

const (
	actionStart  = "start"
	actionStop   = "stop"
	actionUpdate = "update"
)

type service struct {
	terminal  *terminalRunner
	actionCh  chan serviceAction
	statusCh  chan any
	doneCh    chan struct{}
	bus       events.Bus
	log       *zap.Logger
	logSwitch *logSwitcher
	timeouts  api.TimeoutConfig
	mu        sync.Mutex
}

func newService(
	app api.Terminal,
	opts api.Options,
	id registry.ID,
	timeouts api.TimeoutConfig,
	bus events.Bus,
	log *zap.Logger,
) *service {
	return &service{
		terminal:  newTerminalRunner(app, opts, id, bus, log),
		actionCh:  make(chan serviceAction),
		statusCh:  make(chan any, 10),
		doneCh:    make(chan struct{}),
		bus:       bus,
		log:       log,
		logSwitch: newLogSwitcher(bus, log),
		timeouts:  timeouts,
	}
}

func (s *service) Start(ctx context.Context) (<-chan any, error) {
	resultCh := make(chan error, 1)
	select {
	case s.actionCh <- serviceAction{
		actionType: actionStart,
		result:     resultCh,
	}:
		select {
		case err := <-resultCh:
			if err != nil {
				return nil, err
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}

		log.Printf("STARTED!")

		return s.statusCh, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *service) Stop(ctx context.Context) error {
	resultCh := make(chan error, 1)
	select {
	case s.actionCh <- serviceAction{
		actionType: actionStop,
		result:     resultCh,
	}:
		select {
		case err := <-resultCh:
			if err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *service) UpdateApp(ctx context.Context, term api.Terminal, opts api.Options, id registry.ID) error {
	resultCh := make(chan error, 1)
	select {
	case s.actionCh <- serviceAction{
		actionType: actionUpdate,
		terminal:   term,
		options:    opts,
		id:         id,
		result:     resultCh,
	}:
		select {
		case err := <-resultCh:
			if err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *service) run(ctx context.Context) {
	defer func() {
		s.logSwitch.restore(context.Background())

		// Ensure last runner error is sent before closing channels
		if s.terminal != nil {
			if err := s.terminal.exitErr; err != nil {
				s.sendStatus(err)
			}
		}
		close(s.statusCh)
		close(s.doneCh)
	}()

	for {
		select {
		case <-ctx.Done():
			if s.terminal != nil {
				if err := s.terminal.stop(ctx); err != nil {
					s.log.Warn("failed to stop terminal", zap.Error(err))
				}
			}
			return

		case action := <-s.actionCh:
			var err error

			switch action.actionType {
			case actionStart:
				if err = s.logSwitch.enable(ctx); err != nil {
					break
				}
				if s.terminal != nil {
					err = s.terminal.Start(ctx)
				}
				if err == nil {
					s.sendStatus("running")
				}

			case actionStop:
				if s.terminal != nil {
					err = s.terminal.stop(ctx)
				}
				if err == nil {
					s.sendStatus("stopped")
				}

			case actionUpdate:
				if s.terminal == nil {
					err = errors.New("service not running")
					break
				}

				newRunner := newTerminalRunner(action.terminal, action.options, action.id, s.bus, s.log)

				// Stop current terminal
				if err = s.terminal.stop(ctx); err != nil {
					break
				}

				// Transfer state if possible
				if err = s.terminal.transferState(newRunner); err != nil {
					break
				}

				// Start new terminal
				if err = newRunner.Start(ctx); err != nil {
					break
				}

				s.terminal = newRunner
				s.sendStatus("terminal updated")
			}

			action.result <- err
			if err != nil {
				s.sendStatus(err)
				return
			}

		case <-s.terminal.wait():
			err := s.terminal.exitErr
			if errors.Is(err, supervisor.TerminatedErr) || errors.Is(err, supervisor.ExitErr) {
				s.sendStatus(err)
			} else {
				s.sendStatus(fmt.Errorf("terminal failed: %w", err))
			}
			return
		}
	}
}

func (s *service) sendStatus(status any) {
	select {
	case s.statusCh <- status:
	default:
		s.log.Warn("failed to send status update", zap.Any("status", status))
	}
}
