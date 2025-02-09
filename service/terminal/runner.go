package terminal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/service/terminal"
	"go.uber.org/zap"
)

// terminalRunner encapsulates terminal execution and lifecycle
type terminalRunner struct {
	terminal api.Terminal
	id       registry.Name
	bus      events.Bus
	log      *zap.Logger

	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	mu      sync.Mutex
	exitErr error
}

func newTerminalRunner(
	app api.Terminal,
	id registry.Name,
	bus events.Bus,
	log *zap.Logger,
) *terminalRunner {
	return &terminalRunner{
		terminal: app,
		id:       id,
		bus:      bus,
		log:      log,
		done:     make(chan struct{}),
	}
}

func (r *terminalRunner) start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.ctx != nil {
		return errors.New("terminal already running")
	}

	// Setup debug observation if supported
	if debugTerm, ok := r.terminal.(api.DebugTerminal); ok {
		if err := debugTerm.Observe(ctx, r.bus); err != nil {
			return fmt.Errorf("debug setup failed: %w", err)
		}
	}

	r.ctx, r.cancel = context.WithCancel(ctx)
	go r.run()

	return nil
}

func (r *terminalRunner) run() {
	defer close(r.done)

	err := r.terminal.Run(r.ctx, os.Stdin, os.Stdout)
	r.exitErr = err
	if err != nil {
		r.log.Error("terminal exited with error",
			zap.String("id", string(r.id)),
			zap.Error(err))
	} else {
		r.log.Info("terminal exited",
			zap.String("id", string(r.id)))
	}
}

func (r *terminalRunner) stop(ctx context.Context) error {
	r.mu.Lock()
	if r.ctx == nil {
		r.mu.Unlock()
		return nil
	}

	r.ctx = nil
	r.mu.Unlock()

	// coordinated shutdown
	done := make(chan struct{})
	go func() {
		r.cancel()
		<-r.done
		close(done)
	}()

	// wait for either shutdown completion or timeout
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

func (r *terminalRunner) wait() <-chan struct{} {
	return r.done
}

func (r *terminalRunner) transferState(next *terminalRunner) error {
	// Don't transfer state if it's the same app being updated
	if r.id != next.id {
		return nil
	}

	currentStateful, ok := r.terminal.(api.StatefulTerminal)
	if !ok {
		return nil
	}

	nextStateful, ok := next.terminal.(api.StatefulTerminal)
	if !ok {
		return nil
	}

	state := currentStateful.State()
	if state == nil {
		return nil
	}

	return nextStateful.SetState(state)
}
