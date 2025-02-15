local time = require("time")

function App()
    -- State management
    local state = {
        messages = {},
        window = {
            width = 80,
            height = 24
        }
    }

    -- Create box styles
    local box_style = btea.style()
        :border("rounded")
        :padding(0, 1)
        :width(state.window.width - 2)

    local header_style = btea.style()
        :bold()
        :foreground("#7D56F4")

    local log_style = btea.style()
        :foreground("#666666")

    -- Create a done channel for cleanup coordination
    local done = channel.new()

    -- Setup cleanup commands
    local cleanup_cmd = btea.sequence({
        btea.commands.show_cursor,
        btea.commands.exit_alt_screen
    })

    -- Setup init commands
    local init_cmd = btea.sequence({
        btea.commands.enter_alt_screen,
        btea.commands.hide_cursor
    })

    -- Execute init commands
    -- todO: not properly used
    local msgs = init_cmd:execute()

    -- Channel subscriptions
    local view_ch = pubsub.subscribe("@btea/view")
    local update_ch = pubsub.subscribe("@btea/update")
    local cancel_ch = pubsub.subscribe("@cancel")

    -- Message helper functions
    local function format_key_message(key_msg)
        if key_msg.String then
            return string.format(
                "%s: Pressed '%s'",
                time.now():format("15:04:05"),
                key_msg.String
            )
        end
        return string.format(
            "%s: Key '%s'",
            time.now():format("15:04:05"),
            key_msg.key_type
        )
    end

    local function format_mouse_message(mouse_msg)
        return string.format(
            "%s: Mouse %s at (%d,%d)",
            time.now():format("15:04:05"),
            mouse_msg.button,
            mouse_msg.x, mouse_msg.y
        )
    end

    local function format_window_message(window_msg)
        if window_msg.width ~= state.window.width or
           window_msg.height ~= state.window.height then
            state.window.width = window_msg.width
            state.window.height = window_msg.height
            -- Update box style width
            box_style = box_style:width(state.window.width - 2)
            return string.format(
                "%s: Window size: %dx%d",
                time.now():format("15:04:05"),
                window_msg.width,
                window_msg.height
            )
        end
        return nil
    end

    local function handle_ctrl_c()
    local cmd = cleanup_cmd:execute()
        if cmd then
            upstream.send(cmd)
        end

        -- Send ctrl+c upstream
        upstream.send(btea.commands.quit:execute()) -- this WILL shutdown execuition from outside!!

        return string.format(
            "%s: Ctrl+C pressed, signaling exit...",
            time.now():format("15:04:05")
        )
    end

    local function format_message(msg)
        -- Handle raw tick strings
        if type(msg) == "string" and msg == "tick" then
            return string.format("%s: Tick", time.now():format("15:04:05"))
        end

        -- Handle update messages
        if type(msg) == "table" and msg.type == "update" then
            if msg.key then
                -- Handle Ctrl+C specially
                if msg.key.key_type == "ctrl+c" then
                    return handle_ctrl_c()
                end
                return format_key_message(msg.key)
            elseif msg.mouse then
                return format_mouse_message(msg.mouse)
            elseif msg.window_size then
                return format_window_message(msg.window_size)
            end
        end

        return nil -- Skip unknown messages
    end

    -- View rendering
    local function render_view()
        local title = header_style:render(
            string.format("BubbleTea App (%dx%d)", state.window.width, state.window.height)
        )
        local subtitle = log_style:render("Press ESC or Ctrl+C to exit")

        local lines = {
            title,
            subtitle,
            "",
            header_style:render("Message Log:"),
            ""
        }

        -- Show last N messages that fit in the window
        local max_messages = state.window.height - 8
        local start_idx = math.max(1, #state.messages - max_messages)
        for i = start_idx, #state.messages do
            table.insert(lines, "  " .. state.messages[i])
        end

        return box_style:render(table.concat(lines, "\n"))
    end

    -- Start ticker in background
    coroutine.spawn(function()
        local ticker = time.ticker("1s")

        while true do
            local result = channel.select {
                ticker:channel():case_receive(),
                done:case_receive()
            }

            if result.channel == done then
                ticker:stop()
                break
            end

            upstream.send("tick")
        end
    end)

    -- Message handlers
    local function handle_view(task)
        task:complete(render_view())
    end

    local function handle_update(task)
        local input = task:input()
        if input then
            local msg = format_message(input)
            if msg then
                table.insert(state.messages, msg)
            end
        end
        task:complete("ok")
    end

    -- Main event loop
    while true do
        local result = channel.select {
            view_ch:case_receive(),
            update_ch:case_receive(),
            cancel_ch:case_receive()
        }

        -- Handle channel closure or errors
        if not result.ok then
            break
        end

        -- Handle message based on channel
        if result.channel == view_ch then
            handle_view(result.value)
        elseif result.channel == update_ch then
            handle_update(result.value)
        elseif result.channel == cancel_ch then
            break
        end
    end

    -- Cleanup
    done:send(true)  -- Signal ticker to stop
    done:close()

    pubsub.unsubscribe(view_ch)
    pubsub.unsubscribe(update_ch)
    pubsub.unsubscribe(cancel_ch)

    -- Execute cleanup commands
    local cmd = cleanup_cmd:execute()
    if cmd then
        upstream.send(cmd)
    end
end

return App