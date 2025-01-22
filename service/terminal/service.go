package terminal

import (
	"context"
	"errors"
	"github.com/ponyruntime/pony/api/service/terminal"
	"github.com/ponyruntime/pony/service/terminal/logger"
	"log"
	"os"
	"sync"
	"sync/atomic"
)

// we only allow single terminal instance per node
var running atomic.Bool

// service wraps an Options to implement the supervisor.Service interface
type service struct {
	terminal terminal.Terminal
	options  terminal.Options
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// newService creates a new service instance
func newService(
	loggerCore *logger.Core,
	terminal terminal.Terminal,
	options terminal.Options,
) *service {
	return &service{
		terminal: terminal,
		options:  options,
	}
}

// Start implements supervisor.Service interface
func (s *service) Start(ctx context.Context) (<-chan any, error) {
	log.Printf("Starting terminal service %v", running.Load())
	if !running.CompareAndSwap(false, true) {
		return nil, errors.New("terminal is already running")
	}

	log.Printf("Starting terminal service %v", running.Load())

	// Create status channel and context with cancellation
	statusChan := make(chan any, 1)
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	// Start terminal in a goroutine
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer close(statusChan)

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
	if running.CompareAndSwap(true, false) {
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
