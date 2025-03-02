local bapp = require("bapp")

function App()
    -- Create app with custom init commands
    local init_commands = {
        btea.commands.enter_alt_screen,
        btea.commands.hide_cursor
    }

    local app = bapp.new(init_commands)

    -- Initialize spinner configurations
    app.spinners = {
        { name = "Line",      color = "#89B4FA" },
        { name = "Dot",       color = "#F5C2E7" },
        { name = "MiniDot",   color = "#94E2D5" },
        { name = "Jump",      color = "#FAB387" },
        { name = "Pulse",     color = "#F9E2AF" },
        { name = "Points",    color = "#A6E3A1" },
        { name = "Globe",     color = "#89B4FA" },
        { name = "Moon",      color = "#CBA6F7" },
        { name = "Monkey",    color = "#F5C2E7" },
        { name = "Meter",     color = "#94E2D5" },
        { name = "Hamburger", color = "#FAB387" },
        { name = "Ellipsis",  color = "#F9E2AF" },
    }

    -- Initialize spinners with their types
    for _, s in ipairs(app.spinners) do
        local spinner = btea.spinner {
            type = btea.spinners[string.upper(s.name)],
            interval = 100
        }
        s.spinner = spinner

        -- Apply style
        local style = btea.style()
            :foreground(s.color)
            :bold()

        s.spinner:style(style)

        -- Start spinner ticking
        app:dispatch(spinner:tick())
    end

    -- Define key bindings
    app.keys = {
        quit = btea.bind({
            keys = {"ctrl+c", "q", "esc"},
            help = {key = "^C/q/esc", desc = "quit"}
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

    -- Update function
    local function update(self, msg)
        -- Update window size if changed
        if msg.window_size then
            -- Could add window size-dependent logic here if needed
        end

        -- Handle key bindings
        if type(msg) == "table" and msg.type == "update" and msg.key then
            if self.keys.quit:matches(msg) then
                return true -- signal quit
            end
        end

        -- Update all spinners
        local cmds = {}
        for _, s in ipairs(self.spinners) do
            local cmd = s.spinner:update(msg)
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
            self.styles.title:render("Spinner Types Demo"),
            ""
        }

        -- Add each spinner with proper spacing
        for _, s in ipairs(self.spinners) do
            local name = s.name .. ":"
            local spinner_view = s.spinner:view()
            local line = string.format("%-15s %s", name, spinner_view)
            table.insert(lines, line)
        end

        -- Add help text
        table.insert(lines, "")
        table.insert(lines, self.styles.help:render("Press q, ^C, or esc to quit"))

        return self.styles.base:render(table.concat(lines, "\n"))
    end

    -- Run the app
    app:run(update, view)
end

return App