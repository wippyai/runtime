package btea

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	baseprocess "github.com/ponyruntime/pony/runtime/lua/component/process"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/service/terminal"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/api/topology"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/upstream"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

const (
	ChannelEvents = "@btea/events"

	// Timeout constants
	stopTimeout = 1000 * time.Millisecond
	taskTimeout = 5000 * time.Millisecond
	viewTimeout = 5000 * time.Millisecond

	maxViewRetries = 3

	// ExitKey is used to trigger process cancellation.
	ExitKey = "esc"
)

// App represents the main BubbleTea application that uses a State under the hood.
type App struct {
	// Process state
	state *baseprocess.State

	// BubbleTea specific fields
	program    *tea.Program
	terminal   *terminal.PipeContext
	upstream   chan payload.Payload
	numRetries int
	done       chan struct{}

	// Our own cancel mechanism
	appCtx    context.Context
	appCancel context.CancelFunc

	// Task runner - initialized after UoW is available
	taskRunner *TaskRunner

	// Ensure termination only happens once
	terminateOnce sync.Once
}

// NewApp creates a new BubbleTea application with the underlying process State.
func NewApp(log *zap.Logger, dtt payload.Transcoder, runner *engine.Runner, funcName string) (process.Process, error) {
	if dtt == nil {
		return nil, errors.New("transcoder is required")
	}

	state, err := baseprocess.NewState(log, runner, funcName)
	if err != nil {
		return nil, err
	}

	// Create app context separate from state context
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

// Update processes bubbletea messages. It listens for the exit key and sends updates to Lua.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == ExitKey {
			a.scheduleCancel()
			return a, nil
		}
	}

	// Forward update messages to Lua via task runner
	if a.taskRunner != nil {
		err := a.taskRunner.SendTask("update", protocol.MsgToLua(msg))
		if err != nil && !errors.Is(err, context.Canceled) {
			a.state.Log.Error("failed to send update message", zap.Error(err))
		}
	}

	return a, nil
}

// View retrieves the view from the Lua side.
func (a *App) View() string {
	if a.taskRunner == nil {
		return "initializing..."
	}

	response, err := a.taskRunner.ExecuteTask("view", lua.LTrue, viewTimeout)
	if err != nil || response == "" {
		a.numRetries++
		if a.numRetries < maxViewRetries {
			return fmt.Sprintf("view task failed (retrying %d/%d)", a.numRetries, maxViewRetries)
		}
		a.Terminate() // Use our terminate method to ensure proper cleanup
		return "view task failed (exiting)"
	}

	a.numRetries = 0
	return response
}

// Start initializes the app context, sets up terminal integration, and starts the process
func (a *App) Start(ctx context.Context, pid pubsub.PID, input payload.Payloads) error {
	// Get terminal context
	term := terminal.GetTerminalContext(ctx)
	if term == nil {
		return fmt.Errorf("terminal context not found")
	}
	a.terminal = term

	// Create bubbletea program
	a.program = tea.NewProgram(a, tea.WithInput(term.Stdin), tea.WithOutput(term.Stdout))

	// Enhance the context with upstream channel
	ctx = upstream.WithUpstreamChannel(ctx, a.upstream)

	// Initialize the process state
	if err := a.state.InitContext(ctx, pid); err != nil {
		return err
	}

	// Setup context watchers for cleanup
	a.setupContextWatchers()

	// Create a wrapping function to handle process start notification
	onStartFunc := func() {
		// Initialize task runner here when UoW is available
		a.taskRunner = NewTaskRunner(a)

		// Notify that the process has started
		if onStart := process.GetOnStart(a.state.Ctx); onStart != nil {
			onStart(pid, a)
		}

		// Run the bubbletea program concurrently
		go func() {
			if _, err := a.program.Run(); err != nil {
				a.state.Log.Debug("btea program error", zap.Error(err))
			}

			// When program exits, terminate the process
			a.Terminate()
		}()

		// Serve processing upstream messages
		go a.processUpstream()
	}

	// Serve the Lua function
	return a.state.Start(input, onStartFunc)
}

// setupContextWatchers sets up goroutines to watch various cancellation signals
func (a *App) setupContextWatchers() {
	// Watch app context
	go func() {
		select {
		case <-a.appCtx.Done():
		case <-a.state.Ctx.Done():
		case <-a.done:
		}

		a.state.Log.Debug("app context canceled, quitting program")
		a.Terminate()
		if a.program != nil {
			a.program.Quit()
		}
	}()
}

// scheduleCancel centralizes the cancellation routine
func (a *App) scheduleCancel() {
	go func() {
		err := a.Send(topology.Cancel(a.state.PID, a.state.PID, time.Now().Add(stopTimeout)))

		if err != nil {
			a.state.Log.Error("failed to send cancel event", zap.Error(err))
		}

		// Serve a timer to force termination if not already terminating
		select {
		case <-time.After(stopTimeout):
			a.state.Log.Debug("cancellation timeout reached, forcing termination")
			a.Terminate()
		case <-a.done:
			return
		case <-a.state.Ctx.Done():
			return
		case <-a.appCtx.Done():
			return
		}
	}()
}

// processUpstream listens for messages from the Lua runtime and forwards them to the UI
func (a *App) processUpstream() {
	for {
		select {
		case pp, ok := <-a.upstream:
			if !ok {
				return
			}
			value := pp.Data()
			msg, err := protocol.LuaToMsg(value.(lua.LValue))
			if msg == nil {
				msg = value
			}
			if err != nil {
				a.state.Log.Error("failed to convert upstream message", zap.Error(err))
				continue
			}
			a.program.Send(msg)
		case <-a.state.Ctx.Done():
			return
		case <-a.appCtx.Done():
			return
		case <-a.done:
			return
		}
	}
}

// Step advances the process state by one iteration
func (a *App) Step() error {
	select {
	case <-a.done:
		return supervisor.ErrExit
	case <-a.state.Ctx.Done():
		return a.state.Ctx.Err()
	case <-a.appCtx.Done():
		return context.Canceled
	default:
		for a.state.GetTaskCount() > 0 {
			if err := a.state.Step(false); err != nil {
				return err
			}
		}

		return a.state.Step(true)
	}
}

// Ready returns the number of tasks ready to be processed
func (a *App) Ready() int {
	return a.state.GetTaskCount()
}

// Send handles incoming messages to the process
func (a *App) Send(pkg *pubsub.Package) error {
	return a.state.SendPackage(pkg)
}

// Terminate forcefully stops the process
func (a *App) Terminate() {
	a.terminateOnce.Do(func() {
		a.state.Log.Debug("terminating btea app")

		// Try to shutdown the program gracefully
		if a.program != nil {
			a.program.Kill()
		}

		// Cancel app context to signal all our watchers
		a.appCancel()

		// Allow time for terminal to detach
		time.Sleep(stopTimeout)

		// Complete the state with exit error
		a.state.Complete(supervisor.ErrExit, nil)

		// Signal done to all our goroutines
		close(a.done)
		close(a.upstream)
	})
}

// publishTask sends a task to the unified events channel
func (a *App) publishTask(taskType string, luaValue lua.LValue, timeout time.Duration) string {
	// Check if task runner is available
	if a.taskRunner == nil {
		a.state.Log.Error("task runner not initialized", zap.String("task", taskType))
		return "task runner not initialized"
	}

	// Execute task using the task runner
	response, err := a.taskRunner.ExecuteTask(taskType, luaValue, timeout)
	if err != nil {
		// Return empty string for timeouts in fire-and-forget mode
		if errors.Is(err, ErrTimeout) && timeout <= 0 {
			return ""
		}

		// Log errors for debugging
		a.state.Log.Error("task failed", zap.String("task", taskType), zap.Error(err))

		// Return error message
		return err.Error()
	}

	return response
}
