local bapp = require("base_app")

function App()
    local app = bapp.new()

    -- Define colors
    local colors = {
        highlight   = "#7D56F4",
        active_fg   = "#89B4FA",
        inactive_fg = "#6C7086",
        bg          = "#1E1E2E"
    }

    -- Create the table widget with columns and rows.
    local table_widget = btea.new_table {
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
            header   = btea.new_style()
                :bold()
                :padding(0, 1)
                :foreground("#FFFFFF")
                :background(colors.highlight),
            selected = btea.new_style()
                :bold(true)
                :foreground(colors.active_fg)
                :background("#2E2E3E")
        }
    }

    -- Assign the table widget to the app.
    app.tablewidget = table_widget

    -- Setup key bindings using the base app helper.
    app.keys = bapp.create_keys({
        quit     = { "q", "ctrl+c" },
        up       = { "up", "k" },
        down     = { "down", "j" },
        pageup   = { "pgup", "b" },
        pagedown = { "pgdown", " " }
    })

    -- Ensure the app has a command channel for propagating widget commands.
    app.cmd_channel = app.cmd_channel or channel.new(128)

    -- Update function: process input messages and propagate any commands.
    local function update(self, msg)
        if msg.key then
            if self.keys.quit:matches(msg) then
                return true -- Signal to quit the app.
            elseif self.keys.up:matches(msg) then
                local cmd = self.tablewidget:move_up(1)
                if cmd then self.cmd_channel:send(cmd) end
            elseif self.keys.down:matches(msg) then
                local cmd = self.tablewidget:move_down(1)
                if cmd then self.cmd_channel:send(cmd) end
            elseif self.keys.pageup:matches(msg) then
                local cmd = self.tablewidget:page_up()
                if cmd then self.cmd_channel:send(cmd) end
            elseif self.keys.pagedown:matches(msg) then
                local cmd = self.tablewidget:page_down()
                if cmd then self.cmd_channel:send(cmd) end
            end
        end

        local cmd = self.tablewidget:update(msg)
        if cmd then
            self.cmd_channel:send(cmd)
        end

        return false
    end

    -- View function: return the table's rendered view.
    local function view(self)
        return self.tablewidget:view()
    end

    -- Run the app. This call starts the main loop and processes input.
    app:run(update, view)
end

return App
