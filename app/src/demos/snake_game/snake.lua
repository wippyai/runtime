local time = require("time")
local bapp = require("bapp")

function App()
    -- Create app with custom init commands
    local init_commands = {
        btea.commands.enter_alt_screen,
        btea.commands.hide_cursor
    }

    local app = bapp.new(init_commands)

    -- Game dimensions and state
    app.width = 30
    app.height = 15
    app.snake = {}
    app.direction = "right"
    app.next_direction = "right"
    app.food = { x = 0, y = 0 }
    app.score = 0
    app.high_score = 0
    app.game_over = false

    -- Setup key bindings properly using bapp.create_keys
    app.keys = bapp.create_keys({
        up = {
            keys = { "up", "k", "w" },
            help = { key = "↑/k/w", desc = "move up" }
        },
        down = {
            keys = { "down", "j", "s" },
            help = { key = "↓/j/s", desc = "move down" }
        },
        left = {
            keys = { "left", "h", "a" },
            help = { key = "←/h/a", desc = "move left" }
        },
        right = {
            keys = { "right", "l", "d" },
            help = { key = "→/l/d", desc = "move right" }
        },
        restart = {
            keys = { "r" },
            help = { key = "r", desc = "restart game" }
        },
        quit = {
            keys = { "q", "ctrl+c", "esc" },
            help = { key = "q/^C/esc", desc = "quit" }
        }
    })

    -- Define styles with consistent colors
    app.styles = {
        box = btea.style()
            :border(btea.borders.ROUNDED)
            :padding(1, 2)
            :foreground("#89B4FA")
            :background("#1E1E2E")
            :border_foreground("#89B4FA"),

        game_area = btea.style()
            :border(btea.borders.THICK)
            :padding(0)
            :foreground("#89B4FA")
            :background("#1E1E2E")
            :border_foreground("#89B4FA")
            :border_top_foreground("#89B4FA")
            :border_bottom_foreground("#89B4FA")
            :border_left_foreground("#89B4FA")
            :border_right_foreground("#89B4FA"),

        header = btea.style()
            :bold()
            :foreground("#CBA6F7")
            :padding(0, 1),

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

    -- Game helper functions
    function app:spawn_food()
        local empty_spots = {}
        for y = 0, self.height - 1 do
            for x = 0, self.width - 1 do
                local is_empty = true
                for _, segment in ipairs(self.snake) do
                    if segment.x == x and segment.y == y then
                        is_empty = false
                        break
                    end
                end
                if is_empty then
                    table.insert(empty_spots, { x = x, y = y })
                end
            end
        end

        if #empty_spots > 0 then
            local spot = empty_spots[math.random(1, #empty_spots)]
            self.food = { x = spot.x, y = spot.y }
        end
    end

    function app:check_collision(point)
        -- Check walls
        if point.x < 0 or point.x >= self.width or
            point.y < 0 or point.y >= self.height then
            return true
        end

        -- Check self collision (skip head)
        for i = 2, #self.snake do
            if point.x == self.snake[i].x and
                point.y == self.snake[i].y then
                return true
            end
        end
        return false
    end

    function app:move_snake()
        if self.game_over then return end

        -- Update direction
        self.direction = self.next_direction

        local new_head = {
            x = self.snake[1].x,
            y = self.snake[1].y
        }

        -- Move head
        if self.direction == "up" then
            new_head.y = new_head.y - 1
        elseif self.direction == "down" then
            new_head.y = new_head.y + 1
        elseif self.direction == "left" then
            new_head.x = new_head.x - 1
        elseif self.direction == "right" then
            new_head.x = new_head.x + 1
        end

        -- Check collision
        if self:check_collision(new_head) then
            self.game_over = true
            if self.score > self.high_score then
                self.high_score = self.score
            end
            return
        end

        -- Add new head
        table.insert(self.snake, 1, new_head)

        -- Check food
        if new_head.x == self.food.x and new_head.y == self.food.y then
            self.score = self.score + 1
            self:spawn_food()
        else
            table.remove(self.snake)
        end
    end

    function app:reset_game()
        -- Initialize snake in the middle
        self.snake = {
            { x = math.floor(self.width / 2),     y = math.floor(self.height / 2) },
            { x = math.floor(self.width / 2) - 1, y = math.floor(self.height / 2) },
            { x = math.floor(self.width / 2) - 2, y = math.floor(self.height / 2) }
        }
        self.direction = "right"
        self.next_direction = "right"
        self.score = 0
        self.game_over = false
        self:spawn_food()
    end

    -- Initialize game
    app:reset_game()

    -- Start game ticker coroutine
    coroutine.spawn(function()
        local ticker = time.ticker("150ms")
        while true do
            local result = ticker:channel():receive()
            if not result then break end
            app:upstream("tick")
        end
    end)

    -- Update handler
    local function update(self, msg)
        if msg.string == "tick" then
            self:move_snake()
        elseif msg.key then
            if self.keys.quit:matches(msg) then
                return true -- signal quit
            elseif self.keys.restart:matches(msg) then
                self:reset_game()
            elseif not self.game_over then
                -- Handle direction changes
                if self.keys.up:matches(msg) and self.direction ~= "down" then
                    self.next_direction = "up"
                elseif self.keys.down:matches(msg) and self.direction ~= "up" then
                    self.next_direction = "down"
                elseif self.keys.left:matches(msg) and self.direction ~= "right" then
                    self.next_direction = "left"
                elseif self.keys.right:matches(msg) and self.direction ~= "left" then
                    self.next_direction = "right"
                end
            end
        end
        return false -- continue running
    end

    -- View rendering
    local function view(self)
        -- Create game board
        local board = {}
        for y = 0, self.height - 1 do
            board[y] = {}
            for x = 0, self.width - 1 do
                board[y][x] = "  "
            end
        end

        -- Draw snake
        for _, segment in ipairs(self.snake) do
            if segment.x >= 0 and segment.x < self.width and
                segment.y >= 0 and segment.y < self.height then -- Fixed the typo here
                board[segment.y][segment.x] = self.styles.snake:render("██")
            end
        end

        -- Draw food
        board[self.food.y][self.food.x] = self.styles.food:render("◆◆")

        -- Build view content
        local content = {
            -- Header with scores
            self.styles.header:render("Snake Game"),
            self.styles.score:render(string.format(
                "Score: %d   High Score: %d",
                self.score,
                self.high_score
            )),
            ""
        }

        -- Game board with distinct border
        local board_lines = {}
        for y = 0, self.height - 1 do
            local line = table.concat(board[y])
            table.insert(board_lines, line)
        end

        -- Wrap the game board in its own bordered box
        table.insert(content, self.styles.game_area:render(table.concat(board_lines, "\n")))

        -- Game state message
        if self.game_over then
            table.insert(content, "")
            table.insert(content, self.styles.game_over:render("Game Over!"))
            table.insert(content, self.styles.help:render(
                "r: restart  |  q/^C/esc: quit"
            ))
        else
            -- Controls help
            table.insert(content, "")
            table.insert(content, self.styles.help:render(
                "↑/k/w: up  |  ↓/j/s: down  |  ←/h/a: left  |  →/l/d: right  |  q/^C/esc: quit"
            ))
        end

        return self.styles.box
            :width(self.window.width - 2)
            :height(self.window.height - 2)
            :render(table.concat(content, "\n"))
    end

    -- Run the app
    app:run(update, view)
end

return App
