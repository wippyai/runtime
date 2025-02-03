function App()
    -- Setup channels
    inbox = tasks.channel()
    local done = channel.new()
    local cmd_channel = channel.new(128)

    -- Initial state
    local state = {
        x = 10,
        y = 5,
        width = 40,
        height = 20,
    }

    -- Define key bindings
    local keys = {
        up = btea.new_binding {
            keys = { "up", "k" },
            help = { key = "↑/k", desc = "move up" }
        },
        down = btea.new_binding {
            keys = { "down", "j" },
            help = { key = "↓/j", desc = "move down" }
        },
        left = btea.new_binding {
            keys = { "left", "h" },
            help = { key = "←/h", desc = "move left" }
        },
        right = btea.new_binding {
            keys = { "right", "l" },
            help = { key = "→/l", desc = "move right" }
        },
        quit = btea.new_binding {
            keys = { "q", "ctrl+c" },
            help = { key = "q/^C", desc = "quit" }
        }
    }

    -- Styles
    local styles = {
        base = btea.new_style()
            :border(btea.borders.ROUNDED)
            :padding(1)
            :foreground("#89B4FA")
            :background("#1E1E2E"),

        player = btea.new_style()
            :foreground("#F5C2E7")
            :bold(),

        help = btea.new_style()
            :foreground("#6C7086")
            :italic()
    }

    -- Create help text
    local help_text = "Move with arrows/hjkl | q/^C to quit"

    -- Initialize display
    local function create_view()
        local lines = {}
        for y = 1, state.height do
            local line = ""
            for x = 1, state.width do
                if x == state.x and y == state.y then
                    line = line .. styles.player:render("@")
                else
                    line = line .. " "
                end
            end
            table.insert(lines, line)
        end

        -- Add help text at the bottom
        table.insert(lines, "")
        table.insert(lines, styles.help:render(help_text))

        return styles.base:render(table.concat(lines, "\n"))
    end

    -- Start alt screen and hide cursor
    local init_cmd = btea.batch({
        btea.commands.enter_alt_screen,
        btea.commands.hide_cursor
    })
    cmd_channel:send(init_cmd)

    -- Command processor coroutine
    coroutine.spawn(function()
        while true do
            local result = channel.select {
                cmd_channel:case_receive(),
                done:case_receive()
            }

            if result.channel == done then
                break
            else
                local cmd = result.value
                if cmd then
                    local msg = cmd:execute()
                    if msg then
                        upstream.send(msg)
                    end
                end
            end
        end
    end)

    -- Main loop
    while true do
        local task, ok = inbox:receive()
        if not ok then
            done:send(true)
            break
        end

        local msg = task:input()

        if type(msg) == "table" then
            if msg.type == "update" then
                -- Handle window size updates
                if msg.window_size then
                    state.width = math.min(msg.window_size.width - 4, 40)
                    state.height = math.min(msg.window_size.height - 4, 20)
                    -- Keep player in bounds after resize
                    state.x = math.min(state.x, state.width)
                    state.y = math.min(state.y, state.height)
                end

                -- Handle key presses
                if msg.key then
                    if keys.quit:matches(msg) then
                        break
                    elseif keys.up:matches(msg) then
                        state.y = math.max(1, state.y - 1)
                    elseif keys.down:matches(msg) then
                        state.y = math.min(state.height, state.y + 1)
                    elseif keys.left:matches(msg) then
                        state.x = math.max(1, state.x - 1)
                    elseif keys.right:matches(msg) then
                        state.x = math.min(state.width, state.x + 1)
                    end
                end
            end
            task:complete(create_view())
        else
            task:complete("ok")
        end
    end

    -- Cleanup
    done:close()

    -- Restore terminal
    local cleanup_cmd = btea.batch({
        btea.commands.show_cursor,
        btea.commands.exit_alt_screen
    })
    cmd_channel:send(cleanup_cmd)
end

return App
