package btea

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/api/service/terminal"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/api/topology"
	"github.com/ponyruntime/pony/internal/closer"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/subscribe"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	"github.com/ponyruntime/pony/runtime/lua/modules/tasks"
	"github.com/ponyruntime/pony/runtime/lua/modules/upstream"
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

// App represents the main application, integrating bubbletea with the underlying Lua runtime.
type App struct {
	// System fields
	log    *zap.Logger
	dtt    payload.Transcoder
	pubsub *subscribe.Layer

	// process and Lua state
	ctx         context.Context
	cancel      context.CancelFunc
	pid         pubsub.PID
	runner      *engine.Runner
	runnerState *lua.LState
	funcName    string

	// bubbletea integration
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
		log:      log,
		dtt:      dtt,
		pubsub:   subLayer,
		runner:   runner,
		funcName: funcName,
		upstream: make(chan payload.Payload, 100),
		done:     make(chan struct{}),
	}, nil
}

// Init is delegated to the bubbletea program.
func (p *App) Init() tea.Cmd {
	return nil
}

// scheduleCancel centralizes the cancellation routine.
func (p *App) scheduleCancel() {
	go func() {
		err := p.Send(topology.Cancel(p.pid, p.pid, time.Now().Add(stopTimeout)))

		if err != nil {
			p.log.Error("failed to send cancel event", zap.Error(err))
		}

		select {
		case <-time.After(stopTimeout):
			p.log.Debug("cancelling process after timeout")
			p.cancel()
		case <-p.done:
			return
		case <-p.ctx.Done():
			return
		}
	}()
}

// Update processes bubbletea messages. It listens for the exit key and sends updates to Lua.
func (p *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == ExitKey {
			err := p.Send(topology.Cancel(p.pid, p.pid, time.Now().Add(stopTimeout)))
			if err != nil {
				p.log.Error("failed to send cancel event", zap.Error(err))
			}
			p.scheduleCancel()
			return p, nil
		}
	}

	// Forward update messages to Lua via a task on the unified events channel.
	p.publishTask("update", protocol.MsgToLua(msg), 0)
	return p, nil
}

// View retrieves the view from the Lua side.
// If the view task fails, it returns an error string.
func (p *App) View() string {
	response := p.publishTask("view", lua.LTrue, viewTimeout)
	if response == "" {
		p.numRetries++
		if p.numRetries < maxViewRetries {
			return fmt.Sprintf("view task failed (retrying %d/%d)", p.numRetries, maxViewRetries)
		}
		p.cancel() // todo: think about it
		return "view task failed (exiting)"
	}
	p.numRetries = 0
	return response
}

// Start initializes the app context, sets up terminal integration, launches the bubbletea program,
// and starts the underlying Lua process.
func (p *App) Start(ctx context.Context, pid pubsub.PID, input payload.Payloads) error {
	p.ctx, p.cancel = context.WithCancel(ctx)
	p.pid = pid

	term := terminal.FromContext(ctx)
	if term == nil {
		return fmt.Errorf("terminal context not found")
	}
	p.terminal = term

	p.program = tea.NewProgram(p, tea.WithInput(term.Stdin), tea.WithOutput(term.Stdout))

	// sets up the process context
	ctx = process.WithContext(
		upstream.WithUpstreamChannel(ctx, p.upstream),
		&process.Context{
			PID:   pid,
			Start: time.Now(),
			Input: input,
		},
	)

	ctx, p.closer = closer.WithContext(p.runner.WithContext(ctx))

	args, err := p.toLuaPayloads(input)
	if err != nil {
		return fmt.Errorf("failed to convert payloads: %w", err)
	}

	// Start the Lua function.
	resultCh, err := p.runner.Start(ctx, p.funcName, args...)
	if err != nil {
		return fmt.Errorf("failed to start Lua function: %w", err)
	}

	// Run the bubbletea program concurrently.
	go func() {
		if _, err := p.program.Run(); err != nil {
			p.log.Error("btea program error", zap.Error(err))
		}
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
			if cErr := p.closer.Close(); cErr != nil {
				p.log.Error("failed to close resources", zap.Error(cErr))
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
			p.program.Quit()
			time.Sleep(stopTimeout)
		})
	}

	defer func() {
		if p.firstError != nil {
			completeProcess(p.firstError, nil)
		} else {
			completeProcess(supervisor.ErrExit, nil)
		}
		close(p.done)
		close(p.upstream)
		p.runner.Close()
		p.cancel()
	}()

	for {
		select {
		case result, ok := <-resultCh:
			if !ok {
				p.log.Error("runner error", zap.Error(p.firstError))
				completeProcess(p.firstError, nil)
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
func (p *App) Step() (bool, error) {
	select {
	case <-p.done:
		return false, nil
	case <-p.ctx.Done():
		return false, p.ctx.Err()
	default:
		err := p.runner.Continue(p.ctx, true)
		if p.firstError == nil && err != nil {
			p.firstError = err
		}
		return err != nil, err
	}
}

// Send transcodes and publishes messages to the Lua process.
func (p *App) Send(pkg *pubsub.Package) error {
	if pkg == nil {
		return errors.New("messages are nil")
	}
	select {
	case <-p.ctx.Done():
		return p.ctx.Err()
	case <-p.done:
		return errors.New("process stopped")
	default:
		for _, m := range pkg.Messages {
			luaValues, err := p.toLuaPayloads(m.Payloads)
			if err != nil {
				p.log.Error("failed to convert payloads", zap.Error(err))
				continue
			}
			if len(luaValues) > 0 {
				p.pubsub.Publish(m.Topic, luaValues...)
			}
		}
		return nil
	}
}

// publishTask sends a task to the unified events channel.
// If timeout is non-zero, it waits for a response; otherwise, it fires and forgets.
func (p *App) publishTask(taskType string, payload lua.LValue, timeout time.Duration) string {
	task, err := tasks.CreateTask(payload)
	if err != nil {
		p.log.Error("failed to create task", zap.String("task", taskType), zap.Error(err))
		if timeout > 0 {
			return "task creation failed"
		}
		return ""
	}

	wrappedTask := tasks.WrapTask(p.runnerState, task)
	msg := p.runnerState.NewTable()
	msg.RawSetString("type", lua.LString(taskType))
	msg.RawSetString("task", wrappedTask)

	p.pubsub.Publish(ChannelEvents, msg)

	if timeout > 0 {
		return p.waitResponse(task, timeout, taskType)
	}

	// Fire-and-forget: handle response in the background.
	go func() {
		select {
		case <-p.ctx.Done():
			return
		case rsp := <-task.Response:
			if err, ok := rsp.(error); ok {
				p.log.Error("task failed", zap.String("task", taskType), zap.Error(err))
			}
		case <-time.After(taskTimeout):
			p.log.Debug("task timeout", zap.String("task", taskType))
		}
	}()
	return ""
}

// waitResponse consolidates the select pattern for waiting for a task response.
func (p *App) waitResponse(task *tasks.Task, timeout time.Duration, taskType string) string {
	select {
	case rsp := <-task.Response:
		if result, ok := rsp.(lua.LValue); ok {
			return result.String()
		}
		p.log.Error("invalid response type", zap.String("task", taskType))
		return "invalid response type"
	case <-time.After(timeout):
		p.log.Debug("task timeout", zap.String("task", taskType))
		return ""
	case <-p.done:
		return "task cancelled"
	case <-p.ctx.Done():
		return "task cancelled"
	}
}

// toLuaPayloads converts a slice of payloads to Lua values.
func (p *App) toLuaPayloads(payloads payload.Payloads) ([]lua.LValue, error) {
	args := make([]lua.LValue, 0, len(payloads))
	for _, pp := range payloads {
		luaPayload, err := p.dtt.Transcode(pp, payload.Lua)
		if err != nil {
			return nil, fmt.Errorf("transcoding payload failed: %w", err)
		}
		if lv, ok := luaPayload.Data().(lua.LValue); ok {
			args = append(args, lv)
		}
	}
	return args, nil
}
