local bapp = require("bapp")  -- Change to correct bapp import

function App()
    local app = bapp.new()

    -- Define colors
    app.colors = {
        highlight   = "#7D56F4",
        active_fg   = "#89B4FA",
        inactive_fg = "#6C7086",
        bg          = "#1E1E2E"
    }

    -- Create the table widget with columns and rows
    app.table = btea.new_table {
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
            header = btea.new_style()
                :bold()
                :padding(0, 1)
                :foreground("#FFFFFF")
                :background(app.colors.highlight),
            selected = btea.new_style()
                :bold(true)
                :foreground(app.colors.active_fg)
                :background("#2E2E3E")
        }
    }

    -- Setup key bindings with help text
    app.keys = bapp.create_keys({
        up = {
            keys = {"up", "k"},
            help = {key = "↑/k", desc = "move up"}
        },
        down = {
            keys = {"down", "j"},
            help = {key = "↓/j", desc = "move down"}
        },
        pageup = {
            keys = {"pgup", "b"},
            help = {key = "pgup/b", desc = "page up"}
        },
        pagedown = {
            keys = {"pgdown", " "},
            help = {key = "pgdn/space", desc = "page down"}
        }
    })

    -- Add help text style
    app.styles = {
        help = btea.new_style()
            :foreground(app.colors.inactive_fg)
            :italic()
    }

    -- Update function
    local function update(self, msg)
        if msg.key then
            if self.keys.quit:matches(msg) then
                return true
            elseif self.keys.up:matches(msg) then
                local cmd = self.table:move_up(1)
                if cmd then self:dispatch(cmd) end
            elseif self.keys.down:matches(msg) then
                local cmd = self.table:move_down(1)
                if cmd then self:dispatch(cmd) end
            elseif self.keys.pageup:matches(msg) then
                local cmd = self.table:page_up()
                if cmd then self:dispatch(cmd) end
            elseif self.keys.pagedown:matches(msg) then
                local cmd = self.table:page_down()
                if cmd then self:dispatch(cmd) end
            end
        end

        -- Update table state
        local cmd = self.table:update(msg)
        if cmd then self:dispatch(cmd) end

        return false
    end

    -- View function
    local function view(self)
        -- Get table content
        local table_view = self.table:view()

        -- Add help text below table
        local help = self.styles.help:render(
            "↑/k up | ↓/j down | pgup/b page up | pgdn/space page down | q/^C quit"
        )

        return table_view .. "\n" .. help
    end

    -- Run the app
    app:run(update, view)
end

return App