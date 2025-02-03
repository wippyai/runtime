local bapp = require("bapp")

function App()
    local app = bapp.new()

    -- Create different progress bars with various styles
    app.progress_bars = {
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
    app.progress_values = {}
    for i = 1, #app.progress_bars do
        app.progress_values[i] = 0
    end

    -- Setup key bindings with help text
    app.keys = bapp.create_keys({
        space = {
            keys = {" "},
            help = {key = "space", desc = "increment"}
        },
        reset = {
            keys = {"r"},
            help = {key = "r", desc = "reset"}
        }
    })

    -- Styles for layout
    app.styles = {
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

    -- Update function
    local function update(self, msg)
        if msg.key then
            if self.keys.quit:matches(msg) then
                return true
            elseif self.keys.space:matches(msg) then
                -- Increment all progress bars by 10%
                for i, bar in ipairs(self.progress_bars) do
                    if self.progress_values[i] < 1.0 then
                        self.progress_values[i] = math.min(1.0, self.progress_values[i] + 0.1)
                        local cmd = bar.progress:set_percent(self.progress_values[i])
                        if cmd then
                            self:dispatch(cmd)
                        end
                    end
                end
            elseif self.keys.reset:matches(msg) then
                -- Reset all progress bars
                for i, bar in ipairs(self.progress_bars) do
                    self.progress_values[i] = 0
                    local cmd = bar.progress:set_percent(0)
                    if cmd then
                        self:dispatch(cmd)
                    end
                end
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

        return false
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

        -- Add help text
        table.insert(lines, self.styles.help:render("Space to increment | r to reset | q/^C to quit"))

        return self.styles.base:render(table.concat(lines, "\n"))
    end

    -- Run the app
    app:run(update, view)
end

return App