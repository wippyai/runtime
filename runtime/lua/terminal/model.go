package terminal

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea"
	"io"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	transcode "github.com/ponyruntime/pony/pkg/payload/lua"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/tasks"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

/**
@todo: this is draft in progress
*/

type bubbleModel struct {
	tasker   *tasks.TaskRunner
	logger   *zap.Logger
	ctx      context.Context
	state    *lua.LState
	out      io.Writer
	quitting bool
}

func (m bubbleModel) Init() tea.Cmd {
	return tea.Batch(
		tea.EnterAltScreen,
	)
}

func (m bubbleModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
	default:
	}

	mappedMsg := m.mapMessage(msg)

	resultCh, err := m.tasker.Execute(m.ctx, "update", []lua.LValue{mappedMsg})
	if err != nil {
		m.logger.Error("failed to execute update task", zap.Error(err))
	} else {
		// we don't care about the result of the update task, we get messages from upstream
		go func() {
			var result engine.Result
			select {
			case result = <-resultCh:
			case <-m.ctx.Done():
				return
			}
			if result.Error != nil {
				m.logger.Error("update task failed", zap.Error(result.Error))
			}
		}()
	}

	return m, nil
}

func (m bubbleModel) mapMessage(msg tea.Msg) lua.LValue {
	// If it's already LValue, pass through
	if lv, ok := msg.(lua.LValue); ok {
		return lv
	}
	// Otherwise convert using btea
	return btea.ToLua(msg)
}

func (m bubbleModel) View() string {
	output := make(chan string, 1)

	// todo: make configurable
	timeout := time.After(1 * time.Second)

	go func() {
		resultCh, err := m.tasker.Execute(m.ctx, "view", []lua.LValue{
			transcode.GoToLua(map[string]any{"type": "view"}),
		})
		if err != nil {
			m.logger.Error("failed to execute view task", zap.Error(err))
			output <- "view task execution failed: " + err.Error()
			return
		}

		var result engine.Result
		select {
		case result = <-resultCh:
		case <-timeout:
			return
		case <-m.ctx.Done():
			return
		}

		if result.Error != nil {
			m.logger.Error("view task failed", zap.Error(result.Error))
			output <- "view task failed: " + result.Error.Error()
			return
		}

		if len(result.Result) > 0 {
			output <- result.Result[0].String()
		} else {
			output <- "no view data"
		}
	}()

	select {
	case view := <-output:
		return view
	case <-timeout:
		return "view task timed out"
	case <-m.ctx.Done():
		return "context done"
	}
}
