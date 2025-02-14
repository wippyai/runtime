local time = require("time")

function App()
    local done = channel.new()

    -- Create subscriptions
    local inbox = pubsub.subscribe("@btea/inbox")
    local cancel = pubsub.subscribe("@cancel")

    local operations = {}
    local window_width = 80
    local window_height = 24

    -- Start ticker in background
    coroutine.spawn(function()
        local ticker = time.ticker("1s")
        while true do
            local result = channel.select {
                ticker:channel():case_receive(),
                done:case_receive()
            }
            if result.channel == done then
                -- Cleanup ticker on exit
                ticker:stop()
                break
            end

            print("Tick") -- Debug print
            upstream.send("tick")
        end
    end)

    -- Render helpers
    local function create_box(w, h, content)
        local lines = {
            "┌" .. string.rep("─", w - 2) .. "┐"
        }

        -- Add content lines
        for i = 1, h - 2 do
            local content_line = content[i] or ""
            -- Pad content line to width
            content_line = content_line .. string.rep(" ", w - 2 - #content_line)
            table.insert(lines, "│" .. content_line .. "│")
        end

        table.insert(lines, "└" .. string.rep("─", w - 2) .. "┘")
        return table.concat(lines, "\n")
    end

    -- Debug function to inspect object
    local function dump_object(obj)
        if type(obj) == "table" then
            local s = "{"
            for k,v in pairs(obj) do
                if type(k) ~= "number" then k = '"'..k..'"' end
                s = s .. "["..k.."] = " .. dump_object(v) .. ","
            end
            return s .. "}"
        else
            return tostring(obj)
        end
    end

    -- Main loop
    while true do
        -- Add cancel signal handling to select
        local result = channel.select {
            inbox:case_receive(),
            cancel:case_receive()
        }

        -- Handle subscription closure
        if not result.ok then
            done:send(true)
            break
        end

        -- Check if cancel signal received
        if result.channel == cancel then
            print("Cancel signal received") -- Debug print
            done:send(true)
            break
        end

        -- Handle inbox messages
        local task = result.value
        local input = task:input()

        print("Message value:", dump_object(input))

        --if msg.type == "update" then
        --    if msg.msg == "tick" then
        --        local now = time.now()
        --        table.insert(operations, now:format("15:04:05") .. " Tick received")
        --    elseif msg.key then
        --        local now = time.now()
        --        table.insert(operations, now:format("15:04:05") .. " Key: " .. msg.key.String)
        --    end
        --    task:complete("ok")
        --elseif msg.type == "view" then
        --    -- Prepare content
        --    local content = {
        --        "Simple App",
        --        "",
        --        "Operations Log:",
        --        ""
        --    }
        --
        --    -- Add last N operations
        --    local max_ops = window_height - 8 -- Reserve space for borders and headers
        --    local start_idx = math.max(1, #operations - max_ops)
        --    for i = start_idx, #operations do
        --        table.insert(content, "  " .. operations[i])
        --    end
        --
        --    -- Create box with content
        --    local view = create_box(window_width, window_height, content)
        --    task:complete(view)
        --else
            task:complete("ok")
        --end

        ::continue::
    end

    -- Proper cleanup
    done:close()
    -- Unsubscribe from topics
    pubsub.unsubscribe(inbox)
    pubsub.unsubscribe(cancel)
end

return App