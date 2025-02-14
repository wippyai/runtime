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

    -- Create a done channel for cleanup coordination
    local done = channel.new()

    -- Channel subscriptions
    local view_ch = pubsub.subscribe("@btea/view")
    local update_ch = pubsub.subscribe("@btea/update")
    local cancel_ch = pubsub.subscribe("@cancel")

    -- Message helper functions
    local function format_key_message(key_msg)
        return string.format(
            "%s: Key %s%s%s%s",
            time.now():format("15:04:05"),
            key_msg.alt and "Alt+" or "",
            key_msg.ctrl and "Ctrl+" or "",
            key_msg.shift and "Shift+" or "",
            key_msg.String or key_msg.key_type
        )
    end

    local function format_mouse_message(mouse_msg)
        return string.format(
            "%s: Mouse %s at (%d,%d)%s%s%s",
            time.now():format("15:04:05"),
            mouse_msg.button,
            mouse_msg.x, mouse_msg.y,
            mouse_msg.alt and " +Alt" or "",
            mouse_msg.ctrl and " +Ctrl" or "",
            mouse_msg.shift and " +Shift" or ""
        )
    end

    local function format_window_message(window_msg)
        return string.format(
            "%s: Window resized to %dx%d",
            time.now():format("15:04:05"),
            window_msg.width,
            window_msg.height
        )
    end

    local function format_message(msg)
        if msg.type == "update" then
            if msg.key then
                return format_key_message(msg.key)
            elseif msg.mouse then
                return format_mouse_message(msg.mouse)
            elseif msg.window_size then
                -- Update window size state
                state.window.width = msg.window_size.width
                state.window.height = msg.window_size.height
                return format_window_message(msg.window_size)
            end
        end
        return string.format("%s: Unknown message type: %s", time.now():format("15:04:05"), msg.type or "nil")
    end

    -- View rendering
    local function render_view()
        local lines = {
            "BubbleTea App (" .. state.window.width .. "x" .. state.window.height .. ")",
            "",
            "Message Log:",
            ""
        }

        -- Show last N messages that fit in the window
        local max_messages = state.window.height - 8
        local start_idx = math.max(1, #state.messages - max_messages)
        for i = start_idx, #state.messages do
            table.insert(lines, "  " .. state.messages[i])
        end

        -- Create bordered box
        local box = {
            "┌" .. string.rep("─", state.window.width - 2) .. "┐"
        }

        for i = 1, state.window.height - 2 do
            local content = lines[i] or ""
            content = content .. string.rep(" ", state.window.width - 2 - #content)
            table.insert(box, "│" .. content .. "│")
        end

        table.insert(box, "└" .. string.rep("─", state.window.width - 2) .. "┘")

        return table.concat(box, "\n")
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
            table.insert(state.messages, format_message(input))
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
end

return App