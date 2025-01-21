function App()
    -- Create channels for input and display
    local inbox = tasks.channel()
    local text = ""
    local cursor_pos = 0

    -- Main event loop
    while true do
        -- Wait for next task
        local task, ok = inbox:receive()
        if not ok then
            break
        end

        -- Handle the message
        local msg = task:input()

        if msg.type == "update" then
            if msg.key then
                if msg.key.Type == "Enter" then
                    text = text .. "\n"
                    cursor_pos = cursor_pos + 1
                elseif msg.key.Type == "Space" or msg.key.String == " " then
                    text = text .. " "
                    cursor_pos = cursor_pos + 1
                elseif msg.key.Type == "Rune" then
                    text = text .. msg.key.String
                    cursor_pos = cursor_pos + 1
                elseif msg.key.Type == "Backspace" then
                    if #text > 0 then
                        text = text:sub(1, -2)
                        cursor_pos = math.max(0, cursor_pos - 1)
                    end
                end
            end
            task:send(true)
            task:complete(nil)
        elseif msg.type == "view" then
            -- Build the display string with cursor
            local display = "Simple Terminal Editor\n"
                .. "----------------------\n\n"
                .. text .. "█\n\n"
                .. "Commands: Ctrl+C to exit"

            task:complete(display)
        elseif msg.type == "exit" then
            task:complete("Goodbye!")
            break
        end
    end
end

return App
