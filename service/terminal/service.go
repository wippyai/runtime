package terminal

import (
	"context"
	"errors"
	"fmt"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/service/terminal"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/service/logs"
	"go.uber.org/zap"
	"sync"
)

/*
*

	                 goroutine 195 [running]:
	                                                        github.com/ponyruntime/pony/service/terminal.(*service).sendStatus(0xc000165570, {0xaa6880, 0xc0021d67c0})
	                                           /mnt/d/Projects/pony/service/terminal/service.go:235 +0x56
	                                                                                                     github.com/ponyruntime/pony/service/terminal.(*service).run.func1()
	                                                   /mnt/d/Projects/pony/service/terminal/service.go:147 +0x85
	                                                                                                             panic({0xaa38e0?, 0xc2f9e0?})
	                   /home/wolfy-j/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.23.4.linux-amd64/src/runtime/panic.go:785 +0x132
	github.com/ponyruntime/pony/service/terminal.(*service).sendStatus(0xc000165570, {0xa948a0, 0xc0009e6290})
	                                                                                                           /mnt/d/Projects/pony/service/terminal/service.go:235 +0x56
	                                         github.com/ponyruntime/pony/service/terminal.(*service).run(0xc000165570, {0xc37410, 0xc000f9a780})
	                   /mnt/d/Projects/pony/service/terminal/service.go:217 +0x587
	                                                                              created by github.com/ponyruntime/pony/service/terminal.(*service).Start in goroutine 194
	                                                   /mnt/d/Projects/pony/service/terminal/service.go:72 +0x205
	                                                                                                             exit status 2
*/
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

type service struct {
	terminal  *terminalRunner
	ctx       context.Context
	opCh      chan controlOp
	statusCh  chan any
	doneCh    chan struct{}
	bus       events.Bus
	log       *zap.Logger
	logSwitch *logs.LogSwitcher
	timeouts  api.TimeoutConfig
	mu        sync.Mutex
}

func newService(
	app api.Terminal,
	id registry.ID,
	timeouts api.TimeoutConfig,
	bus events.Bus,
	log *zap.Logger,
) *service {
	return &service{
		terminal:  newTerminalRunner(app, id, bus, log),
		opCh:      make(chan controlOp, 1),
		bus:       bus,
		log:       log,
		logSwitch: logs.NewLogSwitcher(bus, log),
		timeouts:  timeouts,
	}
}

func (s *service) Start(ctx context.Context) (<-chan any, error) {
	s.mu.Lock()
	if s.ctx != nil {
		s.mu.Unlock()
		return nil, errors.New("service already running")
	}
	s.mu.Unlock()

	s.ctx = ctx
	s.statusCh = make(chan any, 10)
	s.doneCh = make(chan struct{})

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
				return nil, err
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
		s.ctx = nil
		s.logSwitch.RestoreOn(context.Background())

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

		case op := <-s.opCh:
			var err error

			switch op.action {
			case actionStart:
				// todo: make configurable
				if err = s.logSwitch.EnableOn(ctx); err != nil {
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

				newRunner := newTerminalRunner(op.terminal, op.id, s.bus, s.log)

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

			op.result <- err
			if err != nil {
				s.sendStatus(err)
				return
			}

		case <-s.terminal.wait():
			err := s.terminal.exitErr
			if errors.Is(err, supervisor.ErrTerminated) || errors.Is(err, supervisor.ErrExit) {
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
