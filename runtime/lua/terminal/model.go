package terminal

import (
	"context"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	transcode "github.com/ponyruntime/pony/pkg/payload/lua"
	"github.com/ponyruntime/pony/runtime/lua/tasks"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"io"
	"time"
)

type tickMsg struct{}

func tick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {

		return tickMsg{}
	})
}

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
		tick(),
	)
}

func (m bubbleModel) mapMessage(msg tea.Msg) lua.LValue {
	switch msg := msg.(type) {
	case tickMsg:
		return transcode.GoToLua(m.state, map[string]any{
			"type": "update",
			"tick": true,
		})
	case tea.KeyMsg:
		return transcode.GoToLua(m.state, map[string]any{
			"type": "update",
			"key": map[string]any{
				"Type":   msg.Type.String(),
				"String": msg.String(),
				"Alt":    msg.Alt,
				"Runes":  string(msg.Runes),
			},
		})
	default:
		return transcode.GoToLua(m.state, map[string]any{
			"type": "update",
			"msg":  fmt.Sprintf("%v", msg),
		})
	}
}

func (m bubbleModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
	}

	mappedMsg := m.mapMessage(msg)
	resultCh, err := m.tasker.Execute(m.ctx, "update", []lua.LValue{mappedMsg})
	if err != nil {
		m.logger.Error("failed to execute update task", zap.Error(err))
		return m, nil
	}

	result := <-resultCh
	if result.Error != nil {
		m.logger.Error("update task failed", zap.Error(result.Error))
	}

	if _, ok := msg.(tickMsg); ok {
		return m, tick()
	}

	return m, nil
}

func (m bubbleModel) View() string {
	resultCh, err := m.tasker.Execute(m.ctx, "view", []lua.LValue{
		transcode.GoToLua(m.state, map[string]any{"type": "view"}),
	})
	if err != nil {
		return "Error: failed to execute view task"
	}

	result := <-resultCh
	if result.Error != nil {
		return "Error: view task failed"
	}

	if len(result.Result) > 0 {
		return result.Result[0].String()
	}

	return "No view data"
}
