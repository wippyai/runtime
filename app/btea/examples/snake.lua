local time = require("time")
local bapp = require("bapp")

local border_style = btea.style()
    :border("rounded")
    :foreground("#7D56F4")

local score_style = btea.style()
    :bold()
    :foreground("#89B4FA")

local game_over_style = btea.style()
    :bold()
    :foreground("#FF5555")

function App()
    local app = bapp.new()

    -- Game dimensions
    local width = 40
    local height = 20

    -- Key bindings
    local keys = {
        up = btea.bind {
            keys = {"up", "k"},
            help = {key = "↑/k", desc = "move up"}
        },
        down = btea.bind {
            keys = {"down", "j"},
            help = {key = "↓/j", desc = "move down"}
        },
        left = btea.bind {
            keys = {"left", "h"},
            help = {key = "←/h", desc = "move left"}
        },
        right = btea.bind {
            keys = {"right", "l"},
            help = {key = "→/l", desc = "move right"}
        },
        restart = btea.bind {
            keys = {"r"},
            help = {key = "r", desc = "restart game"}
        },
        quit = btea.bind {
            keys = {"q", "ctrl+c", "esc"},
            help = {key = "q/^C/esc", desc = "quit game"}
        }
    }

    -- Game state
    local state = {
        snake = {},
        direction = "right",
        food = { x = 0, y = 0 },
        score = 0,
        game_over = false
    }

    local function spawn_food()
        state.food = {
            x = math.random(0, width - 1),
            y = math.random(0, height - 1)
        }
    end

    local function check_collision(point)
        if point.x < 0 or point.x >= width or point.y < 0 or point.y >= height then
            return true
        end
        for i = 2, #state.snake do
            if point.x == state.snake[i].x and point.y == state.snake[i].y then
                return true
            end
        end
        return false
    end

    local function move_snake()
        if state.game_over then return end

        local new_head = { x = state.snake[1].x, y = state.snake[1].y }
        if state.direction == "up" then
            new_head.y = new_head.y - 1
        elseif state.direction == "down" then
            new_head.y = new_head.y + 1
        elseif state.direction == "left" then
            new_head.x = new_head.x - 1
        elseif state.direction == "right" then
            new_head.x = new_head.x + 1
        end

        if check_collision(new_head) then
            state.game_over = true
            return
        end

        table.insert(state.snake, 1, new_head)
        if new_head.x == state.food.x and new_head.y == state.food.y then
            state.score = state.score + 1
            spawn_food()
        else
            table.remove(state.snake)
        end
    end

    local function reset_game()
        state.snake = {
            { x = math.floor(width / 2),     y = math.floor(height / 2) },
            { x = math.floor(width / 2) - 1, y = math.floor(height / 2) },
            { x = math.floor(width / 2) - 2, y = math.floor(height / 2) }
        }
        state.direction = "right"
        state.score = 0
        state.game_over = false
        spawn_food()
    end

    -- Initialize game
    reset_game()

    -- Update function for bapp runner
    local function update(self, msg)
        if msg.tick then
            move_snake()
        elseif msg.key then
            if keys.quit:matches(msg.key) then
                return true -- quit game
            elseif keys.restart:matches(msg.key) and state.game_over then
                reset_game()
            elseif not state.game_over then
                if keys.up:matches(msg.key) and state.direction ~= "down" then
                    state.direction = "up"
                elseif keys.down:matches(msg.key) and state.direction ~= "up" then
                    state.direction = "down"
                elseif keys.left:matches(msg.key) and state.direction ~= "right" then
                    state.direction = "left"
                elseif keys.right:matches(msg.key) and state.direction ~= "left" then
                    state.direction = "right"
                end
            end
        end
        return false -- continue running
    end

    -- View function for bapp runner
    local function view(self)
        local board = {}
        for y = 0, height - 1 do
            board[y] = {}
            for x = 0, width - 1 do
                board[y][x] = "  " -- empty cell
            end
        end

        -- Draw snake segments
        for i, segment in ipairs(state.snake) do
            if segment.x >= 0 and segment.x < width and segment.y >= 0 and segment.y < height then
                board[segment.y][segment.x] = "██"
            end
        end

        -- Draw food
        board[state.food.y][state.food.x] = "●●"

        local lines = {}
        table.insert(lines, score_style:render(string.format("Score: %d", state.score)))
        table.insert(lines, "") -- blank line
        -- Top border
        table.insert(lines, border_style:render("╔" .. string.rep("═", width * 2) .. "╗"))
        for y = 0, height - 1 do
            local line = "║"
            for x = 0, width - 1 do
                line = line .. board[y][x]
            end
            line = line .. "║"
            table.insert(lines, border_style:render(line))
        end
        -- Bottom border
        table.insert(lines, border_style:render("╚" .. string.rep("═", width * 2) .. "╝"))
        table.insert(lines, "")

        -- Help text
        if state.game_over then
            table.insert(lines, game_over_style:render("Game Over!"))
            table.insert(lines, keys.restart.help.key .. ": " .. keys.restart.help.desc)
        else
            table.insert(lines, "Controls:")
            for _, key in pairs({"up", "down", "left", "right"}) do
                table.insert(lines, keys[key].help.key .. ": " .. keys[key].help.desc)
            end
        end
        table.insert(lines, keys.quit.help.key .. ": " .. keys.quit.help.desc)

        return table.concat(lines, "\n")
    end

    -- Spawn a ticker coroutine that sends tick messages every 200ms
    coroutine.spawn(function()
        local ticker = time.ticker("200ms")
        while true do
            local result = ticker:channel():receive()
            if not result then break end
            upstream.send({ tick = true, type = "update" })
        end
    end)

    -- Run the game with our update and view functions
    app:run(update, view)
end

return App