local components = require("chat_components")

function App()
    local inbox = tasks.channel()
    local done = channel.new()
    local update_channel = channel.new()

    local window_width = 80
    local window_height = 24

    local llm = components.LLMClient.new()
    local session = components.ChatSession.new()
    local ui = components.ChatUI.new(window_width, window_height)

    -- helper for logging messages in the UI
    local function add_log(msg)
        ui:add_log_entry(msg)
        upstream.send("refresh")
    end

    coroutine.spawn(function()
        local ticker = time.ticker("0.5s")
        while true do
            local result = channel.select {
                ticker:channel():case_receive(),
                done:case_receive()
            }
            if result.channel == done then break end
            ui.cursor_visible = not ui.cursor_visible
            upstream.send("refresh")
        end
    end)

    coroutine.spawn(function()
        while true do
            local msg, ok = update_channel:receive()
            if not ok then break end

            if msg.type == "update" then
                session:update_response(msg.text)
                upstream.send("refresh")
            elseif msg.type == "done" then
                session:finish_response()
                add_log("Response complete")
                upstream.send("refresh")
            elseif msg.type == "error" then
                add_log("Error: " .. tostring(msg.error))
            end
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

        if msg.type == "update" then
            if msg.key then
                if msg.key.String == "enter" and #ui.input_text > 0 then
                    local prompt = ui.input_text
                    session:add_message("user", prompt)
                    add_log("User prompt: " .. prompt)
                    ui.input_text = ""
                    session:start_response()
                    -- Pass the full message history to the LLM client along with our log callback
                    llm:query(prompt, session:get_history(), update_channel, add_log)
                elseif msg.key.String == "backspace" then
                    ui.input_text = ui.input_text:sub(1, -2)
                elseif #msg.key.String == 1 then
                    ui.input_text = ui.input_text .. msg.key.String
                end
            end
            task:complete("ok")
        elseif msg.type == "view" then
            task:complete(ui:render(session))
        else
            task:complete("ok")
        end
    end

    done:close()
end

return App
