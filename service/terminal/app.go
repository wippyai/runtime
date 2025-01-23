package terminal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/service/terminal"
)

type appState uint32

const (
	appStateNone appState = iota
	appStateInitializing
	appStateRunning
	appStateTerminating
	appStateDead // Final state, no more operations possible
)

var (
	ErrTerminating = errors.New("app is terminating")
	ErrDead        = errors.New("app lifecycle is dead")
)

type appInstance struct {
	terminal api.Terminal
	options  api.Options
	id       registry.ID
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
}

type appLifecycle struct {
	state atomic.Uint32

	current *appInstance
	next    *appInstance

	bus      events.Bus
	timeouts api.TimeoutConfig
	status   chan any  // Changed to bidirectional channel
	statusMu sync.Once // For closing status channel exactly once
	wg       sync.WaitGroup
	mu       sync.RWMutex
}

func newAppLifecycle(bus events.Bus, timeouts api.TimeoutConfig) *appLifecycle {
	l := &appLifecycle{
		bus:      bus,
		timeouts: timeouts,
		status:   make(chan any, 1),
	}
	l.state.Store(uint32(appStateNone))
	return l
}

func (a *appLifecycle) getState() appState {
	return appState(a.state.Load())
}

func (a *appLifecycle) setState(state appState) {
	a.state.Store(uint32(state))
}

func (a *appLifecycle) cleanup() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.current != nil {
		a.current.cancel()
		a.current = nil
	}
	if a.next != nil {
		a.next.cancel()
		a.next = nil
	}

	// Close status channel exactly once
	a.statusMu.Do(func() {
		if a.status != nil {
			close(a.status)
		}
	})

	a.setState(appStateDead)
}

func (a *appLifecycle) runTerminal(instance *appInstance) {
	defer a.wg.Done()
	defer close(instance.done)

	if err := instance.terminal.Run(instance.ctx, os.Stdin, os.Stdout); err != nil && a.status != nil {
		select {
		case a.status <- err:
		default:
		}
	}
}

func (a *appLifecycle) Start(ctx context.Context, term api.Terminal, opts api.Options, id registry.ID) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	switch a.getState() {
	case appStateTerminating:
		return ErrTerminating
	case appStateDead:
		return ErrDead
	case appStateRunning, appStateInitializing:
		return errors.New("app already started")
	case appStateNone:
		// all clear
	}

	appCtx, cancel := context.WithCancel(ctx)

	select {
	case a.status <- "starting terminal":
	default:
	}

	a.setState(appStateInitializing)
	a.current = &appInstance{
		terminal: term,
		options:  opts,
		id:       id,
		ctx:      appCtx,
		cancel:   cancel,
		done:     make(chan struct{}, 1),
	}

	if debugTerm, ok := term.(api.DebugTerminal); ok {
		if err := debugTerm.Observe(appCtx, a.bus); err != nil {
			cancel()
			a.current = nil
			a.setState(appStateNone)
			return fmt.Errorf("failed to initialize debug terminal: %w", err)
		}
	}

	a.wg.Add(1)
	go a.runTerminal(a.current)

	a.setState(appStateRunning)
	select {
	case a.status <- "terminal started":
	default:
	}

	return nil
}

func (a *appLifecycle) Update(ctx context.Context, term api.Terminal, opts api.Options, id registry.ID) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	switch a.getState() {
	case appStateTerminating:
		return ErrTerminating
	case appStateDead:
		return ErrDead
	case appStateInitializing:
		return errors.New("app not running")
	case appStateNone, appStateRunning:
		// all clear
	}

	appCtx, cancel := context.WithCancel(ctx)
	next := &appInstance{
		terminal: term,
		options:  opts,
		id:       id,
		ctx:      appCtx,
		cancel:   cancel,
		done:     make(chan struct{}, 1),
	}

	if debugTerm, ok := term.(api.DebugTerminal); ok {
		if err := debugTerm.Observe(appCtx, a.bus); err != nil {
			cancel()
			return fmt.Errorf("failed to initialize next terminal: %w", err)
		}
	}

	if a.current != nil {
		a.current.cancel()
		closeCtx, closeCancel := context.WithTimeout(ctx, a.timeouts.CloseTimeout)
		defer closeCancel()

		select {
		case <-a.current.done:
		case <-closeCtx.Done():
			select {
			case a.status <- "terminal close timeout":
			default:
			}
		}

		if err := a.current.terminal.Close(closeCtx); err != nil {
			select {
			case a.status <- fmt.Errorf("failed to close terminal: %w", err):
			default:
			}
			return err
		}

		if a.current.id == id {
			currentStateful, currentOk := a.current.terminal.(api.StatefulTerminal)
			nextStateful, nextOk := term.(api.StatefulTerminal)

			if currentOk && nextOk {
				state := currentStateful.State()
				if state != nil {
					select {
					case a.status <- "transferring terminal state":
					default:
					}

					if err := nextStateful.SetState(state); err != nil {
						cancel()
						return fmt.Errorf("failed to transfer state: %w", err)
					}

					select {
					case a.status <- "terminal state transferred":
					default:
					}
				}
			}
		}
	}

	a.wg.Add(1)
	go a.runTerminal(next)

	a.current = next

	select {
	case a.status <- "terminal updated":
	default:
	}

	return nil
}

func (a *appLifecycle) Release(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()

	prevState := a.getState()
	if prevState == appStateDead {
		return
	}

	a.setState(appStateTerminating)

	if a.current != nil {
		a.current.cancel()

		closeCtx, cancel := context.WithTimeout(ctx, a.timeouts.CloseTimeout)
		defer cancel()

		select {
		case <-a.current.done:
		case <-closeCtx.Done():
			select {
			case a.status <- "terminal close timeout":
			default:
			}
		}

		if err := a.current.terminal.Close(closeCtx); err != nil {
			select {
			case a.status <- fmt.Errorf("failed to close terminal: %w", err):
			default:
			}
		}
	}

	if a.next != nil {
		a.next.cancel()
	}

	a.wg.Wait()

	select {
	case a.status <- "terminal released":
	default:
	}

	a.cleanup()
}

func (a *appLifecycle) Current() (*appInstance, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.current, a.current != nil
}

func (a *appLifecycle) Status() <-chan any {
	return a.status
}
