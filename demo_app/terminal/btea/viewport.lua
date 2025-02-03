function App()
    -- Setup channels
    inbox = tasks.channel()
    local done = channel.new()
    local cmd_channel = channel.new(128)

    -- Create a viewport for our log viewer
    local log_view = btea.new_viewport {
        width = 60,
        height = 20,
        mouse_wheel_enabled = true,
        style = btea.new_style()
            :border(btea.borders.ROUNDED)
            :padding(1)
            :background("#1E1E2E")
    }

    -- Define key bindings
    local keys = {
        quit = btea.new_binding {
            keys = {"q", "ctrl+c"},
            help = {key = "q/^C", desc = "quit"}
        },
        pagedown = btea.new_binding {
            keys = {"pgdown", " "},
            help = {key = "pgdn/space", desc = "page down"}
        },
        pageup = btea.new_binding {
            keys = {"pgup", "b"},
            help = {key = "pgup/b", desc = "page up"}
        },
        down = btea.new_binding {
            keys = {"down", "j"},
            help = {key = "↓/j", desc = "down"}
        },
        up = btea.new_binding {
            keys = {"up", "k"},
            help = {key = "↑/k", desc = "up"}
        },
        add = btea.new_binding {
            keys = {"a"},
            help = {key = "a", desc = "add log"}
        }
    }

    -- Styles for different log levels
    local styles = {
        info = btea.new_style():foreground("#89B4FA"),
        warn = btea.new_style():foreground("#FAB387"),
        error = btea.new_style():foreground("#F38BA8"),
        debug = btea.new_style():foreground("#A6E3A1"),
        help = btea.new_style():foreground("#6C7086"):italic()
    }

    -- Sample log levels and messages for random generation
    local log_levels = {"info", "warn", "error", "debug"}
    local sample_messages = {
        "Application started",
        "Processing request",
        "Database connection established",
        "Cache miss detected",
        "Invalid input received",
        "Memory usage: 256MB",
        "Request timeout",
        "Task completed successfully",
        "Configuration loaded",
        "Rate limit exceeded"
    }

    -- Helper to generate a random log entry
    local function random_log()
        local level = log_levels[math.random(1, #log_levels)]
        local msg = sample_messages[math.random(1, #sample_messages)]
        local now = time.now()
        local timestamp = now:format("15:04:05")
        return styles[level]:render(string.format("[%s] [%s] %s", timestamp, level:upper(), msg))
    end

    -- Initialize with some logs
    local logs = {}
    for i = 1, 50 do
        table.insert(logs, random_log())
    end
    log_view:set_content(table.concat(logs, "\n"))

    local function create_view()
        -- Get viewport content
        local viewport_view = log_view:view()

        -- Add help text below viewport
        local help = styles.help:render(
            "↑/k up | ↓/j down | pgup/b page up | pgdn/space page down | a add log | q/^C quit"
        )

        return viewport_view .. "\n" .. help
    end

    -- Start alt screen and hide cursor
    cmd_channel:send(btea.batch({
        btea.commands.enter_alt_screen,
        btea.commands.hide_cursor
    }))

    -- Command processor
    coroutine.spawn(function()
        while true do
            local result = channel.select({
                cmd_channel:case_receive(),
                done:case_receive()
            })

            if result.channel == done then
                break
            else
                local cmd = result.value
                if cmd then
                    local msg = cmd:execute()
                    if msg then upstream.send(msg) end
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
                if msg.key then
                    if keys.quit:matches(msg) then
                        break
                    elseif keys.pagedown:matches(msg) then
                        local cmd = log_view:page_down()
                        if cmd then cmd_channel:send(cmd) end
                    elseif keys.pageup:matches(msg) then
                        local cmd = log_view:page_up()
                        if cmd then cmd_channel:send(cmd) end
                    elseif keys.down:matches(msg) then
                        local cmd = log_view:line_down()
                        if cmd then cmd_channel:send(cmd) end
                    elseif keys.up:matches(msg) then
                        local cmd = log_view:line_up()
                        if cmd then cmd_channel:send(cmd) end
                    elseif keys.add:matches(msg) then
                        -- Add a new random log entry
                        table.insert(logs, random_log())
                        log_view:set_content(table.concat(logs, "\n"))
                        -- If we were at the bottom, scroll to new content
                        if log_view:at_bottom() then
                            local cmd = log_view:scroll_to_bottom()
                            if cmd then cmd_channel:send(cmd) end
                        end
                    end
                end

                -- Handle mouse wheel events
                if msg.mouse and log_view.mouse_wheel_enabled then
                    if msg.mouse.button == "wheel_up" then
                        local cmd = log_view:line_up(3)
                        if cmd then cmd_channel:send(cmd) end
                    elseif msg.mouse.button == "wheel_down" then
                        local cmd = log_view:line_down(3)
                        if cmd then cmd_channel:send(cmd) end
                    end
                end

                -- Update viewport state
                local cmd = log_view:update(msg)
                if cmd then cmd_channel:send(cmd) end
            end
            task:complete(create_view())
        else
            task:complete("ok")
        end
    end

    -- Cleanup
    done:close()
    cmd_channel:send(btea.batch({
        btea.commands.show_cursor,
        btea.commands.exit_alt_screen
    }))
end

return App