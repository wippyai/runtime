package terminal

import (
	"context"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/tasks"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"io"
	"sync/atomic"
	"time"
)

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
	// Start the tasker with the configured function and args
	resultChan, err := t.tasker.Start(ctx, t.funcName, t.args...)
	if err != nil {
		return fmt.Errorf("failed to start tasker: %w", err)
	}

	model := bubbleModel{
		tasker: t.tasker,
		logger: t.log,
		ctx:    ctx,
		state:  t.tasker.State(),
		out:    out,
	}

	p := tea.NewProgram(
		model,
		tea.WithInput(in),
		tea.WithOutput(out),
		//	tea.WithAltScreen(),
	)

	// Handle context cancellation
	go func() {
		<-ctx.Done()
		p.Quit()
	}()

	go func() {
		for {
			select {
			case msg := <-t.upstream:
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
			time.Sleep(2 * time.Second)

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
			if leak, ok := err.(*engine.CoroutineLeak); ok {
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

func (t *LuaTerminal) Close(ctx context.Context) error {
	return nil
}

func (t *LuaTerminal) Observe(ctx context.Context, bus events.Bus) error {
	return nil
}

func (t *LuaTerminal) State() payload.Payload {
	if state := t.state.Load(); state != nil {
		//return state
	}

	return nil
}

// todo: ctx
func (t *LuaTerminal) SetState(ctx context.Context, state payload.Payload) error {
	// todO: get bus and dtt from ctx
	if state == nil {
		t.args = nil
		return nil
	}

	//luaArgs, ok := state.([]lua.LValue)
	//if !ok {
	//	return fmt.Errorf("invalid state type: expected []lua.LValue, got %T", state)
	//}
	//
	//t.args = luaArgs
	return nil
}
