package terminal

import (
	"context"
	"errors"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/service/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"io"
	"sync"
	"testing"
	"time"
)

// mockTerminal implements the basic Terminal interface
type mockTerminal struct {
	mu             sync.Mutex
	runCalled      bool
	closeCalled    bool
	shouldRunErr   error
	shouldCloseErr error
	runBlock       chan struct{} // Used to block Run() for testing
	started        chan struct{} // Used to signal that Run has started
}

func newMockTerminal() *mockTerminal {
	return &mockTerminal{
		runBlock: make(chan struct{}),
		started:  make(chan struct{}),
	}
}

func (m *mockTerminal) Run(ctx context.Context, in io.Reader, out io.Writer) error {
	if ctx == nil {
		return errors.New("nil context")
	}

	m.mu.Lock()
	m.runCalled = true
	m.mu.Unlock()

	// Signal that Run has started
	close(m.started)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-m.runBlock:
		return m.shouldRunErr
	}
}

func (m *mockTerminal) Close(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeCalled = true
	return m.shouldCloseErr
}

// mockDebugTerminal implements the DebugTerminal interface
type mockDebugTerminal struct {
	*mockTerminal
	observeCalled bool
	observeErr    error
}

func newMockDebugTerminal() *mockDebugTerminal {
	return &mockDebugTerminal{
		mockTerminal: newMockTerminal(),
	}
}

func (m *mockDebugTerminal) Observe(ctx context.Context, bus events.Bus) error {
	m.observeCalled = true
	return m.observeErr
}

// mockStatefulTerminal implements the StatefulTerminal interface
type mockStatefulTerminal struct {
	*mockTerminal
	state    payload.Payload
	stateErr error
}

func newMockStatefulTerminal() *mockStatefulTerminal {
	return &mockStatefulTerminal{
		mockTerminal: newMockTerminal(),
	}
}

func (m *mockStatefulTerminal) State() payload.Payload {
	return m.state
}

func (m *mockStatefulTerminal) SetState(p payload.Payload) error {
	m.state = p
	return m.stateErr
}

func TestTerminalRunner_Start(t *testing.T) {
	logger := zap.NewNop()
	bus := &mockEventBus{}
	id := registry.ID("test-terminal")

	tests := []struct {
		name      string
		terminal  terminal.Terminal
		wantErr   bool
		setupFunc func(*testing.T, terminal.Terminal)
		checkFunc func(*testing.T, terminal.Terminal)
	}{
		{
			name:     "basic terminal starts successfully",
			terminal: newMockTerminal(),
			wantErr:  false,
			checkFunc: func(t *testing.T, term terminal.Terminal) {
				mt := term.(*mockTerminal)
				<-mt.started // Wait for Run to actually start
				assert.True(t, mt.runCalled, "Run should be called")
			},
		},
		{
			name:     "debug terminal starts with observation",
			terminal: newMockDebugTerminal(),
			wantErr:  false,
			checkFunc: func(t *testing.T, term terminal.Terminal) {
				mt := term.(*mockDebugTerminal)
				<-mt.started // Wait for Run to actually start
				assert.True(t, mt.observeCalled, "Observe should be called")
				assert.True(t, mt.runCalled, "Run should be called")
			},
		},
		{
			name: "debug terminal fails observation",
			terminal: func() terminal.Terminal {
				mt := newMockDebugTerminal()
				mt.observeErr = errors.New("observe error")
				return mt
			}(),
			wantErr: true,
		},
		{
			name:     "cannot start already running terminal",
			terminal: newMockTerminal(),
			setupFunc: func(t *testing.T, term terminal.Terminal) {
				runner := newTerminalRunner(term, id, bus, logger)
				ctx := context.Background()
				err := runner.start(ctx)
				require.NoError(t, err)

				mt := term.(*mockTerminal)
				<-mt.started // Wait for Run to actually start

				// Try to start again
				err = runner.start(ctx)
				assert.Error(t, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := newTerminalRunner(tt.terminal, id, bus, logger)

			if tt.setupFunc != nil {
				tt.setupFunc(t, tt.terminal)
				return
			}

			err := runner.start(context.Background())
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tt.checkFunc != nil {
				tt.checkFunc(t, tt.terminal)
			}
		})
	}
}

func TestTerminalRunner_Stop(t *testing.T) {
	logger := zap.NewNop()
	bus := &mockEventBus{}
	id := registry.ID("test-terminal")

	tests := []struct {
		name        string
		terminal    terminal.Terminal
		wantErr     bool
		setup       func(*terminalRunner)
		makeContext func() (context.Context, context.CancelFunc) // Add context factory
	}{
		{
			name:     "stop not running terminal",
			terminal: newMockTerminal(),
			wantErr:  false,
			makeContext: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), time.Second)
			},
		},
		{
			name:     "stop running terminal",
			terminal: newMockTerminal(),
			setup: func(r *terminalRunner) {
				ctx := context.Background()
				err := r.start(ctx)
				require.NoError(t, err)

				mt := r.terminal.(*mockTerminal)
				<-mt.started // Wait for Run to actually start
				// Allow terminal to exit normally
				close(mt.runBlock)
			},
			makeContext: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), time.Second)
			},
			wantErr: false,
		},
		{
			name: "stop with context timeout",
			terminal: func() terminal.Terminal {
				mt := newMockTerminal()
				// Never close runBlock to make Run block indefinitely
				mt.runBlock = make(chan struct{})
				return mt
			}(),
			setup: func(r *terminalRunner) {
				ctx := context.Background()
				err := r.start(ctx)
				require.NoError(t, err)

				mt := r.terminal.(*mockTerminal)
				<-mt.started // Wait for Run to actually start
			},
			makeContext: func() (context.Context, context.CancelFunc) {
				// Use an already expired context for the timeout test
				return context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := newTerminalRunner(tt.terminal, id, bus, logger)

			if tt.setup != nil {
				tt.setup(runner)
			}

			// Create context based on test case
			ctx, cancel := tt.makeContext()
			defer cancel()

			err := runner.stop(ctx)
			if tt.wantErr {
				assert.Error(t, err)
				assert.ErrorIs(t, err, context.DeadlineExceeded)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestTerminalRunner_TransferState(t *testing.T) {
	logger := zap.NewNop()
	bus := &mockEventBus{}
	id := registry.ID("test-terminal")

	tests := []struct {
		name      string
		current   terminal.Terminal
		next      terminal.Terminal
		wantErr   bool
		checkFunc func(*testing.T, terminal.Terminal, terminal.Terminal)
	}{
		{
			name:    "transfer between non-stateful terminals",
			current: newMockTerminal(),
			next:    newMockTerminal(),
			wantErr: false,
		},
		{
			name: "transfer from stateful to non-stateful",
			current: func() terminal.Terminal {
				mt := newMockStatefulTerminal()
				mt.state = payload.New([]byte("test state"))
				return mt
			}(),
			next:    newMockTerminal(),
			wantErr: false,
		},
		{
			name: "successful state transfer",
			current: func() terminal.Terminal {
				mt := newMockStatefulTerminal()
				mt.state = payload.New([]byte("test state"))
				return mt
			}(),
			next:    newMockStatefulTerminal(),
			wantErr: false,
			checkFunc: func(t *testing.T, current, next terminal.Terminal) {
				currentSt := current.(*mockStatefulTerminal)
				nextSt := next.(*mockStatefulTerminal)
				assert.Equal(t, currentSt.state, nextSt.state)
			},
		},
		{
			name: "failed state transfer",
			current: func() terminal.Terminal {
				mt := newMockStatefulTerminal()
				mt.state = payload.New([]byte("test state"))
				return mt
			}(),
			next: func() terminal.Terminal {
				mt := newMockStatefulTerminal()
				mt.stateErr = errors.New("state transfer error")
				return mt
			}(),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			currentRunner := newTerminalRunner(tt.current, id, bus, logger)
			nextRunner := newTerminalRunner(tt.next, id, bus, logger)

			err := currentRunner.transferState(nextRunner)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tt.checkFunc != nil {
				tt.checkFunc(t, tt.current, tt.next)
			}
		})
	}
}

// mockEventBus implements a simple event bus for testing
type mockEventBus struct{}

func (m *mockEventBus) Subscribe(context.Context, events.System, chan<- events.Event) (events.SubscriberID, error) {
	return "", nil
}

func (m *mockEventBus) SubscribeP(context.Context, events.System, events.Kind, chan<- events.Event) (events.SubscriberID, error) {
	return "", nil
}

func (m *mockEventBus) Unsubscribe(context.Context, events.SubscriberID) {}

func (m *mockEventBus) Send(context.Context, events.Event) {}
