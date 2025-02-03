local bapp = require("bapp")

function App()
    local app = bapp.new()

    -- Initialize app state
    app.operations = {}

    -- Initialize text input
    app.input = btea.new_text_input({
        placeholder = "Type something...",
        width = app.window.width - 8
    })
    app.input:focus()

    -- Define styles
    app.styles = {
        box = btea.new_style()
            :border(btea.borders.ROUNDED)
            :padding(1, 2)
            :foreground("#89B4FA")
            :background("#1E1E2E"),

        header = btea.new_style()
            :bold()
            :foreground("#CBA6F7")
            :padding(0, 1)
            :underline(),

        -- Messages styling
        key = btea.new_style():foreground("#F9E2AF"):bold(),
        mouse = btea.new_style():foreground("#94E2D5"):bold(),
        size = btea.new_style():foreground("#A6E3A1"):bold(),
        tick = btea.new_style():foreground("#89B4FA"),
        timestamp = btea.new_style():foreground("#6C7086"),
        command = btea.new_style():foreground("#F38BA8"):italic(),

        -- Input field styling
        input = btea.new_style()
            :foreground("#F5C2E7")
            :background("#313244")
            :padding(0, 1)
            :margin(1, 0)
    }

    -- Helper function to format messages
    local function format_msg(msg)
        if not msg then return app.styles.command:render("INVALID MSG") end

        if msg.key then
            return app.styles.key:render(string.format(
                "KEY %s [%s]",
                msg.key.string or "",
                msg.key.key_type or "unknown"
            ))
        elseif msg.mouse then
            return app.styles.mouse:render(string.format(
                "MOUSE %s at %d,%d",
                msg.mouse.action or "unknown",
                msg.mouse.x or 0,
                msg.mouse.y or 0
            ))
        elseif msg.window_size then
            return app.styles.size:render(string.format(
                "SIZE %d×%d",
                msg.window_size.width or 80,
                msg.window_size.height or 24
            ))
        elseif msg.msg == "tick" then
            return app.styles.tick:render("TICK")
        else
            return app.styles.command:render("UNKNOWN MSG: " .. msg.type)
        end
    end

    -- Update function
    local function update(self, msg)
        -- Update window size if message is valid
        if msg.window_size and type(msg.window_size) == "table" then
            self.input:set_width(self.window.width - 10)
        end

        -- Update text input
        local cmd = self.input:update(msg)
        if cmd then
            self:dispatch(cmd)
        end

        -- Log message with timestamp
        local now = time.now()
        local formatted = string.format("%s %s",
            self.styles.timestamp:render(now:format("15:04:05")),
            format_msg(msg)
        )

        if msg.key and msg.key.key_type == "enter" then
            local value = self.input:value()
            if value ~= "" then
                table.insert(self.operations,
                    self.styles.command:render("INPUT: " .. value)
                )
                self.input:set_value("")
            end
        end
        table.insert(self.operations, formatted)

        return false
    end

    -- View function
    local function view(self)
        local content_width = self.window.width - 6
        local header_divider = string.rep("─", content_width)

        local display_content = {
            self.styles.header:render("Debug View"),
            self.styles.timestamp:render(header_divider),
            self.styles.header:render("Recent messages:")
        }

        -- Calculate visible messages area
        local max_visible = self.window.height - 8
        local start_idx = math.max(1, #self.operations - max_visible)

        -- Add spacing before messages
        table.insert(display_content, "")

        -- Add messages with improved formatting
        for i = start_idx, #self.operations do
            local line = self.operations[i]
            if #line > content_width then
                line = line:sub(1, content_width - 3) .. "..."
            end
            table.insert(display_content, " " .. line)
        end

        -- Add input field with better visual separation
        table.insert(display_content, "")
        table.insert(display_content, self.styles.input:render(self.input:view() or ""))

        return self.styles.box
            :width(self.window.width - 2)
            :height(self.window.height - 2)
            :render(table.concat(display_content, "\n"))
    end

    -- Run the app
    app:run(update, view)
end

return App
