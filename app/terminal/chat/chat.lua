local components = require("chat_components")

function App()
    local inbox = tasks.channel()
    local done = channel.new()
    local operations = {}
    local window_width = 80
    local window_height = 24

    -- Initialize text input
    local input = btea.new_textinput()
    input:placeholder("Type something...")
    input:set_width(window_width - 8)
    local cmd = input:focus()
    if cmd then
        upstream.send(cmd)
    end

    -- LLM and session initialization
    local llm = components.LLMClient.new()
    local session = components.ChatSession.new()
    local update_channel = channel.new()

    local styles = {
        box = btea.style()
            :border(btea.borders.ROUNDED)
            :padding(1, 2)
            :foreground("#89B4FA")
            :background("#1E1E2E"),

        header = btea.style()
            :bold()
            :foreground("#CBA6F7")
            :padding(0, 1)
            :underline(),

        key = btea.style():foreground("#F9E2AF"):bold(),
        mouse = btea.style():foreground("#94E2D5"):bold(),
        size = btea.style():foreground("#A6E3A1"):bold(),
        tick = btea.style():foreground("#89B4FA"),
        timestamp = btea.style():foreground("#6C7086"),
        command = btea.style():foreground("#F38BA8"):italic(),
        ai = btea.style():foreground("#A6E3A1"):italic(), -- AI responses style

        input = btea.style()
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

        local max_visible = window_height - 8
        local start_idx = math.max(1, #content - max_visible)

        table.insert(display_content, "")

        for i = start_idx, #content do
            local line = content[i]
            if #line > content_width then
                line = line:sub(1, content_width - 3) .. "..."
            end
            table.insert(display_content, " " .. line)
        end

        table.insert(display_content, "")
        table.insert(display_content, styles.input:render(input_view or ""))

        return styles.box
            :width(window_width - 2)
            :height(window_height - 2)
            :render(table.concat(display_content, "\n"))
    end

    -- Helper: log messages with timestamp (for non-AI logs)
    local function add_log(entry)
        local now = time.now():format("15:04:05")
        table.insert(operations, styles.timestamp:render(now) .. " " .. entry)
        upstream.send("refresh")
    end

    -- LLM update listener: accumulate update chunks into one AI response (without timestamp)
    coroutine.spawn(function()
        local ai_response = nil
        while true do
            local msg, ok = update_channel:receive()
            if not ok then break end

            if msg.type == "update" then
                session:update_response(msg.text)
                if not ai_response then
                    ai_response = msg.text
                    table.insert(operations, styles.ai:render(ai_response))
                else
                    ai_response = ai_response .. msg.text
                    operations[#operations] = styles.ai:render(ai_response)
                end
                upstream.send("refresh")
            elseif msg.type == "done" then
                session:finish_response()
                -- Reset for next AI response
                ai_response = nil
            elseif msg.type == "error" then
                add_log("LLM error: " .. tostring(msg.error))
            end
        end
    end)

    -- Optional: ticker for debug messages
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
            update_channel:close()
            break
        end

        local msg = task:input()

        if type(msg) == "table" and msg.type == "update" then
            -- Update window size if provided
            if msg.window_size and type(msg.window_size) == "table" then
                window_width = msg.window_size.width or window_width
                window_height = msg.window_size.height or window_height
                input:set_width(window_width - 10)
            end

            local cmd = input:update(msg)
            if cmd then
                task:complete(cmd)
            else
                -- On enter key, trigger the LLM query if input is non-empty.
                if msg.key and msg.key.key_type == "enter" then
                    local value = input:value()
                    if value ~= "" then
                        table.insert(operations, styles.command:render("INPUT: " .. value))
                        session:add_message("user", value)
                        session:start_response()
                        input:set_value("")
                        llm:query(value, session:get_history(), update_channel, add_log)
                    end
                end

                task:complete("ok")
            end

        elseif type(msg) == "table" and msg.type == "view" then
            task:complete(create_box(operations, input:view()))
        else
            task:complete("ok")
        end
    end

    done:close()
end

return App
