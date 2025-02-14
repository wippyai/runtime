function App()
print("BOOT")
    local inbox = subscribe.subscribe("tasks")
    local done = channel.new()
    local operations = {}
    local window_width = 80
    local window_height = 24

    -- Start ticker in background
    coroutine.spawn(function()
        local ticker = time.ticker("1s")
        while true do
            local result = channel.select{
                ticker:channel():case_receive(),
                done:case_receive()
            }

            if result.channel == done then
                break
            end

            print("Tick") -- Debug print
            upstream.send("tick")
        end
    end)

    -- Render helpers
    local function create_box(w, h, content)
        local lines = {
            "┌" .. string.rep("─", w-2) .. "┐"
        }

        -- Add content lines
        for i = 1, h-2 do
            local content_line = content[i] or ""
            -- Pad content line to width
            content_line = content_line .. string.rep(" ", w-2-#content_line)
            table.insert(lines, "│" .. content_line .. "│")
        end

        table.insert(lines, "└" .. string.rep("─", w-2) .. "┘")
        return table.concat(lines, "\n")
    end

    -- Main loop
    while true do
        local task, ok = inbox:receive()
        if not ok then
            done:send(true)
            break
        end

        local msg = task:input()
        print("Received message:", msg.type) -- Debug print

        if msg.type == "update" then
            if msg.msg == "tick" then
                local now = time.now()
                table.insert(operations, now:format("15:04:05") .. " Tick received")
            elseif msg.key then
                local now = time.now()
                table.insert(operations, now:format("15:04:05") .. " Key: " .. msg.key.String)
            end
            task:complete("ok")
        elseif msg.type == "view" then
            -- Prepare content
            local content = {
                "Simple App",
                "",
                "Operations Log:",
                ""
            }

            -- Add last N operations
            local max_ops = window_height - 8 -- Reserve space for borders and headers
            local start_idx = math.max(1, #operations - max_ops)
            for i = start_idx, #operations do
                table.insert(content, "  " .. operations[i])
            end

            -- Create box with content
            local view = create_box(window_width, window_height, content)
            task:complete(view)
        else
            task:complete("ok")
        end
    end

    done:close()
end

return App