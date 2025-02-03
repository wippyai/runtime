function App()
    -- Setup channels
    inbox = tasks.channel()
    local done = channel.new()
    local cmd_channel = channel.new(128)

    -- Create all spinner types with different colors
    local spinners = {
        { name = "Line", color = "#89B4FA" },
        { name = "Dot", color = "#F5C2E7" },
        { name = "MiniDot", color = "#94E2D5" },
        { name = "Jump", color = "#FAB387" },
        { name = "Pulse", color = "#F9E2AF" },
        { name = "Points", color = "#A6E3A1" },
        { name = "Globe", color = "#89B4FA" },
        { name = "Moon", color = "#CBA6F7" },
        { name = "Monkey", color = "#F5C2E7" },
        { name = "Meter", color = "#94E2D5" },
        { name = "Hamburger", color = "#FAB387" },
        { name = "Ellipsis", color = "#F9E2AF" },
    }

    -- Initialize spinners with their types
    for _, s in ipairs(spinners) do
        local spinner = btea.new_spinner {
            type = btea.spinners[string.upper(s.name)],
            interval = 1000000
        }
        s.spinner = spinner

        -- Apply style
        local style = btea.new_style()
            :foreground(s.color)
            :bold()

        s.spinner:style(style)

        cmd_channel:send(spinner:tick())
    end

    -- Define key bindings
    local keys = {
        quit = btea.new_binding {
            keys = { "q", "ctrl+c" },
            help = { key = "q/^C", desc = "quit" }
        }
    }

    -- Styles for layout
    local styles = {
        base = btea.new_style()
            :border(btea.borders.ROUNDED)
            :padding(1)
            :background("#1E1E2E"),

        title = btea.new_style()
            :foreground("#CDD6F4")
            :bold(),

        help = btea.new_style()
            :foreground("#6C7086")
            :italic()
    }

    -- Initialize display
    local function create_view()
        local lines = {
            styles.title:render("Spinner Types Demo"),
            ""
        }

        -- Add each spinner with proper spacing
        for _, s in ipairs(spinners) do
            local name = s.name .. ":"
            local spinner_view = s.spinner:view()
            local line = string.format("%-15s %s", name, spinner_view)
            table.insert(lines, line)
        end

        -- Add help text
        table.insert(lines, "")
        table.insert(lines, styles.help:render("Press q or ^C to quit"))

        return styles.base:render(table.concat(lines, "\n"))
    end

    -- Start alt screen and hide cursor
    local init_cmd = btea.batch({
        btea.commands.enter_alt_screen,
        btea.commands.hide_cursor
    })
    cmd_channel:send(init_cmd)

    -- Command processor coroutine
    coroutine.spawn(function()
        while true do
            local result = channel.select {
                cmd_channel:case_receive(),
                done:case_receive()
            }

            if result.channel == done then
                break
            else
                local cmd = result.value
                if cmd then
                    local msg = cmd:execute()
                    if msg then upstream.send(msg) end
                end
            end
        end
    end)

    -- Main loop
    while true do
        local task, ok = inbox:receive()
        if not ok then
            done:send(true)
            break
        end

        local msg = task:input()

        if type(msg) == "table" then
            if msg.type == "update" then
                -- Handle key presses
                if msg.key and keys.quit:matches(msg) then
                    break
                end

                -- Update all spinners
                local cmds = {}
                for _, s in ipairs(spinners) do
                    local cmd = s.spinner:update(msg)
                    if cmd then
                        table.insert(cmds, cmd)
                    end
                end

                -- Send all commands as a batch if any
                if #cmds > 0 then
                    cmd_channel:send(btea.batch(cmds))
                end

                task:complete("ok")
            elseif msg.type == "view" then
                task:complete(create_view())
            else
                task:complete("ok")
            end
        else
            task:complete("ok")
        end
    end

    -- Cleanup
    done:close()

    -- Restore terminal
    local cleanup_cmd = btea.batch({
        btea.commands.show_cursor,
        btea.commands.exit_alt_screen
    })
    cmd_channel:send(cleanup_cmd)
end

return App