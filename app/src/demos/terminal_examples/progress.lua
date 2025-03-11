local bapp = require("bapp")

function App()
    -- Create app with custom init commands
    local init_commands = {
        btea.commands.enter_alt_screen,
        btea.commands.hide_cursor
    }

    local app = bapp.new(init_commands)

    -- Create different progress bars with various styles
    app.progress_bars = {
        {
            name = "Default",
            progress = btea.progress {
                width = 40
            }
        },
        {
            name = "Solid Blue",
            progress = btea.progress {
                width = 40,
                fill_type = "solid",
                color = "#89B4FA"
            }
        },
        {
            name = "Default Gradient",
            progress = btea.progress {
                width = 40,
                fill_type = "gradient"
            }
        },
        {
            name = "Custom Gradient",
            progress = btea.progress {
                width = 40,
                gradient = {
                    from = "#F5C2E7",
                    to = "#94E2D5"
                }
            }
        },
        {
            name = "No Percentage",
            progress = btea.progress {
                width = 40,
                show_percentage = false,
                fill_type = "solid",
                color = "#CBA6F7"
            }
        }
    }

    -- Initialize progress values
    app.progress_values = {}
    for i = 1, #app.progress_bars do
        app.progress_values[i] = 0
    end

    -- Define key bindings
    app.keys = {
        quit = btea.bind({
            keys = {"ctrl+c", "q", "esc"},
            help = {key = "^C/q/esc", desc = "quit"}
        }),
        increment = btea.bind({
            keys = {" "},
            help = {key = "space", desc = "increment"}
        }),
        reset = btea.bind({
            keys = {"r"},
            help = {key = "r", desc = "reset"}
        })
    }

    -- Styles for layout
    app.styles = {
        base = btea.style()
            :border(btea.borders.ROUNDED)
            :padding(1)
            :background("#1E1E2E"),

        title = btea.style()
            :foreground("#CDD6F4")
            :bold(),

        help = btea.style()
            :foreground("#6C7086")
            :italic()
    }

    -- Helper functions
    local function increment_progress(self)
        -- Increment all progress bars by 10%
        local cmds = {}
        for i, bar in ipairs(self.progress_bars) do
            if self.progress_values[i] < 1.0 then
                self.progress_values[i] = math.min(1.0, self.progress_values[i] + 0.1)
                local cmd = bar.progress:set_percent(self.progress_values[i])
                if cmd then
                    table.insert(cmds, cmd)
                end
            end
        end
        if #cmds > 0 then
            self:dispatch(btea.batch(cmds))
        end
    end

    local function reset_progress(self)
        -- Reset all progress bars
        local cmds = {}
        for i, bar in ipairs(self.progress_bars) do
            self.progress_values[i] = 0
            local cmd = bar.progress:set_percent(0)
            if cmd then
                table.insert(cmds, cmd)
            end
        end
        if #cmds > 0 then
            self:dispatch(btea.batch(cmds))
        end
    end

    -- Update function
    local function update(self, msg)
        -- Update window size if changed
        if msg.window_size then
            -- Update progress bar widths based on new window size
            for _, bar in ipairs(self.progress_bars) do
                bar.progress:set_width(math.min(40, self.window.width - 4))
            end
        end

        -- Handle key bindings
        if type(msg) == "table" and msg.type == "update" and msg.key then
            if self.keys.quit:matches(msg) then
                return true -- signal quit
            elseif self.keys.increment:matches(msg) then
                increment_progress(self)
            elseif self.keys.reset:matches(msg) then
                reset_progress(self)
            end
        end

        -- Update all progress bars
        local cmds = {}
        for _, bar in ipairs(self.progress_bars) do
            local cmd = bar.progress:update(msg)
            if cmd then
                table.insert(cmds, cmd)
            end
        end

        -- Send all commands as a batch if any
        if #cmds > 0 then
            self:dispatch(btea.batch(cmds))
        end

        return false -- continue running
    end

    -- View function
    local function view(self)
        local lines = {
            self.styles.title:render("Progress Bar Types Demo"),
            ""
        }

        -- Add each progress bar
        for i, bar in ipairs(self.progress_bars) do
            table.insert(lines, bar.name .. ":")
            table.insert(lines, bar.progress:view())
            table.insert(lines, "")
        end

        -- Add help text with simple format
        local help_text = "space to increment | r to reset | ^C/q/esc to quit"
        table.insert(lines, self.styles.help:render(help_text))

        return self.styles.base:render(table.concat(lines, "\n"))
    end

    -- Run the app
    app:run(update, view)
end

return App