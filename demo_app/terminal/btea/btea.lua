function App()
    inbox = tasks.channel()
    local done = channel.new()
    local cmd_channel = channel.new(128)
    local operations = {}
    local window_width = 80
    local window_height = 24

    -- Initialize text input
    local input = btea.new_textinput()
    input:placeholder("Type something...")
    input:set_width(window_width - 8)

    -- Initial focus command
    local focus_cmd = input:focus()
    if focus_cmd then
        cmd_channel:send(focus_cmd)
    end

    local styles = {
        box = btea.new_style()
            :border(btea.borders.ROUNDED)
            :padding(1, 2)
            :foreground("#89B4FA")
            :background("#1E1E2E"),

        header = btea.new_style()
            :bold()
            :foreground("#CBA6F7")
            :padding(0, 1)
            :underline(),

        -- Messages styling
        key = btea.new_style():foreground("#F9E2AF"):bold(),
        mouse = btea.new_style():foreground("#94E2D5"):bold(),
        size = btea.new_style():foreground("#A6E3A1"):bold(),
        tick = btea.new_style():foreground("#89B4FA"),
        timestamp = btea.new_style():foreground("#6C7086"),
        command = btea.new_style():foreground("#F38BA8"):italic(),

        -- Input field styling
        input = btea.new_style()
            :foreground("#F5C2E7")
            :background("#313244")
            :padding(0, 1)
            :margin(1, 0)
    }

    local function create_box(content, input_view)
        local content_width = window_width - 6
        local header_divider = string.rep("─", content_width)

        local display_content = {
            styles.header:render("Debug View"),
            styles.timestamp:render(header_divider),
            styles.header:render("Recent messages:")
        }

        -- Calculate visible messages area
        local max_visible = window_height - 8
        local start_idx = math.max(1, #content - max_visible)

        -- Add spacing before messages
        table.insert(display_content, "")

        -- Add messages with improved formatting
        for i = start_idx, #content do
            local line = content[i]
            if #line > content_width then
                line = line:sub(1, content_width - 3) .. "..."
            end
            table.insert(display_content, " " .. line)
        end

        -- Add input field with better visual separation
        table.insert(display_content, "")
        table.insert(display_content, styles.input:render(input_view or ""))

        return styles.box
            :width(window_width - 2)
            :height(window_height - 2)
            :render(table.concat(display_content, "\n"))
    end

    local function format_msg(msg)
        if not msg then return styles.command:render("INVALID MSG") end

        if msg.key then
            return styles.key:render(string.format(
                "KEY %s [%s]",
                msg.key.string or "",
                msg.key.key_type or "unknown"
            ))
        elseif msg.mouse then
            return styles.mouse:render(string.format(
                "MOUSE %s at %d,%d",
                msg.mouse.action or "unknown",
                msg.mouse.x or 0,
                msg.mouse.y or 0
            ))
        elseif msg.window_size then
            return styles.size:render(string.format(
                "SIZE %d×%d",
                msg.window_size.width or 80,
                msg.window_size.height or 24
            ))
        elseif msg.msg == "tick" then
            return styles.tick:render("TICK")
        else
            return styles.command:render("UNKNOWN MSG: " .. msg.type)
        end
    end

    -- Command processor coroutine
    coroutine.spawn(function()
        while true do
            local result = channel.select {
                cmd_channel:case_receive(), -- Handle commands
                done:case_receive()         -- Handle done signal
            }

            if result.channel == done then
                break
            else -- Command channel
                local cmd = result.value
                if cmd then
                    local msg = cmd:execute()
                    if msg then
                        --table.insert(operations, styles.command:render("Command result: " .. msg.type))
                        upstream.send(msg)
                    end
                end
            end
        end
    end)

    while true do
        local task, ok = inbox:receive()
        if not ok then
            done:send(true)
            break
        end

        local msg = task:input()

        if type(msg) == "table" and msg.type == "update" then
            -- Update window size if message is valid
            if msg.window_size and type(msg.window_size) == "table" then
                window_width = msg.window_size.width or window_width
                window_height = msg.window_size.height or window_height
                input:set_width(window_width - 10)
            end

            -- Update text input synchronously
            local cmd = input:update(msg)
            if cmd then
                cmd_channel:send(cmd) -- Send command to processor
            end

            local now = time.now()
            local formatted = string.format("%s %s",
                styles.timestamp:render(now:format("15:04:05")),
                format_msg(msg)
            )

            if msg.key and msg.key.key_type == "enter" then
                local value = input:value()
                if value ~= "" then
                    table.insert(operations,
                        styles.command:render("INPUT: " .. value)
                    )
                    input:set_value("")
                end
            end
            --table.insert(operations, formatted)
            task:complete("ok")
        elseif type(msg) == "table" and msg.type == "view" then
            task:complete(create_box(operations, input:view()))
        else
            task:complete("ok")
        end
    end

    done:close()
end

return App
