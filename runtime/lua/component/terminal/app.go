package terminal

import (
	"context"
	"errors"
	"fmt"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/supervisor"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/api/service/terminal"
	"github.com/ponyruntime/pony/internal/closer"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/subscribe"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	"github.com/ponyruntime/pony/runtime/lua/modules/tasks"
	"github.com/ponyruntime/pony/runtime/lua/modules/upstream"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// todo: we have memory leak in this package

const (
	// Channel identifiers for pubsub communication
	ChannelView   = "@btea/view"
	ChannelUpdate = "@btea/update"

	// Timeout constants
	stopTimeout = 500 * time.Millisecond
	taskTimeout = 300 * time.Millisecond
	viewTimeout = 200 * time.Millisecond

	maxViewRetries = 3

	// ExitKey is used to trigger process cancellation.
	ExitKey = "esc"
)

// NewApp creates and returns a new App instance.
// It validates that the transcoder and runner are provided,
// and finds the subscribe layer from the runner.
func NewApp(
	log *zap.Logger,
	dtt payload.Transcoder,
	runner *engine.Runner,
	funcName string,
) (process.Process, error) {

	if log == nil {
		log = zap.NewNop()
	}

	if dtt == nil {
		return nil, errors.New("transcoder is required")
	}

	if runner == nil {
		return nil, errors.New("runner is required")
	}

	var subLayer *subscribe.Layer
	for _, layer := range runner.GetLayers() {
		if sl, ok := layer.(*subscribe.Layer); ok {
			subLayer = sl
			break
		}
	}

	if subLayer == nil {
		return nil, errors.New("subscribe layer not found in runner")
	}

	return &App{
		// System fields
		log:    log,
		dtt:    dtt,
		pubsub: subLayer,

		// Runner and function information
		runner:   runner,
		funcName: funcName,

		// Channels for upstream messages and process cleanup
		upstream: make(chan payload.Payload, 100),
		done:     make(chan struct{}),
	}, nil
}

// App represents the main application, integrating bubbletea with the underlying Lua runtime.
type App struct {
	// System fields
	log    *zap.Logger
	dtt    payload.Transcoder
	pubsub *subscribe.Layer

	// Runner and Lua state
	ctx         context.Context
	cancel      context.CancelFunc
	pid         pubsub.PID
	runner      *engine.Runner
	runnerState *lua.LState
	funcName    string

	// Bubbletea integration
	program  *tea.Program
	terminal *terminal.PipeContext

	// Data from the underlying Lua application
	upstream chan payload.Payload

	// Cleanup and error handling
	done       chan struct{}
	firstError error
	closer     *closer.Cleanup

	numRetries int
}

// Init is delegated to the bubbletea program.
func (p *App) Init() tea.Cmd {
	return nil
}

// Update processes bubbletea messages. It listens for the exit key and sends updates to Lua.
func (p *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == ExitKey {
			// Send cancellation message and schedule context cancellation if needed.
			_ = p.Send(&pubsub.Batch{&pubsub.Message{Topic: process.TopicCancel}})
			go func() {
				select {
				case <-time.After(stopTimeout):
					p.cancel()
				case <-p.done:
					return
				case <-p.ctx.Done():
					return
				}
			}()
			return p, nil
		}
	}

	// Forward update messages to Lua.
	p.publishTask(ChannelUpdate, protocol.MsgToLua(msg)) // todo: deprecate?

	return p, nil
}

// View retrieves the view from the Lua side.
// If the view task fails, it returns an error string.
func (p *App) View() string {
	response := p.publishTaskWithResponse(ChannelView, lua.LTrue, viewTimeout)
	if response == "" {
		p.numRetries++
		if p.numRetries < maxViewRetries {
			return fmt.Sprintf("view task failed (retrying %d/%d)", p.numRetries, maxViewRetries)
		}

		p.cancel()
		return "view task failed (exiting)"
	}
	p.numRetries = 0

	return response
}

// publishTask sends a task to a specific channel without waiting for its response.
func (p *App) publishTask(channel string, value lua.LValue) {
	task, err := p.createTask(value)
	if err != nil {
		p.log.Error("failed to create task",
			zap.String("channel", channel),
			zap.Error(err))
		return
	}

	p.pubsub.Publish(channel, tasks.WrapTask(p.runnerState, task))

	// Handle the task response in the background.
	go func() {
		select {
		case <-p.ctx.Done():
			return
		case rsp := <-task.Response:
			if err, ok := rsp.Data().(error); ok {
				p.log.Error("task failed",
					zap.String("channel", channel),
					zap.Error(err))
			}
		case <-time.After(taskTimeout):
			p.log.Debug("task timeout", zap.String("channel", channel))
		}
	}()
}

// publishTaskWithResponse sends a task and waits for a response or timeout.
func (p *App) publishTaskWithResponse(channel string, value lua.LValue, timeout time.Duration) string {
	task, err := p.createTask(value)
	if err != nil {
		p.log.Error("failed to create task",
			zap.String("channel", channel),
			zap.Error(err))
		return "task creation failed"
	}

	p.pubsub.Publish(channel, tasks.WrapTask(p.runnerState, task))

	select {
	case rsp := <-task.Response:
		if result, ok := rsp.Data().(lua.LValue); ok {
			return result.String()
		}
		p.log.Error("invalid response type", zap.String("channel", channel))
		return "invalid response type"
	case <-time.After(timeout):
		p.log.Debug("task timeout", zap.String("channel", channel))
		return ""
	case <-p.done:
		return "task cancelled"
	case <-p.ctx.Done():
		return "task cancelled"
	}
}

// createTask wraps a value in a task payload.
func (p *App) createTask(value lua.LValue) (*tasks.Task, error) {
	return tasks.CreateTask(payload.NewPayload(value, payload.Lua)) // todo: track proper value?
}

// Start initializes the app context, sets up terminal integration, launches the bubbletea program,
// and starts the underlying Lua process.
func (p *App) Start(ctx context.Context, pid pubsub.PID, input payload.Payloads) error {
	// Create a cancellable context.
	p.ctx, p.cancel = context.WithCancel(ctx)
	p.pid = pid

	// Retrieve the terminal from context.
	term := terminal.FromContext(ctx)
	if term == nil {
		return fmt.Errorf("terminal context not found")
	}
	p.terminal = term

	// Initialize the bubbletea program.
	p.program = tea.NewProgram(p, tea.WithInput(term.Stdin), tea.WithOutput(term.Stdout))

	// Inject upstream channel into context and add cleanup handling.
	ctx = upstream.WithUpstreamChannel(ctx, p.upstream)
	ctx = p.runner.WithContext(ctx)
	ctx, p.closer = closer.WithContext(ctx)

	// Start the Lua function.
	resultCh, err := p.runner.Start(ctx, p.funcName, getLuaArgs(input)...)
	if err != nil {
		return err
	}

	// Run the bubbletea program concurrently.
	go func() {
		if _, err := p.program.Run(); err != nil {
			p.log.Error("btea program error", zap.Error(err))
		}

		// Set firstError if no error has been set yet.
		if p.firstError == nil {
			p.firstError = supervisor.ErrExit
		}

		p.cancel()
	}()

	p.runnerState = p.runner.GetCVM().State()
	if p.runnerState == nil {
		return errors.New("runner state is nil")
	}

	// Notify that the process has started.
	if onStart := process.GetOnStart(ctx); onStart != nil {
		onStart(pid, p)
	}

	// Start processing results and upstream messages.
	go p.processLoop(resultCh)

	return nil
}

// processLoop listens for Lua runner results and upstream messages,
// and handles process completion and cleanup.
func (p *App) processLoop(resultCh <-chan engine.Result) {
	var once sync.Once
	completeProcess := func(err error, result interface{}) {
		once.Do(func() {
			if cerr := p.closer.Close(); cerr != nil {
				p.log.Error("failed to close resources", zap.Error(cerr))
			}

			if onComplete := process.GetOnComplete(p.ctx); onComplete != nil {
				if err != nil {
					onComplete(p.pid, &runtime.Result{Error: err})
				} else {
					onComplete(p.pid, &runtime.Result{
						Payload: payload.NewPayload(result, payload.Lua),
					})
				}
			}

			_ = p.program.ReleaseTerminal()
			p.program.Quit()
		})
	}

	defer func() {
		completeProcess(supervisor.ErrExit, nil)
		close(p.done)
		close(p.upstream)
		p.runner.Close()
		p.cancel()
	}()

	for {
		select {
		case result, ok := <-resultCh:
			if !ok {
				return
			}
			if result.Error != nil {
				p.log.Error("runner error", zap.Error(result.Error))
				completeProcess(result.Error, nil)
				return
			}
			if len(result.Result) > 0 {
				p.log.Debug("runner completed", zap.Any("result", result.Result[0]))
				completeProcess(nil, result.Result[0])
				return
			}

		case pp, ok := <-p.upstream:
			if !ok {
				continue
			}
			value := pp.Data()
			msg, err := protocol.LuaToMsg(value.(lua.LValue))
			if msg == nil {
				msg = value
			}

			if err != nil {
				p.log.Error("failed to convert upstream message", zap.Error(err))
				continue
			}
			p.program.Send(msg)

		case <-p.ctx.Done():
			err := p.ctx.Err()
			if p.firstError != nil {
				err = p.firstError
			}
			completeProcess(err, nil)
			return
		}
	}
}

// Step continues the runner. If an error occurs, it quits the bubbletea program.
func (p *App) Step() error {
	select {
	case <-p.done:
		return nil
	case <-p.ctx.Done():
		return p.ctx.Err()
	default:
		err := p.runner.Continue(p.ctx)
		if p.firstError != nil && err != nil {
			p.firstError = err
		}
		return err
	}
}

// Send transcodes and publishes messages to the Lua process.
func (p *App) Send(msgs *pubsub.Batch) error {
	if msgs == nil {
		return errors.New("messages are nil")
	}

	select {
	case <-p.ctx.Done():
		return p.ctx.Err()
	case <-p.done:
		return errors.New("process stopped")
	default:
		for _, m := range *msgs {
			// For cancellation messages, release the channel.
			if m.Topic == process.TopicCancel {
				p.pubsub.Release(m.Topic) // todo: not working!
				continue
			}

			var luaValues []lua.LValue
			for _, pp := range m.Payloads {
				luaPayload, err := p.dtt.Transcode(pp, payload.Lua)
				if err != nil {
					p.log.Error("failed to transcode payload",
						zap.Error(err),
						zap.String("topic", m.Topic))
					continue
				}
				if lv, ok := luaPayload.Data().(lua.LValue); ok {
					luaValues = append(luaValues, lv)
				}
			}

			if len(luaValues) > 0 {
				p.pubsub.Publish(m.Topic, luaValues...)
			}
			p.log.Debug("sent message to process", zap.Any("msg", m))
		}
		return nil
	}
}

// getLuaArgs converts payloads to a slice of lua.LValue.
func getLuaArgs(payloads payload.Payloads) []lua.LValue {
	args := make([]lua.LValue, 0, len(payloads))
	for _, p := range payloads {
		if lv, ok := p.Data().(lua.LValue); ok {
			args = append(args, lv)
		}
	}
	return args
}
