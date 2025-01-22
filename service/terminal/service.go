package terminal

import (
	"context"
	"errors"
	"github.com/ponyruntime/pony/api/service/terminal"
	"os"
	"sync"
	"sync/atomic"
)

// we only allow single terminal instance per node
var running atomic.Pointer[service]

// service wraps an Options to implement the supervisor.Service interface
type service struct {
	terminal terminal.Terminal
	options  terminal.Options
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	logger   *LoggerInterceptor
}

// newService creates a new service instance
func newService(
	terminal terminal.Terminal,
	options terminal.Options,
	logger *LoggerInterceptor,
) *service {
	return &service{
		terminal: terminal,
		options:  options,
		logger:   logger,
	}
}

// Start implements supervisor.Service interface
func (s *service) Start(ctx context.Context) (<-chan any, error) {
	if !running.CompareAndSwap(nil, s) {
		return nil, errors.New("terminal is already running")
	}

	// we do not want to display logs in terminal, todo: probably use simpler interface
	s.logger.SetTerminalActive(true)
	// todo: can we combine with active flag or migration process between versions?

	// Create status channel and context with cancellation
	statusChan := make(chan any, 1)
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	// Start terminal in a goroutine
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer close(statusChan)
		defer s.logger.SetTerminalActive(false)
		defer running.Store(nil)

		err := s.terminal.Run(ctx, os.Stdin, os.Stdout)
		if err != nil {
			statusChan <- err
			return
		}
	}()

	statusChan <- "terminal started"

	return statusChan, nil
}

// Stop implements supervisor.Service interface
func (s *service) Stop(ctx context.Context) error {
	if running.CompareAndSwap(s, nil) {
		return errors.New("terminal is not running")
	}

	// Cancel the running context
	s.cancel()

	// Wait for terminal to finish with timeout
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
