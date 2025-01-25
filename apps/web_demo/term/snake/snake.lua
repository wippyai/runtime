function App()
    local inbox = tasks.channel()
    local width = 40
    local height = 20

    local state = {
        snake = {
            { x = math.floor(width / 2), y = math.floor(height / 2) },
            { x = math.floor(width / 2) - 1, y = math.floor(height / 2) },
            { x = math.floor(width / 2) - 2, y = math.floor(height / 2) }
        },
        direction = "right",
        food = { x = 0, y = 0 },
        score = 0,
        game_over = false,
        last_tick = 0
    }

    local function spawn_food()
        state.food = {
            x = math.random(0, width - 1),
            y = math.random(0, height - 1)
        }
    end

    local function check_collision(point)
        -- Check walls
        if point.x < 0 or point.x >= width or point.y < 0 or point.y >= height then
            return true
        end

        -- Check self collision
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
            { x = math.floor(width / 2), y = math.floor(height / 2) },
            { x = math.floor(width / 2) - 1, y = math.floor(height / 2) },
            { x = math.floor(width / 2) - 2, y = math.floor(height / 2) }
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

        print(task:input())

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
            for y = 0, height - 1 do
                view = view .. "║"
                for x = 0, width - 1 do
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
