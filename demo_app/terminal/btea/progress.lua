function App()
    -- Setup channels
    inbox = tasks.channel()
    local done = channel.new()
    local cmd_channel = channel.new(128)

    -- Create different progress bars with various styles
    local progress_bars = {
        {
            name = "Default",
            progress = btea.new_progress {
                width = 40
            }
        },
        {
            name = "Solid Blue",
            progress = btea.new_progress {
                width = 40,
                fill_type = "solid",
                color = "#89B4FA"
            }
        },
        {
            name = "Default Gradient",
            progress = btea.new_progress {
                width = 40,
                fill_type = "gradient"
            }
        },
        {
            name = "Custom Gradient",
            progress = btea.new_progress {
                width = 40,
                gradient = {
                    from = "#F5C2E7",
                    to = "#94E2D5"
                }
            }
        },
        {
            name = "No Percentage",
            progress = btea.new_progress {
                width = 40,
                show_percentage = false,
                fill_type = "solid",
                color = "#CBA6F7"
            }
        }
    }

    -- Initialize progress values
    local progress_values = {}
    for i = 1, #progress_bars do
        progress_values[i] = 0
    end

    -- Define key bindings
    local keys = {
        quit = btea.new_binding {
            keys = { "q", "ctrl+c" },
            help = { key = "q/^C", desc = "quit" }
        },
        space = btea.new_binding {
            keys = { " " },
            help = { key = "space", desc = "increment" }
        },
        reset = btea.new_binding {
            keys = { "r" },
            help = { key = "r", desc = "reset" }
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
            styles.title:render("Progress Bar Types Demo"),
            ""
        }

        -- Add each progress bar
        for i, bar in ipairs(progress_bars) do
            table.insert(lines, bar.name .. ":")
            table.insert(lines, bar.progress:view())
            table.insert(lines, "")
        end

        -- Add help text
        table.insert(lines, styles.help:render("Space to increment | r to reset | q/^C to quit"))

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
                if msg.key then
                    if keys.quit:matches(msg) then
                        break
                    elseif keys.space:matches(msg) then
                        -- Increment all progress bars by 10%
                        for i, bar in ipairs(progress_bars) do
                            if progress_values[i] < 1.0 then
                                progress_values[i] = math.min(1.0, progress_values[i] + 0.1)
                                local cmd = bar.progress:set_percent(progress_values[i])
                                if cmd then
                                    cmd_channel:send(cmd)
                                end
                            end
                        end
                    elseif keys.reset:matches(msg) then
                        -- Reset all progress bars
                        for i, bar in ipairs(progress_bars) do
                            progress_values[i] = 0
                            local cmd = bar.progress:set_percent(0)
                            if cmd then
                                cmd_channel:send(cmd)
                            end
                        end
                    end
                end

                -- Update all progress bars
                local cmds = {}
                for _, bar in ipairs(progress_bars) do
                    local cmd = bar.progress:update(msg)
                    if cmd then
                        table.insert(cmds, cmd)
                    end
                end

                -- Send all commands as a batch if any
                if #cmds > 0 then
                    cmd_channel:send(btea.batch(cmds))
                end
            end
            task:complete(create_view())
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