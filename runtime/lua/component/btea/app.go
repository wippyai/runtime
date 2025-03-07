package btea

import (
	"context"
	"errors"
	"fmt"
	process2 "github.com/ponyruntime/pony/runtime/lua/component/process"
	"github.com/ponyruntime/pony/runtime/lua/engine/task"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/service/terminal"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/api/topology"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/subscribe"
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
	state *process2.State

	// BubbleTea specific fields
	program    *tea.Program
	terminal   *terminal.PipeContext
	upstream   chan payload.Payload
	numRetries int
	done       chan struct{}

	// Our own cancel mechanism
	appCtx    context.Context
	appCancel context.CancelFunc

	// Ensure termination only happens once
	terminateOnce sync.Once
}

// NewApp creates a new BubbleTea application with the underlying process State.
func NewApp(log *zap.Logger, dtt payload.Transcoder, runner *engine.Runner, funcName string) (process.Process, error) {
	if dtt == nil {
		return nil, errors.New("transcoder is required")
	}

	state, err := process2.NewState(log, runner, funcName)
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

	// Forward update messages to Lua via a task on the unified events channel.
	a.publishTask("update", protocol.MsgToLua(msg), 0)
	return a, nil
}

// View retrieves the view from the Lua side.
func (a *App) View() string {
	response := a.publishTask("view", lua.LTrue, viewTimeout)
	if response == "" {
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
		// Notify that the process has started
		if onStart := process.GetOnStart(a.state.Ctx); onStart != nil {
			onStart(pid, a)
		}

		// Run the bubbletea program concurrently
		go func() {
			if _, err := a.program.Run(); err != nil {
				a.state.Log.Error("btea program error", zap.Error(err))
			}
			// When program exits, terminate the process
			a.Terminate()
		}()

		// Start processing upstream messages
		go a.processUpstream()
	}

	// Start the Lua function
	return a.state.Start(input, onStartFunc)
}

// setupContextWatchers sets up goroutines to watch various cancellation signals
func (a *App) setupContextWatchers() {
	// Watch parent context (state.Ctx)
	go func() {
		<-a.state.Ctx.Done()
		a.state.Log.Debug("parent context cancelled, terminating app")
		a.Terminate()
	}()

	// Watch app context
	go func() {
		<-a.appCtx.Done()
		a.state.Log.Debug("app context cancelled, quitting program")
		// Quit the program if not already quitting
		if a.program != nil {
			a.program.Quit()
		}
	}()

	// Watch done channel
	go func() {
		<-a.done
		a.state.Log.Debug("done channel closed, ensuring termination")
		// Cleanup if anything is still running
		a.appCancel()
	}()
}

// scheduleCancel centralizes the cancellation routine
func (a *App) scheduleCancel() {
	go func() {
		err := a.Send(topology.Cancel(a.state.PID, a.state.PID, time.Now().Add(stopTimeout)))

		if err != nil {
			a.state.Log.Error("failed to send cancel event", zap.Error(err))
		}

		// Start a timer to force termination if not already terminating
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
		return nil
	case <-a.state.Ctx.Done():
		return a.state.Ctx.Err()
	case <-a.appCtx.Done():
		return context.Canceled
	default:
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

		// Cancel app context to signal all our watchers
		a.appCancel()

		// Try to shutdown the program gracefully
		if a.program != nil {
			a.program.Quit()
		}

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
func (a *App) publishTask(taskType string, payload lua.LValue, timeout time.Duration) string {
	// Check all possible cancellation signals
	select {
	case <-a.state.Ctx.Done():
		a.state.Log.Error("context error", zap.Error(a.state.Ctx.Err()))
		return "context error"
	case <-a.appCtx.Done():
		return "app context cancelled"
	case <-a.done:
		return "done"
	default:
		// Continue with task
	}

	t, err := task.CreateTask(payload)
	if err != nil {
		a.state.Log.Error("failed to create task", zap.String("task", taskType), zap.Error(err))
		if timeout > 0 {
			return "task creation failed"
		}
		return ""
	}

	wrappedTask := task.WrapTask(a.state.UoW.State(), t)
	msg := a.state.UoW.State().CreateTable(0, 2)
	msg.RawSetString("type", lua.LString(taskType))
	msg.RawSetString("task", wrappedTask)

	if pErr := subscribe.Publish(a.state.Ctx, ChannelEvents, msg); pErr != nil {
		a.state.Log.Error("failed to publish task", zap.String("task", taskType), zap.Error(err))
		return fmt.Errorf("failed to publish task: %w", pErr).Error()
	}

	if timeout > 0 {
		return a.waitResponse(t, timeout, taskType)
	}

	// Fire-and-forget: handle response in the background
	go func() {
		select {
		case <-a.state.Ctx.Done():
			return
		case <-a.appCtx.Done():
			return
		case <-a.done:
			return
		case rsp := <-t.Response:
			a.state.UoW.Tasks().WakeUp()
			if err, ok := rsp.(error); ok {
				a.state.Log.Error("task failed", zap.String("task", taskType), zap.Error(err))
			}
		case <-time.After(taskTimeout):
			a.state.Log.Debug("task timeout", zap.String("task", taskType))
		}
	}()

	return ""
}

// waitResponse waits for a task response with timeout
func (a *App) waitResponse(t *task.Task, timeout time.Duration, taskType string) string {
	select {
	case rsp := <-t.Response:
		a.state.UoW.Tasks().WakeUp()
		if result, ok := rsp.(lua.LValue); ok {
			return result.String()
		}
		a.state.Log.Error("invalid response type", zap.String("task", taskType))
		return "invalid response type"
	case <-time.After(timeout):
		a.state.Log.Debug("task timeout", zap.String("task", taskType))
		return ""
	case <-a.done:
		return "task cancelled"
	case <-a.state.Ctx.Done():
		return "task cancelled"
	case <-a.appCtx.Done():
		return "app cancelled"
	}
}
