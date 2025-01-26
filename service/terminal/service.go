package terminal

import (
	"context"
	"errors"
	"fmt"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
	logsapi "github.com/ponyruntime/pony/api/service/logs"
	api "github.com/ponyruntime/pony/api/service/terminal"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/service/logs"
	"go.uber.org/zap"
	"sync"
)

type service struct {
	terminal *terminalRunner
	mu       sync.Mutex
	ctx      context.Context
	opCh     chan controlOp
	statusCh chan any
	bus      events.Bus
	log      *zap.Logger
	cfg      *api.ServiceConfig
	csw      *logs.ConfigSwitcher
	ops      *operations
	done     chan struct{} // Signal for complete shutdown
}

func newService(
	app api.Terminal,
	id registry.ID,
	cfg *api.ServiceConfig,
	bus events.Bus,
	log *zap.Logger,
) *service {
	return &service{
		terminal: newTerminalRunner(app, id, bus, log),
		opCh:     make(chan controlOp, 1),
		bus:      bus,
		log:      log,
		cfg:      cfg,
		csw:      logs.NewConfigSwitcher(bus, log),
		done:     make(chan struct{}),
	}
}

func (s *service) Start(ctx context.Context) (<-chan any, error) {
	s.mu.Lock()
	if s.ctx != nil {
		s.mu.Unlock()
		return nil, errors.New("service already running")
	}

	s.ctx = ctx
	s.statusCh = make(chan any, 10)
	s.ops = newOperations(s.terminal, s.bus, s.log, s.csw, s.statusCh)
	s.mu.Unlock()

	go s.run(ctx)

	resultCh := make(chan error, 1)
	select {
	case s.opCh <- controlOp{
		action: actionStart,
		result: resultCh,
	}:
		select {
		case err := <-resultCh:
			if err != nil {
				return nil, fmt.Errorf("failed to start service: %w", err)
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return s.statusCh, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *service) Stop(ctx context.Context) error {
	resultCh := make(chan error, 1)
	select {
	case s.opCh <- controlOp{
		action: actionStop,
		result: resultCh,
	}:
		select {
		case err := <-resultCh:
			if err != nil {
				return fmt.Errorf("failed to stop service: %w", err)
			}
		case <-ctx.Done():
			return ctx.Err()
		}

		// Wait for complete shutdown
		select {
		case <-s.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	case <-s.done:
		return errors.New("service is not running")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *service) UpdateApp(ctx context.Context, term api.Terminal, id registry.ID) error {
	resultCh := make(chan error, 1)
	select {
	case s.opCh <- controlOp{
		action:   actionUpdate,
		terminal: term,
		id:       id,
		result:   resultCh,
	}:
		select {
		case err := <-resultCh:
			if err != nil {
				return fmt.Errorf("failed to update service: %w", err)
			}
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	case <-s.done:
		return errors.New("service is not running")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *service) run(ctx context.Context) {
	defer func() {
		s.mu.Lock()
		s.ctx = nil
		close(s.statusCh)
		close(s.done)
		s.mu.Unlock()

		s.csw.RestoreBaseConfig(ctx)
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

		case op := <-s.opCh:
			var err error

			switch op.action {
			case actionStart:
				if err := s.redirectLogs(ctx); err != nil {
					op.result <- err
					s.ops.sendStatus(err)
					return
				}
				err = s.ops.handleStart(ctx)
			case actionStop:
				err = s.ops.handleStop(ctx)
				s.csw.RestoreBaseConfig(ctx)
				if err == nil {
					// After successful stop, exit the run loop
					op.result <- nil
					return
				}
			case actionUpdate:
				s.csw.RestoreBaseConfig(ctx)
				err = s.ops.handleUpdate(ctx, op.terminal, op.id)
				if err := s.redirectLogs(ctx); err != nil {
					err = fmt.Errorf("updated but, failed to redirect logs: %w", err)
				}
			}

			op.result <- err
			if err != nil {
				s.ops.sendStatus(err)
				return
			}

		case <-s.terminal.wait():
			err := s.terminal.exitErr
			if errors.Is(err, supervisor.ErrTerminated) || errors.Is(err, supervisor.ErrExit) {
				s.ops.sendStatus(err)
			} else {
				s.ops.sendStatus(fmt.Errorf("terminal failed: %w", err))
			}
			return
		}
	}
}

func (s *service) redirectLogs(ctx context.Context) error {
	if s.cfg.HideLogs {
		return s.csw.EnableTemporaryConfig(ctx, logsapi.Config{
			MinLevel:       zap.DebugLevel,
			StreamToEvents: true,
		})
	}
	return nil
}
