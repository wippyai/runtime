package terminal

import (
	"context"
	"errors"
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	"io"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/tasks"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

/**
@todo: this is draft in progress
*/

type LuaTerminal struct {
	log      *zap.Logger
	tasker   *tasks.TaskRunner
	funcName string
	args     []lua.LValue
	state    atomic.Value // stores last captured state
	upstream <-chan any
}

type Options struct {
	FuncName string
	Args     []lua.LValue
}

func NewLuaTerminal(
	log *zap.Logger,
	runner *tasks.TaskRunner,
	opts Options,
	upstream <-chan any,
) *LuaTerminal {
	if log == nil {
		log = zap.NewNop()
	}

	return &LuaTerminal{
		log:      log,
		tasker:   runner,
		funcName: opts.FuncName,
		args:     opts.Args,
		upstream: upstream,
	}
}

func (t *LuaTerminal) Run(ctx context.Context, in io.Reader, out io.Writer) error {
	// start the tasker with the configured function and args
	resultChan, err := t.tasker.Start(ctx, t.funcName, t.args...)
	if err != nil {
		return fmt.Errorf("failed to start tasker: %w", err)
	}

	model := bubbleModel{
		tasker: t.tasker,
		logger: t.log,
		ctx:    ctx,
		out:    out,
	}

	p := tea.NewProgram(model, tea.WithInput(in), tea.WithOutput(out))

	// Handle context cancellation
	go func() {
		<-ctx.Done()
		p.Quit()
	}()

	go func() {
		for {
			select {
			case msgLua := <-t.upstream:
				// for lua tables only
				if _, ok := msgLua.(*lua.LTable); !ok {
					continue
				}

				msg, err := protocol.LuaToMsg(msgLua.(*lua.LTable))
				if err != nil {
					t.log.Error("failed to convert upstream message", zap.Error(err))
					continue
				}
				p.Send(msg)
			case <-ctx.Done():
				return
			}
		}
	}()

	result := make(chan any, 1)

	go func() {
		// Store final state from state channel as our state
		select {
		case state := <-resultChan:
			p.Quit()
			select {
			case result <- state:
			case <-ctx.Done():
			}
		case <-ctx.Done():
		}
	}()

	m, err := p.Run()
	if err != nil {
		return fmt.Errorf("bubbletea error: %w", err)
	}

	// stop the tasker and capture final state as state
	if err := t.tasker.Stop(ctx); err != nil {
		t.log.Error("failed to stop tasker", zap.Error(err))
	}

	select {
	case result := <-result:
		if state, ok := result.(lua.LValue); ok {
			t.state.Store(state)
		}

		if err, ok := result.(error); ok {
			var leak *engine.CoroutineLeak
			if errors.As(err, &leak) {
				t.log.Error("found coroutine leak, exiting", zap.Any("leak", leak))
				return supervisor.ErrExit
			}

			return fmt.Errorf("terminal app error: %w", err)
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	if m.(bubbleModel).quitting {
		return supervisor.ErrExit
	}

	return nil
}

func (t *LuaTerminal) Close(context.Context) error {
	return nil
}

func (t *LuaTerminal) Observe(context.Context, events.Bus) error {
	return nil
}

func (t *LuaTerminal) State() payload.Payload {
	// if state := t.state.Load(); state != nil {
	//	return state
	// }

	return nil
}

func (t *LuaTerminal) SetState(_ context.Context, state payload.Payload) error {
	// todo: get bus and dtt from ctx
	if state == nil {
		t.args = nil
		return nil
	}

	// luaArgs, ok := state.([]lua.LValue)
	// if !ok {
	//	return fmt.Errorf("invalid state type: expected []lua.LValue, got %T", state)
	// }
	//
	// t.args = luaArgs
	return nil
}
