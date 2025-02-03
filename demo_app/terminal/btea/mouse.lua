function App()
    -- Setup channels
    inbox = tasks.channel()
    local done = channel.new()
    local cmd_channel = channel.new(128)

    -- Create zone manager
    local zone_manager = btea.new_zone_manager()

    -- Create viewport for log messages
    local log_viewport = btea.new_viewport({
        width = 60,
        height = 10,
        mouse_wheel_enabled = true,
        style = btea.new_style()
            :border(btea.borders.ROUNDED)
            :padding(1)
            :background("#1E1E2E")
    })

    -- Initial state
    local state = {
        log_messages = {},
        auto_scroll = true
    }

    -- Styles
    local styles = {
        button = btea.new_style()
            :padding_left(1)
            :padding_right(1)
            :border(btea.borders.ROUNDED)
            :background("#45475A"),

        active_button = btea.new_style()
            :padding_left(1)
            :padding_right(1)
            :border(btea.borders.ROUNDED)
            :background("#89B4FA")
            :foreground("#1E1E2E"),

        help = btea.new_style()
            :foreground("#6C7086")
            :italic()
    }

    -- Key bindings
    local keys = {
        quit = btea.new_binding({
            keys = {"q", "ctrl+c"},
            help = {key = "q/^C", desc = "quit"}
        })
    }

    -- Add a log message
    local function add_log(message)
        table.insert(state.log_messages, message)
        local content = table.concat(state.log_messages, "\n")
        log_viewport:set_content(content)

        -- Auto-scroll to bottom if enabled
        if state.auto_scroll then
            local cmd = log_viewport:scroll_to_bottom()
            if cmd then cmd_channel:send(cmd) end
        end
    end

    local function create_view()
        -- Create buttons with zones
        local add_msg_btn = zone_manager:mark(
            "add-msg",
            styles.button:render("Add Message")
        )

        local auto_scroll_style = state.auto_scroll and styles.active_button or styles.button
        local auto_scroll_btn = zone_manager:mark(
            "toggle-scroll",
            auto_scroll_style:render("Auto-scroll: " .. (state.auto_scroll and "ON" or "OFF"))
        )

        local clear_btn = zone_manager:mark(
            "clear",
            styles.button:render("Clear Logs")
        )

        -- Combine view elements
        local view = {
            btea.text.join_horizontal(btea.text.position.LEFT, add_msg_btn, "  ", auto_scroll_btn, "  ", clear_btn),
            "",
            log_viewport:view(),
            "",
            styles.help:render("Mouse wheel to scroll | Click buttons or use q/^C to quit")
        }

        -- Scan zones in final output
        return zone_manager:scan(table.concat(view, "\n"))
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

    -- Add initial messages
    for i = 1, 20 do
        add_log(string.format("Initial log message #%d", i))
    end

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
                -- Handle key presses
                if msg.key and keys.quit:matches(msg) then
                    break
                end

                -- Handle mouse events
                if msg.mouse then
                    if msg.mouse.type == "mouse" then
                        -- Handle scrolling
                        if log_viewport.mouse_wheel_enabled then
                            if msg.mouse.button == "wheel_up" then
                                local cmd = log_viewport:line_up(3)
                                if cmd then cmd_channel:send(cmd) end
                            elseif msg.mouse.button == "wheel_down" then
                                local cmd = log_viewport:line_down(3)
                                if cmd then cmd_channel:send(cmd) end
                            end
                        end

                        -- Handle button clicks
                        if msg.mouse.action == "press" then
                            local add_zone = zone_manager:get("add-msg")
                            local scroll_zone = zone_manager:get("toggle-scroll")
                            local clear_zone = zone_manager:get("clear")

                            if add_zone:in_bounds(msg.mouse) then
                                add_log(string.format("New message added at %s!", os.date("%H:%M:%S")))
                            elseif scroll_zone:in_bounds(msg.mouse) then
                                state.auto_scroll = not state.auto_scroll
                            elseif clear_zone:in_bounds(msg.mouse) then
                                state.log_messages = {}
                                log_viewport:set_content("")
                            end
                        end
                    end
                end

                -- Update viewport
                local cmd = log_viewport:update(msg)
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