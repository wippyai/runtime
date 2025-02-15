local bapp = require("bapp")

function App()
    -- Create app with custom init commands
    local init_commands = {
        btea.commands.enter_alt_screen,
        btea.commands.hide_cursor
    }

    local app = bapp.new(init_commands)

    -- Define colors
    app.colors = {
        highlight   = "#7D56F4",
        active_fg   = "#89B4FA",
        inactive_fg = "#6C7086",
        bg          = "#1E1E2E"
    }

    -- Create the table widget with columns and rows
    app.table = btea.table {
        cols = {
            { title = "ID",   width = 5 },
            { title = "Name", width = 20 },
            { title = "Role", width = 15 }
        },
        rows = {
            { "1",  "Alice", "Developer" },
            { "2",  "Bob",   "Designer" },
            { "3",  "Carol", "Manager" },
            { "4",  "Dave",  "Tester" },
            { "5",  "Eve",   "DevOps" },
            { "6",  "Frank", "Support" },
            { "7",  "Grace", "HR" },
            { "8",  "Heidi", "QA" },
            { "9",  "Ivan",  "Engineer" },
            { "10", "Judy",  "Product" }
        },
        width = 50,
        height = 10,
        focused = true,
        styles = {
            header = btea.style()
                :bold()
                :padding(0, 1)
                :foreground("#FFFFFF")
                :background(app.colors.highlight),
            selected = btea.style()
                :bold()
                :foreground(app.colors.active_fg)
                :background("#2E2E3E")
        }
    }

    -- Define key bindings
    app.keys = {
        quit = btea.bind({
            keys = {"ctrl+c", "q", "esc"},
            help = {key = "^C/q/esc", desc = "quit"}
        }),
        up = btea.bind({
            keys = {"up", "k"},
            help = {key = "↑/k", desc = "move up"}
        }),
        down = btea.bind({
            keys = {"down", "j"},
            help = {key = "↓/j", desc = "move down"}
        }),
        pageup = btea.bind({
            keys = {"pgup", "b"},
            help = {key = "pgup/b", desc = "page up"}
        }),
        pagedown = btea.bind({
            keys = {"pgdown", " "},
            help = {key = "pgdn/space", desc = "page down"}
        })
    }

    -- Add help text style
    app.styles = {
        base = btea.style()
            :padding(1)
            :background(app.colors.bg)
            :border(btea.borders.ROUNDED),

        help = btea.style()
            :foreground(app.colors.inactive_fg)
            :italic()
    }

    -- Update function
    local function update(self, msg)
        -- Update window size if changed
        if msg.window_size then
            self.table:set_width(math.min(50, self.window.width - 4))
        end

        -- Handle key bindings
        if type(msg) == "table" and msg.type == "update" and msg.key then
            if self.keys.quit:matches(msg) then
                return true -- signal quit
            elseif self.keys.up:matches(msg) then
                self:dispatch(self.table:move_up(1))
            elseif self.keys.down:matches(msg) then
                self:dispatch(self.table:move_down(1))
            elseif self.keys.pageup:matches(msg) then
                self:dispatch(self.table:move_up(self.table:height()))
            elseif self.keys.pagedown:matches(msg) then
                self:dispatch(self.table:move_down(self.table:height()))
            end
        end

        return false -- continue running
    end

    -- View function
    local function view(self)
        -- Get table content
        local table_view = self.table:view()

        -- Add help text below table
        local help = self.styles.help:render(
            "↑/k up | ↓/j down | pgup/b page up | pgdn/space page down | ^C/q/esc quit"
        )

        -- Combine view elements
        local content = {
            table_view,
            "",
            help
        }

        return self.styles.base:render(table.concat(content, "\n"))
    end

    -- Run the app
    app:run(update, view)
end

return App