package terminal

import (
	"context"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/api/supervisor"
	transcode "github.com/ponyruntime/pony/pkg/payload/lua"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/tasks"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"io"
)

const luaScript = `
function App()
    local inbox = tasks.channel()
    local inputs = {
        { text = "", focused = true, label = "Input 1" },
        { text = "", focused = false, label = "Input 2" }
    }
    
    local function getCurrentInput()
        for i, input in ipairs(inputs) do
            if input.focused then
                return i, input
            end
        end
        return 1, inputs[1]
    end

    while true do
        local task, ok = inbox:receive()
        if not ok then
            break
        end
        
        local msg = task:input()
        if msg.type == "update" then
            local idx, current = getCurrentInput()
            
            if msg.key then
                if msg.key.String == "tab" then
                    inputs[idx].focused = false
                    local nextIdx = (idx % #inputs) + 1
                    inputs[nextIdx].focused = true
                elseif msg.key.Type == "space" or msg.key.String == " " then
                    current.text = current.text .. " "
                elseif msg.key.Type == "runes" then
                    current.text = current.text .. msg.key.String
                elseif msg.key.String == "backspace" then
                    if #current.text > 0 then
                        current.text = current.text:sub(1, -2)
                    end
                end
            end
            task:send(true)
            task:complete(nil)
        elseif msg.type == "view" then
            local view = ""
            for _, input in ipairs(inputs) do
                view = view .. input.label .. ": " 
                if input.focused then
                    view = view .. input.text .. "█"
                else
                    view = view .. input.text
                end
                view = view .. "\n"
            end
            view = view .. "\n[Tab] ?Switch fields • [Enter] Submit • [q] Quit"
            task:complete(view)
        end
    end
end

return App
`

type bubbleModel struct {
	tasker   *tasks.TaskRunner
	logger   *zap.Logger
	ctx      context.Context
	state    *lua.LState
	out      io.Writer
	quitting bool
}

func (m bubbleModel) Init() tea.Cmd {
	return tea.EnterAltScreen
}

func (m bubbleModel) mapMessage(msg tea.Msg) lua.LValue {
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

type LuaTerminal struct {
	logger *zap.Logger
}

func NewLuaTerminal(logger *zap.Logger) *LuaTerminal {
	if logger == nil {
		logger = zap.NewExample()
	}
	return &LuaTerminal{
		logger: logger,
	}
}

func (t *LuaTerminal) Run(ctx context.Context, in io.Reader, out io.Writer) error {
	vm, err := engine.NewCVM(t.logger,
		engine.WithPreloaded("tasks", tasks.NewTaskModule().Loader),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
	)
	if err != nil {
		return fmt.Errorf("failed to create VM: %w", err)
	}

	if err := vm.Import(luaScript, "app", "App"); err != nil {
		return fmt.Errorf("failed to import Lua code: %w", err)
	}

	tasker := tasks.NewTasker(t.logger, vm, channel.NewChannelLayer(), 1024)
	statusCh, err := tasker.Start(ctx, "App")
	if err != nil {
		return fmt.Errorf("failed to start tasker: %w", err)
	}

	status := <-statusCh
	t.logger.Info("tasker status", zap.Any("status", status))

	model := bubbleModel{
		tasker: tasker,
		logger: t.logger,
		ctx:    ctx,
		state:  vm.State(),
		out:    out,
	}

	p := tea.NewProgram(
		model,
		tea.WithInput(in),
		tea.WithOutput(out),
		tea.WithAltScreen(),
	)

	go func() { <-ctx.Done(); p.Quit() }()

	m, err := p.Run()
	if err != nil {
		return fmt.Errorf("bubbletea error: %w", err)
	}

	if m.(bubbleModel).quitting {
		return supervisor.Exited
	}

	return nil
}
