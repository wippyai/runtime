package main

import (
	"context"
	"fmt"
	"github.com/charmbracelet/bubbletea"
	transcode "github.com/ponyruntime/pony/pkg/payload/lua"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/tasks"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

type model struct {
	tasker *tasks.TaskRunner
	logger *zap.Logger
	ctx    context.Context
	state  *lua.LState
}

const luaScript = `
function App()
    local inbox = tasks.channel()
    local text = ""
    
    while true do
        local task, ok = inbox:receive()
        if not ok then
            break
        end
        
        local msg = task:input()
        if msg.type == "update" then
            if msg.key then
                if msg.key.Type == " " or msg.key.String == " " then
                    -- Handle space
                    text = text .. " "
                elseif msg.key.Type == "runes" then
                    text = text .. msg.key.String
                elseif msg.key.String == "backspace" then
                    if #text > 0 then
                        repeat
                            text = text:sub(1, -2)
                        until #text == 0 or text:match("[\128-\191]$") == nil
                    end
                end
            end
            task:send(true)
            task:complete(nil)
        elseif msg.type == "view" then
            task:complete("Text: " .. text .. "▌")
        end
    end
end

return App
`

func (m model) mapMessage(msg tea.Msg) lua.LValue {
	switch msg := msg.(type) {
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

func initialModel() model {
	logger := zap.NewExample()
	ctx := context.Background()

	vm, err := engine.NewCVM(logger,
		engine.WithPreloaded("tasks", tasks.NewTaskModule().Loader),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
	)
	if err != nil {
		logger.Fatal("failed to create VM", zap.Error(err))
	}

	if err := vm.Import(luaScript, "app", "App"); err != nil {
		logger.Fatal("failed to import Lua code", zap.Error(err))
	}

	tasker := tasks.NewTasker(logger, vm, channel.NewChannelLayer(), 1024)
	statusCh, err := tasker.Start(ctx, "App")
	if err != nil {
		logger.Fatal("failed to start tasker", zap.Error(err))
	}

	status := <-statusCh
	logger.Info("tasker status", zap.Any("status", status))

	return model{
		tasker: tasker,
		logger: logger,
		ctx:    ctx,
		state:  vm.State(),
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Debug all key info
		m.logger.Info("key details",
			zap.String("type", msg.Type.String()),
			zap.String("string", msg.String()),
			zap.String("runes", string(msg.Runes)),
			zap.Any("key", msg),
		)

		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}

	// Map message and send to Lua
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

	return m, nil
}

func (m model) View() string {
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

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running program: %v\n", err)
	}
}
