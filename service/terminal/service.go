package terminal

import (
	"context"
	"github.com/ponyruntime/pony/api/service/terminal"
	"log"
	"os"
	"sync"
)

// service wraps a Options to implement the supervisor.Service interface
type service struct {
	terminal terminal.Terminal
	options  terminal.Options

	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running bool
	mu      sync.Mutex
}

// newService creates a new service instance
func newService(terminal terminal.Terminal, options terminal.Options) *service {
	return &service{
		terminal: terminal,
		options:  options,
	}
}

// Start implements supervisor.Service interface
func (s *service) Start(ctx context.Context) (<-chan any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil, nil
	}

	// Create status channel and context with cancellation
	statusChan := make(chan any, 1)
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	// Start terminal in a goroutine
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer close(statusChan)

		//// Run terminal with stdin/stdout
		err := s.terminal.Run(ctx, os.Stdin, os.Stdout)
		if err != nil {
			statusChan <- err
			return
		}
	}()

	s.running = true
	statusChan <- "terminal started"

	return statusChan, nil
}

// Stop implements supervisor.Service interface
func (s *service) Stop(ctx context.Context) error {
	log.Printf("STOP TERMINAL APP")
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}

	// Cancel the running context
	s.cancel()
	s.mu.Unlock()

	// Wait for terminal to finish with timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
