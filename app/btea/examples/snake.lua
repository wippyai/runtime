local time = require("time")
local bapp = require("bapp")

function App()
    -- Initialize app with custom settings
    local app = bapp.new({
        btea.commands.enter_alt_screen,
        btea.commands.hide_cursor
    })

    -- Game dimensions
    local width = 30
    local height = 15

    -- Styles
    local styles = {
        border = btea.style()
            :border("rounded")
            :foreground("#89B4FA")
            :padding(1),

        score = btea.style()
            :bold()
            :foreground("#F9E2AF")
            :padding(0, 1),

        game_over = btea.style()
            :bold()
            :foreground("#F38BA8")
            :padding(1),

        help = btea.style()
            :foreground("#94E2D5")
            :padding(0, 1),

        snake = btea.style()
            :foreground("#A6E3A1"),

        food = btea.style()
            :foreground("#F5C2E7")
            :bold()
    }

    -- Key bindings
    local keys = {
        up = btea.bind {
            keys = {"up", "k", "w"},
            help = {key = "↑/k/w", desc = "move up"}
        },
        down = btea.bind {
            keys = {"down", "j", "s"},
            help = {key = "↓/j/s", desc = "move down"}
        },
        left = btea.bind {
            keys = {"left", "h", "a"},
            help = {key = "←/h/a", desc = "move left"}
        },
        right = btea.bind {
            keys = {"right", "l", "d"},
            help = {key = "→/l/d", desc = "move right"}
        },
        restart = btea.bind {
            keys = {"r"},
            help = {key = "r", desc = "restart game"}
        },
        quit = btea.bind {
            keys = {"q", "ctrl+c", "esc"},
            help = {key = "q/^C/esc", desc = "quit"}
        }
    }

    -- Game state
    local state = {
        snake = {},
        direction = "right",
        next_direction = "right",
        food = { x = 0, y = 0 },
        score = 0,
        high_score = 0,
        game_over = false
    }

    -- Helper functions
    local function spawn_food()
        -- Try to find an empty spot
        local empty_spots = {}
        for y = 0, height - 1 do
            for x = 0, width - 1 do
                local is_empty = true
                for _, segment in ipairs(state.snake) do
                    if segment.x == x and segment.y == y then
                        is_empty = false
                        break
                    end
                end
                if is_empty then
                    table.insert(empty_spots, {x = x, y = y})
                end
            end
        end

        if #empty_spots > 0 then
            local spot = empty_spots[math.random(1, #empty_spots)]
            state.food = {x = spot.x, y = spot.y}
        end
    end

    local function check_collision(point)
        -- Check walls
        if point.x < 0 or point.x >= width or
           point.y < 0 or point.y >= height then
            return true
        end

        -- Check self collision
        for i = 2, #state.snake do
            if point.x == state.snake[i].x and
               point.y == state.snake[i].y then
                return true
            end
        end
        return false
    end

    local function move_snake()
        if state.game_over then return end

        -- Update direction
        state.direction = state.next_direction

        local new_head = {
            x = state.snake[1].x,
            y = state.snake[1].y
        }

        -- Move head
        if state.direction == "up" then
            new_head.y = new_head.y - 1
        elseif state.direction == "down" then
            new_head.y = new_head.y + 1
        elseif state.direction == "left" then
            new_head.x = new_head.x - 1
        elseif state.direction == "right" then
            new_head.x = new_head.x + 1
        end

        -- Check collision
        if check_collision(new_head) then
            state.game_over = true
            if state.score > state.high_score then
                state.high_score = state.score
            end
            return
        end

        -- Add new head
        table.insert(state.snake, 1, new_head)

        -- Check food
        if new_head.x == state.food.x and new_head.y == state.food.y then
            state.score = state.score + 1
            spawn_food()
        else
            table.remove(state.snake)
        end
    end

    local function reset_game()
        -- Initialize snake in the middle
        state.snake = {
            { x = math.floor(width / 2), y = math.floor(height / 2) },
            { x = math.floor(width / 2) - 1, y = math.floor(height / 2) },
            { x = math.floor(width / 2) - 2, y = math.floor(height / 2) }
        }
        state.direction = "right"
        state.next_direction = "right"
        state.score = 0
        state.game_over = false
        spawn_food()
    end

    -- Initialize game
    reset_game()

    -- Update function
    local function update(self, msg)
        if msg.tick then
            move_snake()
        elseif msg.key then
            if keys.quit:matches(msg.key) then
                return true -- quit game
            elseif keys.restart:matches(msg.key) then
                reset_game()
            elseif not state.game_over then
                -- Handle direction changes
                if keys.up:matches(msg.key) and state.direction ~= "down" then
                    state.next_direction = "up"
                elseif keys.down:matches(msg.key) and state.direction ~= "up" then
                    state.next_direction = "down"
                elseif keys.left:matches(msg.key) and state.direction ~= "right" then
                    state.next_direction = "left"
                elseif keys.right:matches(msg.key) and state.direction ~= "left" then
                    state.next_direction = "right"
                end
            end
        end
        return false
    end

    -- View function
    local function view(self)
        -- Create game board
        local board = {}
        for y = 0, height - 1 do
            board[y] = {}
            for x = 0, width - 1 do
                board[y][x] = "  "
            end
        end

        -- Draw snake
        for i, segment in ipairs(state.snake) do
            if segment.x >= 0 and segment.x < width and
               segment.y >= 0 and segment.y < height then
                board[segment.y][segment.x] = styles.snake:render("██")
            end
        end

        -- Draw food
        board[state.food.y][state.food.x] = styles.food:render("◆◆")

        -- Build view
        local lines = {}

        -- Score display
        table.insert(lines, styles.score:render(string.format(
            "Score: %d   High Score: %d",
            state.score,
            state.high_score
        )))

        -- Game board
        local board_lines = {}
        for y = 0, height - 1 do
            local line = ""
            for x = 0, width - 1 do
                line = line .. board[y][x]
            end
            table.insert(board_lines, line)
        end

        -- Add bordered game board
        table.insert(lines, styles.border:render(
            table.concat(board_lines, "\n")
        ))

        -- Game state messages
        if state.game_over then
            table.insert(lines, styles.game_over:render("Game Over!"))
            table.insert(lines, styles.help:render(
                keys.restart.help.key .. ": " .. keys.restart.help.desc
            ))
        else
            -- Controls help
            local controls = {}
            for _, key in pairs({"up", "down", "left", "right"}) do
                table.insert(controls,
                    keys[key].help.key .. ": " .. keys[key].help.desc
                )
            end
            table.insert(lines, styles.help:render(
                table.concat(controls, "  |  ")
            ))
        end

        -- Quit help
        table.insert(lines, styles.help:render(
            keys.quit.help.key .. ": " .. keys.quit.help.desc
        ))

        return table.concat(lines, "\n")
    end

    -- Start game ticker
    coroutine.spawn(function()
        local ticker = time.ticker("150ms")
        while true do
            local result = ticker:channel():receive()
            if not result then break end
            upstream.send({ type = "update", tick = true })
        end
    end)

    -- Run the game
    app:run(update, view)
end

return App