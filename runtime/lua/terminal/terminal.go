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
	"time"
)

const luaScript = `
function App()
    local inbox = tasks.channel()
    local width = 40
    local height = 20
    
    local state = {
        snake = {
            {x = math.floor(width/2), y = math.floor(height/2)},
            {x = math.floor(width/2)-1, y = math.floor(height/2)},
            {x = math.floor(width/2)-2, y = math.floor(height/2)}
        },
        direction = "right",
        food = {x = 0, y = 0},
        score = 0,
        game_over = false,
        last_tick = 0
    }
    
    local function spawn_food()
        state.food = {
            x = math.random(0, width-1),
            y = math.random(0, height-1)
        }
    end
    
    local function check_collision(point)
        -- Check walls
        if point.x < 0 or point.x >= width or point.y < 0 or point.y >= height then
            return true
        end
        
        -- Check self collision
        for i=2, #state.snake do
            if point.x == state.snake[i].x and point.y == state.snake[i].y then
                return true
            end
        end
        
        return false
    end
    
    local function move_snake()
        if state.game_over then return end
        
        local new_head = {x = state.snake[1].x, y = state.snake[1].y}
        
        if state.direction == "up" then
            new_head.y = new_head.y - 1
        elseif state.direction == "right" then
            new_head.x = new_head.x + 1
        elseif state.direction == "down" then
            new_head.y = new_head.y + 1
        elseif state.direction == "left" then
            new_head.x = new_head.x - 1
        end
        
        -- Check collision
        if check_collision(new_head) then
            state.game_over = true
            return
        end
        
        -- Insert new head
        table.insert(state.snake, 1, new_head)
        
        -- Check food
        if new_head.x == state.food.x and new_head.y == state.food.y then
            state.score = state.score + 1
            spawn_food()
        else
            -- Remove tail if didn't eat
            table.remove(state.snake)
        end
    end
    
    local function reset_game()
        state.snake = {
            {x = math.floor(width/2), y = math.floor(height/2)},
            {x = math.floor(width/2)-1, y = math.floor(height/2)},
            {x = math.floor(width/2)-2, y = math.floor(height/2)}
        }
        state.direction = "right"
        state.score = 0
        state.game_over = false
        spawn_food()
    end
    
    -- Initial food spawn
    spawn_food()
    
    while true do
        local task, ok = inbox:receive()
        if not ok then
            break
        end
        
        local msg = task:input()
        if msg.type == "update" then
            if msg.tick then
                move_snake()
            elseif msg.key then
                if msg.key.String == "r" and state.game_over then
                    reset_game()
                elseif not state.game_over then
                    -- Handle direction changes
                    if msg.key.String == "up" and state.direction ~= "down" then
                        state.direction = "up"
                    elseif msg.key.String == "right" and state.direction ~= "left" then
                        state.direction = "right"
                    elseif msg.key.String == "down" and state.direction ~= "up" then
                        state.direction = "down"
                    elseif msg.key.String == "left" and state.direction ~= "right" then
                        state.direction = "left"
                    end
                end
            end
            task:send(true)
            task:complete(nil)
        elseif msg.type == "view" then
            -- Create the game board
            local board = {}
            for y = 0, height-1 do
                board[y] = {}
                for x = 0, width-1 do
                    board[y][x] = "  "
                end
            end
            
            -- Draw snake
            for i, segment in ipairs(state.snake) do
                if segment.x >= 0 and segment.x < width and 
                   segment.y >= 0 and segment.y < height then
                    if i == 1 then
                        board[segment.y][segment.x] = "██" -- Head
                    else
                        board[segment.y][segment.x] = "██" -- Body
                    end
                end
            end
            
            -- Draw food
            board[state.food.y][state.food.x] = "●●"
            
            -- Build the view
            local view = string.format("\nScore: %d\n\n", state.score)
            
            -- Top border
            view = view .. "╔" .. string.rep("═", width * 2) .. "╗\n"
            
            -- Game board
            for y = 0, height-1 do
                view = view .. "║"
                for x = 0, width-1 do
                    view = view .. board[y][x]
                end
                view = view .. "║\n"
            end
            
            -- Bottom border
            view = view .. "╚" .. string.rep("═", width * 2) .. "╝\n\n"
            
            -- Game messages
            if state.game_over then
                view = view .. "Game Over! Press 'r' to restart or 'q' to quit\n"
            else
                view = view .. "Use arrow keys to move, 'q' to quit\n"
            end
            
            task:complete(view)
        end
    end
end

return App
`

type tickMsg struct{}

func tick() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
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
