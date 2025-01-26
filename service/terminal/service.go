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

type controlAction int

const (
	actionStart controlAction = iota
	actionStop
	actionUpdate
)

type controlOp struct {
	action   controlAction
	terminal api.Terminal
	id       registry.ID
	result   chan error
}

type operations struct {
	terminal *terminalRunner
	cfg      *api.ServiceConfig
	bus      events.Bus
	log      *zap.Logger
	csw      *logs.ConfigSwitcher
	statusCh chan<- any // Write-only channel
}

func newOperations(terminal *terminalRunner, bus events.Bus, log *zap.Logger, csw *logs.ConfigSwitcher, statusCh chan<- any) *operations {
	return &operations{
		terminal: terminal,
		bus:      bus,
		log:      log,
		csw:      csw,
		statusCh: statusCh,
	}
}

func (o *operations) handleStart(ctx context.Context) error {
	if err := o.csw.EnableTemporaryConfig(ctx, logsapi.Config{
		MinLevel:       zap.DebugLevel,
		StreamToEvents: true,
	}); err != nil {
		return err
	}

	if o.terminal != nil {
		if err := o.terminal.Start(ctx); err != nil {
			return err
		}
	}

	o.sendStatus("running")
	return nil
}

func (o *operations) handleStop(ctx context.Context) error {
	if o.terminal != nil {
		if err := o.terminal.stop(ctx); err != nil {
			return err
		}
	}
	o.sendStatus("stopped")
	return nil
}

func (o *operations) handleUpdate(ctx context.Context, newTerminal api.Terminal, id registry.ID) error {
	if o.terminal == nil {
		return errors.New("service not running")
	}

	newRunner := newTerminalRunner(newTerminal, id, o.bus, o.log)

	if err := o.terminal.stop(ctx); err != nil {
		return err
	}

	if err := o.terminal.transferState(newRunner); err != nil {
		return err
	}

	if err := newRunner.Start(ctx); err != nil {
		return err
	}

	o.terminal = newRunner
	o.sendStatus("terminal updated")
	return nil
}

// sendStatus attempts to send status update, non-blocking
func (o *operations) sendStatus(status any) {
	// Use non-blocking send to prevent deadlocks
	select {
	case o.statusCh <- status:
	default:
		o.log.Warn("failed to send status update", zap.Any("status", status))
	}
}

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
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *service) run(ctx context.Context) {
	defer func() {
		s.mu.Lock()
		s.ctx = nil
		// Close channels we own
		close(s.statusCh)
		close(s.opCh)
		close(s.done)
		s.mu.Unlock()

		s.csw.RestoreBaseConfig(context.Background())
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
				err = s.ops.handleStart(ctx)
			case actionStop:
				err = s.ops.handleStop(ctx)
				if err == nil {
					// After successful stop, exit the run loop
					op.result <- nil
					return
				}
			case actionUpdate:
				err = s.ops.handleUpdate(ctx, op.terminal, op.id)
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
