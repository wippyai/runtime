function App()
    local inbox = tasks.channel()
    local done = channel.new()
    local operations = {}
    local window_width = 80
    local window_height = 24

    -- Initialize text input
    local input = btea.new_textinput()
    input:placeholder("Type something...")
    local cmd = input:focus()
    if cmd then
        upstream.send(cmd)
    end

    local ESC = string.char(27)
    local c = {
        reset = ESC .. "[0m",
        bold = ESC .. "[1m",
        dim = ESC .. "[2m",
        cyan = ESC .. "[36m",
        yellow = ESC .. "[33m",
        magenta = ESC .. "[35m",
        blue = ESC .. "[34m",
        green = ESC .. "[32m",
    }

    local function format_msg(msg)
        if msg.key then
            return string.format("KEY %s%s [%s]%s",
                c.yellow, msg.key.string, msg.key.key_type, c.reset)
        elseif msg.mouse then
            return string.format("MOUSE %s%s at %d,%d%s",
                c.yellow, msg.mouse.action, msg.mouse.x, msg.mouse.y, c.reset)
        elseif msg.window_size then
            return string.format("SIZE %s%d×%d%s",
                c.yellow, msg.window_size.width, msg.window_size.height, c.reset)
        elseif msg.msg == "tick" then
            return c.green .. "TICK" .. c.reset
        else
            return c.dim .. "UNKNOWN MSG" .. c.reset
        end
    end

    local function create_box(content, input_view)
        local w = window_width - 4
        local h = window_height - 2
        local lines = {
            "┌" .. string.rep("─", w - 2) .. "┐"
        }

        -- Calculate how many messages we can show
        local max_visible = h - 7  -- Header + empty line + borders + input
        local start_idx = math.max(1, #content - max_visible)

        for i = start_idx, #content do
            local line = content[i]
            if #line > w - 4 then
                line = line:sub(1, w - 7) .. "..."
            end
            line = line .. string.rep(" ", w - #line - 3)
            table.insert(lines, "│ " .. line .. "│")
        end

        -- Fill remaining space with empty lines
        while #lines < h - 1 do
            table.insert(lines, "│" .. string.rep(" ", w - 2) .. "│")
        end

        -- Add input line
        local input_line = "│ " .. input_view
        input_line = input_line .. string.rep(" ", w - #input_line - 1) .. "│"
        table.insert(lines, input_line)

        table.insert(lines, "└" .. string.rep("─", w - 2) .. "┘")
        return table.concat(lines, "\n")
    end

    coroutine.spawn(function()
        local ticker = time.ticker("1s")
        while true do
            local result = channel.select {
                ticker:channel():case_receive(),
                done:case_receive()
            }
            if result.channel == done then break end
            upstream.send("tick")
        end
    end)

    while true do
        local task, ok = inbox:receive()
        if not ok then
            done:send(true)
            break
        end

        local msg = task:input()

        if msg.type == "update" then
            -- Update window size if we get that message
            if msg.window_size then
                window_width = msg.window_size.width
                window_height = msg.window_size.height
                input:set_width(window_width - 6)  -- Adjust input width
            end

            -- Update text input
            local cmd = input:update(msg)
            if cmd then
                task:complete(cmd)
            else
                local now = time.now()
                local formatted = string.format("%s%s%s %s",
                    c.dim,
                    now:format("15:04:05"),
                    c.reset,
                    format_msg(msg)
                )

                if msg.key and msg.key.key_type == "enter" then
                    -- Add input value to operations when Enter is pressed
                    local value = input:value()
                    if value ~= "" then
                        table.insert(operations, c.cyan .. "INPUT: " .. value .. c.reset)
                        input:set_value("")
                    end
                end
                table.insert(operations, formatted)
                task:complete("ok")
            end
        elseif msg.type == "view" then
            local content = {
                c.bold .. "Debug View" .. c.reset,
                "",
                "Recent messages:",
                ""
            }

            local max_ops = window_height - 12
            local start_idx = math.max(1, #operations - max_ops)
            for i = start_idx, #operations do
                table.insert(content, operations[i])
            end

            task:complete(create_box(content, input:view()))
        else
            task:complete("ok")
        end
    end

    done:close()
end

return App