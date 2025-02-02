function App()
    local inbox = tasks.channel()
    local window_width = 80
    local window_height = 24

    -- Initialize table with columns
    local table = btea.new_table({
        {title = "ID", width = 6},
        {title = "Name", width = 20},
        {title = "Email", width = 30},
        {title = "Status", width = 15}
    }, {
        height = window_height - 4,
        width = window_width - 4,
        focused = true
    })

    -- Set some sample data
    table:set_rows({
        {"1", "John Smith", "john@example.com", "Active"},
        {"2", "Jane Doe", "jane@example.com", "Inactive"},
        {"3", "Bob Wilson", "bob@example.com", "Active"},
        {"4", "Alice Brown", "alice@example.com", "Pending"},
        {"5", "Charlie Davis", "charlie@example.com", "Active"},
    })

    -- Set styles for the table
    table:set_styles({
        selected = btea.new_style():foreground("#ffd700"):bold(),
        header = btea.new_style():foreground("#00ff00"):bold():underline(),
        cell = btea.new_style():foreground("#ffffff")
    })

    while true do
        local task, ok = inbox:receive()
        if not ok then
            break
        end

        local msg = task:input()

        if type(msg) == "table" then
            if msg.type == "update" then
                -- Handle window resize
                if msg.window_size then
                    window_width = msg.window_size.width
                    window_height = msg.window_size.height
                    table:set_width(window_width - 4)
                    table:set_height(window_height - 4)
                end

                -- Update table with any input (keyboard/mouse)
                table:update(msg)
                task:complete("ok")
            elseif msg.type == "view" then
                -- Render the table
                task:complete(table:view())
            end
        end
    end
end

return App