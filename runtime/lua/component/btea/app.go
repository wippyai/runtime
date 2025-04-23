package btea

import (
	"context"
	"errors"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/service/terminal"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/api/topology"
	baseprocess "github.com/ponyruntime/pony/runtime/lua/component/process"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/upstream"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"sync"
	"sync/atomic"
	"time"
)

const (
	ChannelEvents = "@btea/events"
	// Timeouts
	stopTimeout    = 1000 * time.Millisecond
	taskTimeout    = 5000 * time.Millisecond
	viewTimeout    = 5000 * time.Millisecond
	maxViewRetries = 3
	// ExitKey triggers cancellation
	ExitKey = "esc"
	// View messages
	viewInitializing = "initializing..."
	viewUnavailable  = "State unavailable"
	viewFailed       = "view task failed (exiting)"
)

// App represents the main BubbleTea application.
type App struct {
	state         *baseprocess.State
	program       *tea.Program
	terminal      *terminal.PipeContext
	upstream      chan payload.Payload
	numRetries    int
	done          chan struct{}
	appCtx        context.Context
	appCancel     context.CancelFunc
	taskRunner    atomic.Pointer[TaskRunner]
	stateMu       sync.RWMutex // Protects state access vs invalidation
	terminateOnce sync.Once
}

// NewApp creates a new BubbleTea application.
func NewApp(
	log *zap.Logger,
	dtt payload.Transcoder,
	runner *engine.Runner,
	funcName string,
) (process.Process, error) {
	if dtt == nil {
		return nil, errors.New("transcoder is required")
	}
	state, err := baseprocess.NewState(log, runner, funcName)
	if err != nil {
		return nil, err
	}
	appCtx, appCancel := context.WithCancel(context.Background())
	return &App{
		state:     state,
		upstream:  make(chan payload.Payload, 100),
		done:      make(chan struct{}),
		appCtx:    appCtx,
		appCancel: appCancel,
	}, nil
}

// Init is delegated to the bubbletea program.
func (a *App) Init() tea.Cmd {
	return nil
}

// Update processes bubbletea messages.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == ExitKey {
			a.scheduleCancel()
			return a, nil
		}
	}

	tr := a.taskRunner.Load()
	if tr == nil {
		return a, nil // Not ready or already terminated
	}

	err := tr.SendTask("update", protocol.MsgToLua(msg))

	if errors.Is(err, process.ErrNoProcess) {
		// State became invalid concurrently. Logged in SendTask.
	} else if err != nil && !errors.Is(err, context.Canceled) {
		a.state.Log.Error("failed to send update message", zap.Error(err))
	}

	return a, nil
}

// View retrieves the view from the Lua side.
func (a *App) View() string {
	tr := a.taskRunner.Load()
	if tr == nil {
		select {
		case <-a.done:
			return viewUnavailable
		default:
			return viewInitializing
		}
	}

	response, err := tr.ExecuteTask("view", lua.LTrue, viewTimeout)

	if errors.Is(err, process.ErrNoProcess) {
		a.state.Log.Warn("Lua state unavailable during view execution")
		return viewUnavailable
	}

	if err != nil {
		if errors.Is(err, ErrTimeout) || errors.Is(err, context.Canceled) || errors.Is(err, a.state.Ctx.Err()) {
			a.state.Log.Warn("view task failed, timed out, or canceled", zap.Error(err))
			return fmt.Sprintf("%s (error: %v)", viewUnavailable, err)
		}

		a.numRetries++
		if a.numRetries < maxViewRetries {
			a.state.Log.Warn("view task failed, retrying", zap.Int("retry", a.numRetries), zap.Error(err))
			return fmt.Sprintf("view task failed (retrying %d/%d)", a.numRetries, maxViewRetries)
		}
		a.state.Log.Error("view task failed after multiple retries, terminating", zap.Error(err))
		a.Terminate()
		return viewFailed
	}

	a.numRetries = 0
	return response
}

// Start initializes the app context, sets up terminal integration, and starts the process.
func (a *App) Start(ctx context.Context, pid pubsub.PID, input payload.Payloads) error {
	term := terminal.GetTerminalContext(ctx)
	if term == nil {
		return fmt.Errorf("terminal context not found")
	}
	a.terminal = term
	a.program = tea.NewProgram(a, tea.WithInput(term.Stdin), tea.WithOutput(term.Stdout))
	ctx = upstream.WithUpstreamChannel(ctx, a.upstream)

	if err := a.state.InitContext(ctx, pid); err != nil {
		return err
	}
	a.setupContextWatchers()

	onStartFunc := func() {
		// Create and store the task runner after UoW is available.
		tr := NewTaskRunner(a)
		a.taskRunner.Store(tr)

		if onStart := process.GetOnStart(a.state.Ctx); onStart != nil {
			onStart(pid, a)
		}

		go func() {
			// BubbleTea program loop
			if _, err := a.program.Run(); err != nil && err != tea.ErrProgramKilled {
				a.state.Log.Debug("btea program error", zap.Error(err))
			} else {
				a.state.Log.Debug("btea program exited")
			}
			a.Terminate() // Ensure termination if program exits
		}()

		go a.processUpstream()
	}

	return a.state.Start(input, onStartFunc)
}

// setupContextWatchers sets up goroutines to watch various cancellation signals.
func (a *App) setupContextWatchers() {
	// Watch app context (internal cancellation)
	go func() {
		select {
		case <-a.appCtx.Done():
			a.state.Log.Debug("app context canceled, terminating")
			a.Terminate()
		case <-a.done: // Already terminated
		}
	}()
	// Watch state context (supervisor cancellation)
	go func() {
		select {
		case <-a.state.Ctx.Done():
			a.state.Log.Debug("state context canceled, terminating")
			a.Terminate()
		case <-a.done:
		case <-a.appCtx.Done(): // Handled by other watcher
		}
	}()
}

// scheduleCancel requests graceful cancellation via topology event.
func (a *App) scheduleCancel() {
	go func() {
		a.state.Log.Debug("scheduling process cancellation via topology event")
		err := a.Send(topology.Cancel(a.state.PID, a.state.PID, time.Now().Add(stopTimeout)))
		if err != nil {
			a.state.Log.Error("failed to send self-cancel event, forcing terminate after timeout", zap.Error(err))
			time.Sleep(stopTimeout)
			a.Terminate()
		}
	}()
}

// processUpstream listens for messages from Lua and forwards them to BubbleTea.
func (a *App) processUpstream() {
	defer a.state.Log.Debug("processUpstream goroutine finished")
	for {
		select {
		case pp, ok := <-a.upstream:
			if !ok {
				return
			} // Channel closed

			lval, ok := pp.Data().(lua.LValue)
			if !ok {
				a.state.Log.Error("received non-LValue from upstream", zap.Any("value", pp.Data()))
				if msg, ok := pp.Data().(tea.Msg); ok {
					a.program.Send(msg)
				}
				continue
			}
			msg, err := protocol.LuaToMsg(lval)
			if err != nil {
				a.state.Log.Error("failed to convert upstream Lua message", zap.Error(err))
				continue
			}
			if msg != nil {
				a.program.Send(msg)
			}
		// Exit conditions
		case <-a.state.Ctx.Done():
			return
		case <-a.appCtx.Done():
			return
		case <-a.done:
			return
		}
	}
}

// Step advances the process state by one iteration.
func (a *App) Step() error {
	select {
	case <-a.done:
		return supervisor.ErrExit
	case <-a.state.Ctx.Done():
		a.Terminate()
		return a.state.Ctx.Err()
	case <-a.appCtx.Done():
		a.Terminate()
		return context.Canceled
	default: // Proceed
	}

	handleStepError := func(err error, stepType string) error {
		a.stateMu.Lock() // Acquire write lock
		defer a.stateMu.Unlock()

		if !errors.Is(err, supervisor.ErrExit) {
			a.state.Log.Error(fmt.Sprintf("error during %s step", stepType), zap.Error(err))
		}

		// Invalidate runner state under write lock
		tr := a.taskRunner.Load()
		if tr != nil {
			tr.state.Store(nil)
			a.taskRunner.Store(nil)
			a.state.Log.Debug("Invalidated task runner state under write lock")
		}
		// Trigger termination asynchronously to avoid holding lock during Terminate's potentially blocking parts
		go a.Terminate()
		return err
	}

	// Process non-blocking tasks
	for a.state.GetTaskCount() > 0 {
		select { // Re-check cancellation
		case <-a.done:
			return supervisor.ErrExit
		case <-a.state.Ctx.Done():
			a.Terminate()
			return a.state.Ctx.Err()
		case <-a.appCtx.Done():
			a.Terminate()
			return context.Canceled
		default: // continue
		}
		if err := a.state.Step(false); err != nil {
			return handleStepError(err, "non-blocking")
		}
	}

	// Process blocking step
	err := a.state.Step(true)
	if err != nil {
		return handleStepError(err, "blocking")
	}

	return nil // Success
}

// Ready returns the number of tasks ready to be processed.
func (a *App) Ready() int {
	select {
	case <-a.done:
		return 0
	default:
		return a.state.GetTaskCount()
	}
}

// Send handles incoming messages to the process.
func (a *App) Send(pkg *pubsub.Package) error {
	select {
	case <-a.done:
		return fmt.Errorf("process terminated: %w", process.ErrNoProcess)
	default:
		return a.state.SendPackage(pkg)
	}
}

// Terminate stops the process and cleans up resources.
func (a *App) Terminate() {
	a.terminateOnce.Do(func() {
		a.state.Log.Debug("Terminate sequence started")

		a.appCancel() // Signal internal cancellation
		a.state.Log.Debug("App context canceled")

		a.state.Log.Debug("Acquiring state write lock for termination")
		a.stateMu.Lock() // ---- WRITE LOCK ACQUIRED ----
		a.state.Log.Debug("State write lock acquired")

		// Invalidate runner under lock
		runnerToClose := a.taskRunner.Load()
		if runnerToClose != nil {
			runnerToClose.state.Store(nil)
			a.taskRunner.Store(nil)
			a.state.Log.Debug("Task runner invalidated under lock")
		} else {
			a.state.Log.Debug("Task runner was already nil")
		}

		a.stateMu.Unlock() // ---- WRITE LOCK RELEASED ----
		a.state.Log.Debug("State write lock released")

		// Quit BubbleTea program (non-blocking)
		if a.program != nil {
			a.state.Log.Debug("Sending Quit to bubbletea program")
			a.program.Quit()
		}

		// Close the runner instance (harmless if already nilled)
		if runnerToClose != nil {
			runnerToClose.Close()
		}

		// Close upstream channel (non-blocking pattern)
		select {
		case <-a.upstream: // Already closed
		default:
			close(a.upstream)
		}
		a.state.Log.Debug("Upstream channel closed")

		// Signal internal goroutines via 'done' channel
		close(a.done)
		a.state.Log.Debug("Done channel closed")

		// Complete the underlying process state last
		finalErr := supervisor.ErrExit
		if stateErr := a.state.Ctx.Err(); stateErr != nil {
			finalErr = stateErr
		} else if appErr := a.appCtx.Err(); appErr != nil && !errors.Is(appErr, context.Canceled) {
			finalErr = appErr
		}
		a.state.Complete(finalErr, nil)
		a.state.Log.Info("btea app terminated", zap.Error(finalErr))
	})
}
