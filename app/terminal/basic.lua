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

    -- Helper function to format messages
    local function format_message(msg)
        local timestamp = time.now():format("15:04:05")
        if type(msg) == "table" and msg.String then
            return string.format("%s: Key pressed - %s", timestamp, msg.String)
        end
        return string.format("%s: %s", timestamp, tostring(msg))
    end

    -- View rendering
    local function render_view()
        local lines = {
            "BubbleTea App",
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
        print("Starting ticker coroutine")
        local ticker = time.ticker("1s")

        while true do
            local result = channel.select {
                ticker:channel():case_receive(),
                done:case_receive()
            }

            if result.channel == done then
                print("Stopping ticker")
                ticker:stop()
                break
            end

            print("Tick received")
            upstream.send("tick")
        end
    end)

    -- Message handlers
    local function handle_init(task)
        print("Init handler called")
        table.insert(state.messages, format_message("Application initialized"))
        task:complete("ok")
    end

    local function handle_view(task)
        print("View handler called")
        task:complete(render_view())
    end

    local function handle_update(task)
        print("Update handler called")
        local input = task:input()
        if input then
            table.insert(state.messages, format_message(input))
        end
        task:complete("ok")
    end

    -- Main event loop
    print("Starting main event loop")
    while true do
        local result = channel.select {
            view_ch:case_receive(),
            update_ch:case_receive(),
            cancel_ch:case_receive()
        }
        print("Received message on channel")

        -- Handle channel closure or errors
        if not result.ok then
            print("Channel error or closure")
            break
        end

        -- Handle message based on channel
        if result.channel == view_ch then
            handle_view(result.value)
        elseif result.channel == update_ch then
            handle_update(result.value)
        elseif result.channel == cancel_ch then
            print("Received cancel signal")
            break
        end
    end

    -- Cleanup
    print("Starting cleanup")
    done:send(true)  -- Signal ticker to stop
    done:close()

    print("Unsubscribing from channels")
    pubsub.unsubscribe(view_ch)
    pubsub.unsubscribe(update_ch)
    pubsub.unsubscribe(cancel_ch)

    print("Cleanup complete")
end

return App