local bapp = require("bapp")

function App()
    local app = bapp.new()

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

        app:dispatch(spinner:tick())
    end

    -- Setup key bindings with help text
    app.keys = bapp.create_keys({
        quit = {
            keys = { "q", "ctrl+c" },
            help = { key = "q/^C", desc = "quit" }
        }
    })

    -- Styles for layout
    app.styles = {
        base = btea.style()
            :border(btea.borders.ROUNDED)
            :padding(1)
            :background("#1E1E2E"),

        title = btea.style()
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

        return false
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
        table.insert(lines, self.styles.help:render("Press q or ^C to quit"))

        return self.styles.base:render(table.concat(lines, "\n"))
    end

    -- Run the app
    app:run(update, view)
end

return App
