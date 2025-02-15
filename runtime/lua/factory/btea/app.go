package btea

import (
	"context"
	"errors"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/api/service/terminal"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/internal/closer"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/subscribe"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	"github.com/ponyruntime/pony/runtime/lua/modules/tasks"
	"github.com/ponyruntime/pony/runtime/lua/modules/upstream"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"sync"
	"time"
)

const (
	ChannelView   = "@btea/view"
	ChannelUpdate = "@btea/update"

	// Timeout constants
	taskTimeout = 3000 * time.Millisecond
	viewTimeout = 12000 * time.Millisecond
	stopTimeout = 5000 * time.Millisecond

	ExitKey = "esc"
)

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
		runner:   runner,
		funcName: funcName,
		pubsub:   subLayer,
		upstream: make(chan payload.Payload, 100),
		done:     make(chan struct{}),
	}, nil
}

type App struct {
	// system layer
	log    *zap.Logger
	dtt    payload.Transcoder
	pubsub *subscribe.Layer

	// what we run
	ctx         context.Context
	cancel      context.CancelFunc
	pid         process.PID
	runner      *engine.Runner
	runnerState *lua.LState
	funcName    string

	// bubbletea integration
	program  *tea.Program
	terminal *terminal.PipeContext

	// data from underlying lua application
	upstream chan payload.Payload

	// cleanup
	done       chan struct{}
	firstError error
	closer     *closer.Cleanup
}

func (p *App) Init() tea.Cmd {
	return nil // delegated
}

func (p *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == ExitKey {
			_ = p.Send(&process.Message{Topic: process.TopicCancel})
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

	p.publishTask(ChannelUpdate, protocol.MsgToLua(msg))
	return p, nil
}

func (p *App) View() string {
	response := p.publishTaskWithResponse(ChannelView, lua.LTrue, viewTimeout)
	if response == nil {
		return "view task failed"
	}
	return response.String()
}

// publishTask sends a task to a channel without waiting for a response.
func (p *App) publishTask(channel string, value lua.LValue) {
	task, err := p.createTask(value)
	if err != nil {
		p.log.Error("failed to create task",
			zap.String("channel", channel),
			zap.Error(err))
		return
	}

	p.pubsub.Publish(channel, tasks.WrapTask(p.runnerState, task))

	// Handle response in background.
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

// publishTaskWithResponse sends a task and waits for its response.
func (p *App) publishTaskWithResponse(channel string, value lua.LValue, timeout time.Duration) lua.LValue {
	task, err := p.createTask(value)
	if err != nil {
		p.log.Error("failed to create task",
			zap.String("channel", channel),
			zap.Error(err))
		return nil
	}

	p.pubsub.Publish(channel, tasks.WrapTask(p.runnerState, task))

	select {
	case rsp := <-task.Response:
		if str, ok := rsp.Data().(lua.LValue); ok {
			return str
		}
		p.log.Error("invalid response type", zap.String("channel", channel))
		return nil
	case <-time.After(timeout):
		p.log.Debug("task timeout", zap.String("channel", channel))
		return nil
	case <-p.ctx.Done():
		return nil
	}
}

func (p *App) createTask(value lua.LValue) (*tasks.Task, error) {
	return tasks.CreateTask(payload.NewPayload(value, payload.Lua))
}

func (p *App) Start(ctx context.Context, pid process.PID, input payload.Payloads) error {
	p.ctx, p.cancel = context.WithCancel(ctx)
	p.pid = pid

	// Get terminal from context.
	term := terminal.FromContext(ctx)
	if term == nil {
		return fmt.Errorf("terminal context not found")
	}
	p.terminal = term

	p.program = tea.NewProgram(p, tea.WithInput(term.Stdin), tea.WithOutput(term.Stdout))

	ctx = upstream.WithUpstreamChannel(ctx, p.upstream)
	ctx = p.runner.WithContext(ctx)
	ctx, p.closer = closer.WithContext(ctx)

	go func() {
		if _, err := p.program.Run(); err != nil {
			p.log.Error("btea program error", zap.Error(err))
		}

		// Removed print statements for program exit.
		if p.firstError == nil {
			p.firstError = supervisor.ErrExit
		}

		p.cancel()
	}()

	resultCh, err := p.runner.Start(ctx, p.funcName, getLuaArgs(input)...)
	if err != nil {
		return err
	}

	p.runnerState = p.runner.GetCVM().State()
	if p.runnerState == nil {
		return errors.New("runner state is nil")
	}

	if onStart := process.GetOnStart(ctx); onStart != nil {
		onStart(pid, p)
	}

	go p.processLoop(resultCh)

	return nil
}

func (p *App) processLoop(resultCh <-chan engine.Result) {
	defer func() {
		close(p.done)
		close(p.upstream)
		p.runner.Close()
		p.cancel()
	}()

	var once sync.Once

	handleCompletion := func(err error, payloadResult interface{}) {
		once.Do(func() {
			if cErr := p.closer.Close(); cErr != nil {
				p.log.Error("failed to close resources", zap.Error(cErr))
			}
			if onComplete := process.GetOnComplete(p.ctx); onComplete != nil {
				if err != nil {
					onComplete(p.pid, &runtime.Result{Error: err})
				} else {
					onComplete(p.pid, &runtime.Result{
						Payload: payload.NewPayload(payloadResult, payload.Lua),
					})
				}
			}
		})
	}

	for {
		select {
		case result, ok := <-resultCh:
			if !ok {
				return
			}
			if result.Error != nil {
				p.log.Error("runner error", zap.Error(result.Error))
				handleCompletion(result.Error, nil)
				return
			}
			if len(result.Result) > 0 {
				p.log.Debug("runner completed", zap.Any("result", result.Result[0]))
				handleCompletion(nil, result.Result[0])
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
			handleCompletion(err, nil)
			return
		}
	}
}

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
		if err != nil {
			p.program.Quit()
		}
		return err
	}
}

func (p *App) Send(msg ...*process.Message) error {
	select {
	case <-p.ctx.Done():
		return p.ctx.Err()
	case <-p.done:
		return errors.New("process stopped")
	default:
		for _, m := range msg {
			if m.Topic == process.TopicCancel {
				p.pubsub.Release(m.Topic)
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
				if luaValue, ok := luaPayload.Data().(lua.LValue); ok {
					luaValues = append(luaValues, luaValue)
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

func getLuaArgs(payloads payload.Payloads) []lua.LValue {
	args := make([]lua.LValue, 0, len(payloads))
	for _, p := range payloads {
		if lv, ok := p.Data().(lua.LValue); ok {
			args = append(args, lv)
		}
	}
	return args
}
